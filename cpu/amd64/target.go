// cpu/amd64/target.go
package amd64

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/abi"
	linuxabi "github.com/vertex-language/compiler/abi/linux"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/amd64/asm"
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Target is the amd64 code generation backend.
type Target struct {
	QualifiedSymbols bool
}

func (t *Target) ID() string { return "amd64" }

func (t *Target) Emit(ctx *context.BuildContext, funcIndices []int) error {
	// Snapshot the current text-section length so every offset we emit is
	// absolute within the final object, not relative to our local buffer.
	textBase := ctx.Obj.Text().Len()

	// 1. Write static data segments, globals, and the handle table to .data.
	if err := t.emitDataSegments(ctx); err != nil {
		return err
	}

	// 2. Build the inlined-syscall map and per-import linker symbol names.
	inlined, importNames, numImports, err := t.buildImportInfo(ctx)
	if err != nil {
		return err
	}

	// 3. Register non-inlined imports as undefined external symbols.
	for _, sym := range importNames {
		if sym != "" {
			ctx.Obj.AddSymbol(object.Symbol{Name: sym, Global: true})
		}
	}

	// 4. Compile each function body into a local code buffer.
	var code []byte
	funcOffsets := make(map[int]int) // wasm index → byte offset in code
	var pending []funcReloc

	for _, idx := range funcIndices {
		base := len(code)
		funcOffsets[idx] = base

		funcCode, relocs, err := compileFuncBody(ctx, idx, inlined)
		if err != nil {
			return fmt.Errorf("amd64: function %d: %w", idx, err)
		}
		code = append(code, funcCode...)
		for _, r := range relocs {
			r.codeOff += base
			pending = append(pending, r)
		}
	}

	// 5. Generate callback trampolines for all ref.func abs64 relocs so
	//    native callers can re-enter wasm with correct pointer semantics.
	allTypeIdx := buildTypeIndex(ctx)
	trampolineSyms := t.generateCallbackTrampolines(
		ctx, &code, &pending, funcOffsets, numImports, allTypeIdx,
	)

	// 6. Resolve relocs: patch inline local calls, collect external refs.
	var textRelocs []object.Reloc
	for _, r := range pending {
		absOff := uint32(textBase + r.codeOff)

		switch {
		case r.funcIdx == -1: // mov r15, [rip + __wasm_mem_base]
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: "__wasm_mem_base", Kind: object.RelocPCRel32, Addend: -4,
			})

		case r.funcIdx == -2: // call __vertex_memory_init
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: "__vertex_memory_init", Kind: object.RelocCall32, Addend: -4,
			})

		case r.funcIdx == -3: // lea rcx, [rip + __vertex_handle_table]
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: "__vertex_handle_table", Kind: object.RelocPCRel32, Addend: -4,
			})

		case r.funcIdx == -4: // lea rcx, [rip + __vertex_handle_count]
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: "__vertex_handle_count", Kind: object.RelocPCRel32, Addend: -4,
			})

		case r.isAbs64:
			sym := t.funcSymbolName(ctx, r.funcIdx, numImports, importNames, trampolineSyms)
			if sym == "" {
				continue
			}
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: sym, Kind: object.RelocAbs64,
			})

		case r.funcIdx < numImports:
			if _, isInlined := inlined[r.funcIdx]; isInlined {
				continue
			}
			sym := importNames[r.funcIdx]
			if sym == "" {
				continue
			}
			textRelocs = append(textRelocs, object.Reloc{
				Section: ".text", Offset: absOff,
				Symbol: sym, Kind: object.RelocCall32, Addend: -4,
			})

		default: // local-to-local: patch the displacement inline
			targetOff, ok := funcOffsets[r.funcIdx]
			if !ok {
				return fmt.Errorf("amd64: call to unknown function index %d", r.funcIdx)
			}
			rel := int32(targetOff - (r.codeOff + 4))
			asm.Put32LE(code[r.codeOff:], uint32(rel))
		}
	}

	// 6.5. Generate ELF _start wrapper if "main" is exported
	var mainWasmIdx = -1
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind == wasm.ExportFunc && abi.ParseExport(e.Name).Name == "main" {
			mainWasmIdx = int(e.Idx)
			break
		}
	}

	if mainWasmIdx != -1 {
		startOff := len(code)
		
		// call main
		code = append(code, 0xE8)
		targetOff := funcOffsets[mainWasmIdx]
		rel := int32(targetOff - (len(code) + 4))
		code = asm.Append32LE(code, uint32(rel))
		
		// mov rdi, rax (Use main's i32 return value in RAX as the exit code)
		code = append(code, 0x48, 0x89, 0xC7)
		
		// mov eax, 60 (sys_exit)
		code = append(code, 0xB8, 0x3C, 0x00, 0x00, 0x00)
		
		// syscall
		code = append(code, 0x0F, 0x05)

		// Register _start symbol
		ctx.Obj.AddSymbol(object.Symbol{
			Name: "_start", Section: ".text",
			Offset: uint64(textBase + startOff),
			Global: true, IsFunction: true,
		})
	}

	// 7. Write the compiled code buffer to the text section.
	if _, err := ctx.Obj.Text().Write(code); err != nil {
		return fmt.Errorf("amd64: writing text section: %w", err)
	}

	// 8. Emit all external relocations.
	for _, r := range textRelocs {
		ctx.Obj.AddReloc(r)
	}

	// 9. Emit function symbols (exports first, then unexported locals).
	emittedLocal := make(map[int]bool)
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		off, ok := funcOffsets[int(e.Idx)]
		if !ok {
			continue // routed to a GPU backend
		}
		export := abi.ParseExport(e.Name)
		ctx.Obj.AddSymbol(object.Symbol{
			Name:    export.Name, Section: ".text",
			Offset:  uint64(textBase + off),
			Global: true, IsFunction: true,
		})
		if localIdx := int(e.Idx) - numImports; localIdx >= 0 {
			emittedLocal[localIdx] = true
		}
	}
	for wasmIdx, off := range funcOffsets {
		localIdx := wasmIdx - numImports
		if localIdx < 0 || emittedLocal[localIdx] {
			continue
		}
		ctx.Obj.AddSymbol(object.Symbol{
			Name:       fmt.Sprintf("__local_func_%d", localIdx),
			Section:    ".text",
			Offset:     uint64(textBase + off),
			IsFunction: true,
		})
	}

	// 10. Emit trampoline symbols.
	for localIdx, tramOff := range trampolineSyms {
		ctx.Obj.AddSymbol(object.Symbol{
			Name:       fmt.Sprintf("__cb_%d", localIdx),
			Section:    ".text",
			Offset:     uint64(textBase + tramOff),
			IsFunction: true,
		})
	}

	return nil
}

