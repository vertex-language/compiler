package x86_64

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	bctx "github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/platform"
	linuxplatform "github.com/vertex-language/compiler/platform/linux"
	"github.com/vertex-language/compiler/wasm"
)

func Compile(
	m *wasm.Module,
	arch string,
	qualifiedSymbols bool,
	gpuKernels map[uint32]bool,
) (*object.WasmObj, error) {
	c := &moduleCompiler{
		m:                m,
		arch:             arch,
		qualifiedSymbols: qualifiedSymbols,
		gpuKernels:       gpuKernels,
	}
	if err := c.compile(); err != nil {
		return nil, err
	}
	return c.obj, nil
}

type callReloc struct {
	codeOff int
	funcIdx int
	isAbs64 bool
}

type moduleCompiler struct {
	m                *wasm.Module
	arch             string
	qualifiedSymbols bool
	gpuKernels       map[uint32]bool

	obj             *object.WasmObj
	code            []byte
	funcOff         []int
	relocs          []callReloc
	allTypeIdx      []uint32
	importNames     []string
	importPtrMasks  map[int][]bool
	importHptrMasks map[int][]bool
	returnHptrMasks map[int]bool
	inlinedImports  map[int]inlinedImport
	trampolineSyms  map[int]int
}

func parseImportSig(name string) (base string, ptrMask []bool, hptrMask []bool, retHptr bool) {
	idx := strings.IndexByte(name, '@')
	if idx == -1 {
		return name, nil, nil, false
	}
	base = name[:idx]
	sig := name[idx+1:]
	if sig == "" {
		return base, nil, nil, false
	}

	// Split parameters from the return type
	parts := strings.Split(sig, ":")
	params := parts[0]
	if len(parts) > 1 && parts[1] == "hptr" {
		retHptr = true
	}

	if params != "" {
		pTokens := strings.Split(params, ".")
		ptrMask = make([]bool, len(pTokens))
		hptrMask = make([]bool, len(pTokens))
		for i, p := range pTokens {
			ptrMask[i] = (p == "ptr")
			hptrMask[i] = (p == "hptr")
		}
	}
	return base, ptrMask, hptrMask, retHptr
}