// ── Import processing ─────────────────────────────────────────────────────────

func (t *Target) buildImportInfo(ctx *context.BuildContext) (
	inlined map[int]inlinedImport, importNames []string, numImports int, err error,
) {
	inlined = make(map[int]inlinedImport)
	funcIdx := 0

	for _, e := range ctx.Module.Imports.Entries {
		if e.Kind != wasm.ImportFunc {
			continue
		}
		route := abi.Parse(e.Module)
		sig := abi.ParseSig(e.Name)

		switch route.Kind {
		case abi.LinuxSyscall:
			if n, ok := linuxabi.SyscallNumber(sig.Name, "amd64"); ok {
				inlined[funcIdx] = inlinedImport{
					module: e.Module, name: sig.Name, number: n, ptrMask: sig.PtrMask,
				}
			}
			importNames = append(importNames, "")

		case abi.LinuxLib:
			importNames = append(importNames, sig.Name)

		case abi.VcpkgLib:
			sym := sig.Name
			if t.QualifiedSymbols {
				sym = e.Module + "::" + sig.Name
			}
			importNames = append(importNames, sym)

		case abi.VertexMemory:
			importNames = append(importNames,
				"__vertex_memory_"+strings.ReplaceAll(sig.Name, ".", "_"))

		case abi.WindowsDLL:
			err = fmt.Errorf("amd64: %s::%s — windows/* imports not valid on this target", e.Module, sig.Name)
			return
		case abi.DarwinLib:
			err = fmt.Errorf("amd64: %s::%s — darwin/* imports not valid on this target", e.Module, sig.Name)
			return
		case abi.BIOSService:
			err = fmt.Errorf("amd64: %s::%s — hw/bios not yet implemented", e.Module, sig.Name)
			return
		case abi.UEFIService:
			err = fmt.Errorf("amd64: %s::%s — hw/uefi not yet implemented", e.Module, sig.Name)
			return
		case abi.GPUIntrinsic:
			err = fmt.Errorf("amd64: %s::%s — gpu/* intrinsics require @cuda/@msl/@vulkan export suffix", e.Module, sig.Name)
			return
		default:
			err = fmt.Errorf("amd64: %s::%s — unrecognised import namespace", e.Module, sig.Name)
			return
		}
		funcIdx++
	}
	numImports = funcIdx
	return
}