func (c *moduleCompiler) compile() error {
	c.obj = &object.WasmObj{}
	c.inlinedImports = make(map[int]inlinedImport)
	c.importPtrMasks = make(map[int][]bool)
	c.importHptrMasks = make(map[int][]bool)
	c.returnHptrMasks = make(map[int]bool)

	// ── Data / memory sizing ──────────────────────────────────────────────────

	var maxDataSize int32
	for _, d := range c.m.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, err := evalConstExprI32(active.Offset)
			if err != nil {
				return err
			}
			end := off + 65536 + int32(len(d.Data))
			if end > maxDataSize {
				maxDataSize = end
			}
		}
	}

	const shadowStackTop = int32(1048576)
	minMemSize := shadowStackTop + 65536
	if c.m.Memories.Len() > 0 {
		declared := int32(c.m.Memories.Entries[0].Lim.Min*65536) + 65536
		if declared > minMemSize {
			minMemSize = declared
		}
	}
	if minMemSize > maxDataSize {
		maxDataSize = minMemSize
	}
	if maxDataSize < shadowStackTop+65536 {
		return fmt.Errorf(
			"compile: data buffer (%d bytes) too small for shadow stack top at %d + 64 KB R15 shift",
			maxDataSize, shadowStackTop,
		)
	}

	fmt.Fprintf(os.Stderr, "compile: allocating obj.Data size=%d\n", maxDataSize)

	if maxDataSize > 0 {
		c.obj.Data = make([]byte, maxDataSize)
		for _, d := range c.m.Datas.Entries {
			if active, ok := d.Mode.(wasm.DataModeActive); ok {
				off, _ := evalConstExprI32(active.Offset)
				dst := off + 65536
				fmt.Fprintf(os.Stderr,
					"compile: data seg off=%d len=%d → obj.Data[%d:%d] (bufsize=%d)\n",
					off, len(d.Data), dst, int(dst)+len(d.Data), maxDataSize)
				if int(dst)+len(d.Data) > int(maxDataSize) {
					return fmt.Errorf(
						"compile: data segment [%d+%d] overflows obj.Data (size %d)",
						dst, len(d.Data), maxDataSize,
					)
				}
				copy(c.obj.Data[dst:], d.Data)
			}
		}
	}

	for idx, g := range c.m.Globals.Entries {
		slot := 65536 - 8*(idx+1)
		if slot < 0 || slot+8 > len(c.obj.Data) {
			return fmt.Errorf(
				"compile: global %d slot %d out of data buffer bounds (%d bytes)",
				idx, slot, len(c.obj.Data),
			)
		}
		switch g.Type.Val {
		case wasm.I32:
			iv, err := evalConstExprI32(g.Init)
			if err != nil {
				return fmt.Errorf("compile: global %d (i32): %w", idx, err)
			}
			binary.LittleEndian.PutUint64(c.obj.Data[slot:], uint64(int64(iv)))
		case wasm.I64:
			iv, err := evalConstExprI64(g.Init)
			if err != nil {
				return fmt.Errorf("compile: global %d (i64): %w", idx, err)
			}
			binary.LittleEndian.PutUint64(c.obj.Data[slot:], uint64(iv))
		}
	}

	c.obj.Symbols = append(c.obj.Symbols, object.Symbol{
		Name:    "__wasm_data_base",
		Kind:    object.SymDefined,
		Section: object.SymSecData,
		Offset:  0,
	})

	// ── Import processing ─────────────────────────────────────────────────────

	for _, e := range c.m.Imports.Entries {
		if e.Kind == wasm.ImportFunc {
			c.allTypeIdx = append(c.allTypeIdx, e.TypeIdx)
		}
	}
	c.allTypeIdx = append(c.allTypeIdx, c.m.Functions.TypeIndices...)

	numImportFuncs := 0
	for _, e := range c.m.Imports.Entries {
		if e.Kind != wasm.ImportFunc {
			continue
		}
		funcIdx := numImportFuncs
		route := platform.Parse(e.Module)
		realName, ptrMask, hptrMask, retHptr := parseImportSig(e.Name)

		if hptrMask != nil {
			c.importHptrMasks[funcIdx] = hptrMask
		}
		if retHptr {
			c.returnHptrMasks[funcIdx] = true
		}

		// Intercept internal Vertex allocator imports
		if e.Module == "memory" {
			if ptrMask != nil {
				c.importPtrMasks[funcIdx] = ptrMask
			}
			sym := "__vertex_memory_" + strings.ReplaceAll(realName, ".", "_")
			c.importNames = append(c.importNames, sym)
			c.obj.Symbols = append(c.obj.Symbols, object.Symbol{Name: sym, Kind: object.SymUndefined})
			numImportFuncs++
			continue
		}

		switch route.Kind {
		case platform.SyscallTrampoline:
			ft := c.m.Types.Entries[e.TypeIdx]
			if err := c.resolveSyscallImport(funcIdx, e.Module, realName, ptrMask, ft); err != nil {
				return fmt.Errorf("compile: import %q::%q: %w", e.Module, e.Name, err)
			}
			c.importNames = append(c.importNames, "")

		case platform.PlatformLib:
			if ptrMask != nil {
				c.importPtrMasks[funcIdx] = ptrMask
			}
			sym := e.Module + "::" + realName
			c.importNames = append(c.importNames, sym)
			c.obj.Symbols = append(c.obj.Symbols, object.Symbol{Name: sym, Kind: object.SymUndefined})

		case platform.CrossPlatformLib:
			if ptrMask != nil {
				c.importPtrMasks[funcIdx] = ptrMask
			}
			sym := realName
			if c.qualifiedSymbols {
				sym = e.Module + "::" + realName
			}
			c.importNames = append(c.importNames, sym)
			c.obj.Symbols = append(c.obj.Symbols, object.Symbol{Name: sym, Kind: object.SymUndefined})
		}
		numImportFuncs++
	}

	// ── Function compilation ──────────────────────────────────────────────────

	c.funcOff = make([]int, c.m.Codes.Len())
	for i := range c.m.Codes.Bodies {
		wasmIdx := numImportFuncs + i
		if c.gpuKernels[uint32(wasmIdx)] {
			c.funcOff[i] = len(c.code)
			continue
		}

		// Inject the required temporary build context to satisfy the new signature
		tempCtx := bctx.NewBuildContext(c.m)
		tempCtx.ImportPtrMasks = c.importPtrMasks
		tempCtx.ImportHptrMasks = c.importHptrMasks
		tempCtx.ReturnHptrMasks = c.returnHptrMasks

		funcCode, relocs, err := compileFuncBody(tempCtx, wasmIdx, c.inlinedImports)
		if err != nil {
			return fmt.Errorf("compiler: function %d: %w", wasmIdx, err)
		}

		base := len(c.code)
		c.funcOff[i] = base
		for _, r := range relocs {
			c.relocs = append(c.relocs, callReloc{
				codeOff: base + r.codeOff,
				funcIdx: r.funcIdx,
				isAbs64: r.isAbs64,
			})
		}
		c.code = append(c.code, funcCode...)
	}

	// ── Exports ───────────────────────────────────────────────────────────────

	for _, e := range c.m.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		if c.gpuKernels[e.Idx] {
			continue
		}
		localIdx := int(e.Idx) - numImportFuncs
		if localIdx < 0 || localIdx >= len(c.funcOff) {
			continue
		}
		c.obj.Symbols = append(c.obj.Symbols, object.Symbol{
			Name:   e.Name,
			Kind:   object.SymDefined,
			Offset: c.funcOff[localIdx],
		})
	}

	c.generateCallbackTrampolines(numImportFuncs)
	if err := c.applyRelocs(numImportFuncs); err != nil {
		return err
	}
	c.obj.Code = c.code
	return nil
}

func (c *moduleCompiler) resolveSyscallImport(
	funcIdx int, moduleName, funcName string, ptrMask []bool, ft wasm.FuncType,
) error {
	n, ok := linuxplatform.SyscallNumber(funcName, c.arch)
	if !ok {
		return fmt.Errorf("unknown syscall %q for arch %q", funcName, c.arch)
	}
	c.inlinedImports[funcIdx] = inlinedImport{
		module:  moduleName,
		name:    funcName,
		number:  n,
		ptrMask: ptrMask,
	}
	fmt.Fprintf(os.Stderr, "compile: syscall import %q::%q → inline syscall %d (%s)\n",
		moduleName, funcName, n, c.arch)
	return nil
}

// ── Callback trampolines ──────────────────────────────────────────────────────

func (c *moduleCompiler) generateCallbackTrampolines(numImportFuncs int) {
	c.trampolineSyms = make(map[int]int)

	subR15 := [][3]byte{
		{0x4C, 0x29, 0xFF}, {0x4C, 0x29, 0xFE},
		{0x4C, 0x29, 0xFA}, {0x4C, 0x29, 0xF9},
		{0x4D, 0x29, 0xF8}, {0x4D, 0x29, 0xF9},
	}

	for _, r := range c.relocs {
		if !r.isAbs64 || r.funcIdx < numImportFuncs {
			continue
		}
		localIdx := r.funcIdx - numImportFuncs
		if _, already := c.trampolineSyms[localIdx]; already {
			continue
		}

		ft := c.m.Types.Entries[c.allTypeIdx[r.funcIdx]]
		targetOff := c.funcOff[localIdx]
		trampolineOff := len(c.code)

		// mov r15, [rip + __wasm_mem_base]
		// Loads the wasm base directly; no add-65536 needed since __wasm_mem_base
		// already points to wasm byte 0 (the mmap'd region base).
		c.code = append(c.code, 0x4C, 0x8B, 0x3D)
		c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
			Offset: trampolineOff + 3,
			Symbol: "__wasm_mem_base",
			Kind:   object.RelocRel32,
		})
		c.code = asm.Append32LE(c.code, 0)

		// Convert incoming native pointers back to wasm offsets (sub reg, r15).
		nParams := len(ft.Params)
		if nParams > 6 {
			nParams = 6
		}
		for j := 0; j < nParams; j++ {
			if ft.Params[j] == wasm.I32 {
				c.code = append(c.code, subR15[j][:]...)
			}
		}

		// jmp to the target wasm function body.
		c.code = append(c.code, 0xE9)
		rel := int32(targetOff - (len(c.code) + 4))
		c.code = asm.Append32LE(c.code, uint32(rel))

		c.trampolineSyms[localIdx] = trampolineOff
		c.obj.Symbols = append(c.obj.Symbols, object.Symbol{
			Name:   fmt.Sprintf("__cb_%d", localIdx),
			Kind:   object.SymDefined,
			Offset: trampolineOff,
		})
	}
}