// funcSymbolName returns the linker symbol for funcIdx. For local functions
// that have a callback trampoline the trampoline symbol is returned instead,
// since ref.func must point to the ABI-adapting wrapper.
func (t *Target) funcSymbolName(
	ctx *context.BuildContext,
	funcIdx, numImports int,
	importNames []string,
	trampolineSyms map[int]int,
) string {
	if funcIdx < numImports {
		if funcIdx < len(importNames) {
			return importNames[funcIdx]
		}
		return ""
	}
	localIdx := funcIdx - numImports
	if _, ok := trampolineSyms[localIdx]; ok {
		return fmt.Sprintf("__cb_%d", localIdx)
	}
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind == wasm.ExportFunc && int(e.Idx) == funcIdx {
			return abi.ParseExport(e.Name).Name
		}
	}
	return fmt.Sprintf("__local_func_%d", localIdx)
}

// ── Data segment layout ───────────────────────────────────────────────────────

func (t *Target) emitDataSegments(ctx *context.BuildContext) error {
	// Compute the minimum data buffer size.
	var maxDataSize int32
	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, err := evalConstExprI32(active.Offset)
			if err != nil {
				return err
			}
			if end := off + 65536 + int32(len(d.Data)); end > maxDataSize {
				maxDataSize = end
			}
		}
	}
	const shadowStackTop = int32(1048576)
	minMem := shadowStackTop + 65536
	if ctx.Module.Memories.Len() > 0 {
		if declared := int32(ctx.Module.Memories.Entries[0].Lim.Min*65536) + 65536; declared > minMem {
			minMem = declared
		}
	}
	if minMem > maxDataSize {
		maxDataSize = minMem
	}

	// Build the flat data buffer.
	//
	// Layout within the buffer:
	//   [0 .. 65535]        reserved — globals at [65536 − 8*(idx+1)]
	//   [65536 .. max)      wasm linear-memory byte 0; active data segments
	//   [max ..]            handle table (8192 × 8 B) + 8-byte bump counter
	dataBuffer := make([]byte, maxDataSize)

	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			dst := int(off) + 65536
			if dst+len(d.Data) <= len(dataBuffer) {
				copy(dataBuffer[dst:], d.Data)
			}
		}
	}

	for idx, g := range ctx.Module.Globals.Entries {
		slot := 65536 - 8*(idx+1)
		if slot < 0 || slot+8 > len(dataBuffer) {
			return fmt.Errorf("amd64: global %d slot %d out of bounds (buf=%d)", idx, slot, len(dataBuffer))
		}
		switch g.Type.Val {
		case wasm.I32:
			v, err := evalConstExprI32(g.Init)
			if err != nil {
				return fmt.Errorf("amd64: global %d: %w", idx, err)
			}
			binary.LittleEndian.PutUint64(dataBuffer[slot:], uint64(int64(v)))
		case wasm.I64:
			v, err := evalConstExprI64(g.Init)
			if err != nil {
				return fmt.Errorf("amd64: global %d: %w", idx, err)
			}
			binary.LittleEndian.PutUint64(dataBuffer[slot:], uint64(v))
		}
	}

	// Append the handle table.
	// Slot 0 is the NULL sentinel; the bump counter starts at 1.
	handleTableOff := len(dataBuffer)
	handleTableData := make([]byte, 8192*8+8)
	binary.LittleEndian.PutUint32(handleTableData[8192*8:], 1)
	dataBuffer = append(dataBuffer, handleTableData...)

	// Write to the data section and emit symbols.
	dataBase := ctx.Obj.Data().Len()
	if _, err := ctx.Obj.Data().Write(dataBuffer); err != nil {
		return fmt.Errorf("amd64: writing data section: %w", err)
	}

	ctx.Obj.AddSymbol(object.Symbol{
		Name: "__wasm_data_base", Section: ".data",
		Offset: uint64(dataBase), Global: true,
	})
	ctx.Obj.AddSymbol(object.Symbol{
		Name: "__vertex_handle_table", Section: ".data",
		Offset: uint64(dataBase + handleTableOff), Global: true,
	})
	ctx.Obj.AddSymbol(object.Symbol{
		Name: "__vertex_handle_count", Section: ".data",
		Offset: uint64(dataBase + handleTableOff + 8192*8), Global: true,
	})

	return nil
}