// ── Relocation application ────────────────────────────────────────────────────

func (c *moduleCompiler) applyRelocs(numImportFuncs int) error {
	for _, r := range c.relocs {
		if r.funcIdx == -1 {
			// R15 load → __wasm_mem_base
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__wasm_mem_base",
				Kind:   object.RelocRel32,
			})
			continue
		}

		if r.funcIdx == -2 {
			// Lazy-init call → __vertex_memory_init
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_memory_init",
				Kind:   object.RelocRel32,
			})
			continue
		}

		if r.funcIdx == -3 {
			// Handle Table Load → __vertex_handle_table
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_handle_table",
				Kind:   object.RelocRel32,
			})
			continue
		}

		if r.funcIdx == -4 {
			// Handle Count Load (atomic bump) → __vertex_handle_count
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_handle_count",
				Kind:   object.RelocRel32,
			})
			continue
		}

		if r.isAbs64 {
			sym := ""
			if r.funcIdx < numImportFuncs {
				sym = c.importNames[r.funcIdx]
			} else {
				localIdx := r.funcIdx - numImportFuncs
				sym = c.localFuncSymbolName(localIdx)
				if _, ok := c.trampolineSyms[localIdx]; ok {
					sym = fmt.Sprintf("__cb_%d", localIdx)
				}
			}
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff, Symbol: sym, Kind: object.RelocAbs64,
			})
			continue
		}

		if r.funcIdx < numImportFuncs {
			if _, isInlined := c.inlinedImports[r.funcIdx]; isInlined {
				continue
			}
			c.obj.Relocs = append(c.obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: c.importNames[r.funcIdx],
				Kind:   object.RelocRel32,
			})
			continue
		}

		localIdx := r.funcIdx - numImportFuncs
		if localIdx >= len(c.funcOff) {
			return fmt.Errorf("compiler: local func index %d out of range", localIdx)
		}
		rel := int32(c.funcOff[localIdx] - (r.codeOff + 4))
		asm.Put32LE(c.code[r.codeOff:], uint32(rel))
	}
	return nil
}

func (c *moduleCompiler) localFuncSymbolName(localIdx int) string {
	for _, e := range c.m.Exports.Entries {
		if e.Kind == wasm.ExportFunc {
			if int(e.Idx)-int(c.m.Imports.NumFuncs()) == localIdx {
				return e.Name
			}
		}
	}
	return fmt.Sprintf("__local_func_%d", localIdx)
}

func evalConstExprI32(expr wasm.ConstExpr) (int32, error) {
	b := expr.Bytes()
	if len(b) == 0 || b[0] != 0x41 {
		return 0, fmt.Errorf("evalConstExprI32: expected i32.const (0x41), got 0x%02X", b[0])
	}
	return decode.NewReader(b[1:]).ReadS32()
}

func evalConstExprI64(expr wasm.ConstExpr) (int64, error) {
	b := expr.Bytes()
	if len(b) == 0 || b[0] != 0x42 {
		return 0, fmt.Errorf("evalConstExprI64: expected i64.const (0x42), got 0x%02X", b[0])
	}
	return decode.NewReader(b[1:]).ReadSLEB128()
}