// ── Callback trampolines ──────────────────────────────────────────────────────

// generateCallbackTrampolines emits a small stub for each local function that
// is used as a ref.func callback from native code. The stub loads R15, converts
// native pointer arguments back to wasm offsets (sub reg, r15), then
// tail-jumps to the function body.
//
// Returns localIdx → trampoline code offset (within the local code buffer).
func (t *Target) generateCallbackTrampolines(
	ctx *context.BuildContext,
	code *[]byte,
	pending *[]funcReloc,
	funcOffsets map[int]int,
	numImports int,
	allTypeIdx []uint32,
) map[int]int {
	trampolineSyms := make(map[int]int)

	// Iterate over a snapshot — emitting trampolines appends to *pending.
	snapshot := make([]funcReloc, len(*pending))
	copy(snapshot, *pending)

	subR15 := [][3]byte{
		{0x4C, 0x29, 0xFF}, // sub rdi, r15
		{0x4C, 0x29, 0xFE}, // sub rsi, r15
		{0x4C, 0x29, 0xFA}, // sub rdx, r15
		{0x4C, 0x29, 0xF9}, // sub rcx, r15
		{0x4D, 0x29, 0xF8}, // sub r8,  r15
		{0x4D, 0x29, 0xF9}, // sub r9,  r15
	}

	for _, r := range snapshot {
		if !r.isAbs64 || r.funcIdx < numImports {
			continue
		}
		localIdx := r.funcIdx - numImports
		if _, already := trampolineSyms[localIdx]; already {
			continue
		}

		ft := ctx.Module.Types.Entries[allTypeIdx[r.funcIdx]]
		targetOff := funcOffsets[r.funcIdx]
		tramOff := len(*code)

		// Load wasm mem base into R15.
		*code = append(*code, 0x4C, 0x8B, 0x3D)
		*pending = append(*pending, funcReloc{codeOff: len(*code), funcIdx: -1})
		*code = append(*code, 0, 0, 0, 0)

		// Reverse-translate i32 parameters (native ptr → wasm offset).
		nParams := len(ft.Params)
		if nParams > 6 {
			nParams = 6
		}
		for j := 0; j < nParams; j++ {
			if ft.Params[j] == wasm.I32 {
				*code = append(*code, subR15[j][:]...)
			}
		}

		// Tail-jump to the function body (inline-patched displacement).
		*code = append(*code, 0xE9)
		rel := int32(targetOff - (len(*code) + 4))
		*code = asm.Append32LE(*code, uint32(rel))

		trampolineSyms[localIdx] = tramOff
	}

	return trampolineSyms
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// buildTypeIndex constructs a combined type-index slice (imports first, then
// locals) for use when looking up function signatures in trampolines.
func buildTypeIndex(ctx *context.BuildContext) []uint32 {
	var all []uint32
	for _, e := range ctx.Module.Imports.Entries {
		if e.Kind == wasm.ImportFunc {
			all = append(all, e.TypeIdx)
		}
	}
	return append(all, ctx.Module.Functions.TypeIndices...)
}

func evalConstExprI32(expr wasm.ConstExpr) (int32, error) {
	b := expr.Bytes()
	if len(b) == 0 || b[0] != 0x41 {
		return 0, fmt.Errorf("evalConstExprI32: expected i32.const, got 0x%02X", b[0])
	}
	return decode.NewReader(b[1:]).ReadS32()
}

func evalConstExprI64(expr wasm.ConstExpr) (int64, error) {
	b := expr.Bytes()
	if len(b) == 0 || b[0] != 0x42 {
		return 0, fmt.Errorf("evalConstExprI64: expected i64.const, got 0x%02X", b[0])
	}
	return decode.NewReader(b[1:]).ReadSLEB128()
}