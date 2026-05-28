// cpu/arm64/target.go
package arm64

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/abi"
	linuxabi "github.com/vertex-language/compiler/abi/linux"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/arm64/asm"
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Target is the arm64 (AArch64) code generation backend.
type Target struct {
	QualifiedSymbols bool
}

func (t *Target) ID() string { return "arm64" }

func (t *Target) Emit(ctx *context.BuildContext, funcIndices []int) error {
	textBase := ctx.Obj.Text().Len()

	if err := t.emitDataSegments(ctx); err != nil {
		return err
	}

	inlined, importNames, numImports, err := t.buildImportInfo(ctx)
	if err != nil {
		return err
	}

	// Register non-inlined imports as undefined external symbols.
	for _, sym := range importNames {
		if sym != "" {
			ctx.Obj.AddSymbol(object.Symbol{Name: sym, Global: true})
		}
	}

	// Compile each function body.
	var code []byte
	funcOffsets := make(map[int]int)
	var pending []funcReloc

	for _, idx := range funcIndices {
		base := len(code)
		funcOffsets[idx] = base

		funcCode, relocs, err := compileFuncBody(ctx, idx, inlined)
		if err != nil {
			return fmt.Errorf("arm64: function %d: %w", idx, err)
		}
		code = append(code, funcCode...)
		for _, r := range relocs {
			r.codeOff += base
			pending = append(pending, r)
		}
	}

	// Generate callback trampolines for ref.func references.
	allTypeIdx := buildTypeIndex(ctx)
	trampolineSyms := t.generateCallbackTrampolines(
		ctx, &code, &pending, funcOffsets, numImports, allTypeIdx,
	)

	// Resolve relocations.
	var textRelocs []object.Reloc
	for _, r := range pending {
		absOff := uint32(textBase + r.codeOff)

		switch r.kind {
		case rLoadSym:
			// ADRP at codeOff; LDR at codeOff+4.
			sym := sentinelSym(r.funcIdx)
			if sym == "" {
				continue
			}
			textRelocs = append(textRelocs,
				object.Reloc{Section: ".text", Offset: absOff, Symbol: sym,
					Kind: RelocARM64ADRPHi21},
				object.Reloc{Section: ".text", Offset: absOff + 4, Symbol: sym,
					Kind: RelocARM64Ld64Lo12},
			)

		case rAddrSym:
			// ADRP at codeOff; ADD at codeOff+4.
			sym := sentinelSym(r.funcIdx)
			if sym == "" {
				continue
			}
			textRelocs = append(textRelocs,
				object.Reloc{Section: ".text", Offset: absOff, Symbol: sym,
					Kind: RelocARM64ADRPHi21},
				object.Reloc{Section: ".text", Offset: absOff + 4, Symbol: sym,
					Kind: RelocARM64AddLo12},
			)

		case rAddrFunc:
			// ADRP at codeOff; ADD at codeOff+4 for ref.func.
			sym := t.funcSymbolName(ctx, r.funcIdx, numImports, importNames, trampolineSyms)
			if sym == "" {
				continue
			}
			textRelocs = append(textRelocs,
				object.Reloc{Section: ".text", Offset: absOff, Symbol: sym,
					Kind: RelocARM64ADRPHi21},
				object.Reloc{Section: ".text", Offset: absOff + 4, Symbol: sym,
					Kind: RelocARM64AddLo12},
			)

		case rCall:
			if r.funcIdx < 0 {
				// Call to a runtime stub.
				sym := sentinelSym(r.funcIdx)
				if sym == "" {
					continue
				}
				textRelocs = append(textRelocs, object.Reloc{
					Section: ".text", Offset: absOff, Symbol: sym,
					Kind: RelocARM64Call26,
				})
			} else if r.funcIdx < numImports {
				sym := importNames[r.funcIdx]
				if sym == "" {
					continue
				}
				textRelocs = append(textRelocs, object.Reloc{
					Section: ".text", Offset: absOff, Symbol: sym,
					Kind: RelocARM64Call26,
				})
			} else {
				// Local-to-local call: patch the BL displacement inline.
				targetOff, ok := funcOffsets[r.funcIdx]
				if !ok {
					return fmt.Errorf("arm64: call to unknown function index %d", r.funcIdx)
				}
				// Inline patch: delta in 4-byte instruction units.
				delta := int32((targetOff - r.codeOff) / 4)
				insn := uint32(code[r.codeOff]) | uint32(code[r.codeOff+1])<<8 |
					uint32(code[r.codeOff+2])<<16 | uint32(code[r.codeOff+3])<<24
				insn = (insn &^ 0x03FFFFFF) | asm.Imm26Field(delta)
				asm.Put32LE(code[r.codeOff:], insn)
			}
		}
	}

	// Write compiled code.
	if _, err := ctx.Obj.Text().Write(code); err != nil {
		return fmt.Errorf("arm64: writing text section: %w", err)
	}
	for _, r := range textRelocs {
		ctx.Obj.AddReloc(r)
	}

	// Emit function symbols.
	emittedLocal := make(map[int]bool)
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		off, ok := funcOffsets[int(e.Idx)]
		if !ok {
			continue
		}
		export := abi.ParseExport(e.Name)
		ctx.Obj.AddSymbol(object.Symbol{
			Name: export.Name, Section: ".text",
			Offset: uint64(textBase + off), Global: true, IsFunction: true,
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

// sentinelSym maps negative funcIdx sentinels to their linker symbol names.
func sentinelSym(funcIdx int) string {
	switch funcIdx {
	case -1:
		return "__wasm_mem_base"
	case -2:
		return "__vertex_memory_init"
	case -3:
		return "__vertex_handle_table"
	case -4:
		return "__vertex_handle_count"
	}
	return ""
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
		case abi.LinuxKernelSyscall:
			if n, ok := linuxabi.SyscallNumber(sig.Name, "arm64"); ok {
				inlined[funcIdx] = inlinedImport{
					module: e.Module, name: sig.Name, number: n, ptrMask: sig.PtrMask,
				}
			}
			importNames = append(importNames, "")

		case abi.LinuxSystemLib:
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

		case abi.WindowsSystemLib:
			err = fmt.Errorf("arm64: %s::%s — windows/* imports not valid on this target", e.Module, sig.Name)
			return
		case abi.DarwinSystemLib:
			// Darwin/arm64 is a valid combination but handled by a separate target.
			err = fmt.Errorf("arm64: %s::%s — darwin/* imports require the darwin/arm64 target", e.Module, sig.Name)
			return
		case abi.MetalBIOS:
			err = fmt.Errorf("arm64: %s::%s — hw/bios not yet implemented", e.Module, sig.Name)
			return
		case abi.MetalUEFI:
			err = fmt.Errorf("arm64: %s::%s — hw/uefi not yet implemented", e.Module, sig.Name)
			return
		case abi.GPUIntrinsic:
			err = fmt.Errorf("arm64: %s::%s — gpu/* intrinsics require @cuda/@msl/@vulkan export suffix", e.Module, sig.Name)
			return
		default:
			err = fmt.Errorf("arm64: %s::%s — unrecognised import namespace", e.Module, sig.Name)
			return
		}
		funcIdx++
	}
	numImports = funcIdx
	return
}

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
// Identical to the amd64 layout: the .data section is architecture-neutral.

func (t *Target) emitDataSegments(ctx *context.BuildContext) error {
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
			return fmt.Errorf("arm64: global %d slot %d out of bounds (buf=%d)", idx, slot, len(dataBuffer))
		}
		switch g.Type.Val {
		case wasm.I32:
			v, err := evalConstExprI32(g.Init)
			if err != nil {
				return fmt.Errorf("arm64: global %d: %w", idx, err)
			}
			binary.LittleEndian.PutUint64(dataBuffer[slot:], uint64(int64(v)))
		case wasm.I64:
			v, err := evalConstExprI64(g.Init)
			if err != nil {
				return fmt.Errorf("arm64: global %d: %w", idx, err)
			}
			binary.LittleEndian.PutUint64(dataBuffer[slot:], uint64(v))
		}
	}

	handleTableOff := len(dataBuffer)
	handleTableData := make([]byte, 8192*8+8)
	binary.LittleEndian.PutUint32(handleTableData[8192*8:], 1) // bump counter starts at 1
	dataBuffer = append(dataBuffer, handleTableData...)

	dataBase := ctx.Obj.Data().Len()
	if _, err := ctx.Obj.Data().Write(dataBuffer); err != nil {
		return fmt.Errorf("arm64: writing data section: %w", err)
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

// generateCallbackTrampolines emits a stub for each local function referenced
// via ref.func.  The stub loads X28 (MemBase), subtracts it from any i32
// pointer parameters to convert native addresses back to wasm offsets, then
// tail-branches to the function body.
func (t *Target) generateCallbackTrampolines(
	ctx *context.BuildContext,
	code *[]byte,
	pending *[]funcReloc,
	funcOffsets map[int]int,
	numImports int,
	allTypeIdx []uint32,
) map[int]int {
	trampolineSyms := make(map[int]int)

	snapshot := make([]funcReloc, len(*pending))
	copy(snapshot, *pending)

	for _, r := range snapshot {
		if r.kind != rAddrFunc || r.funcIdx < numImports {
			continue
		}
		localIdx := r.funcIdx - numImports
		if _, already := trampolineSyms[localIdx]; already {
			continue
		}

		ft := ctx.Module.Types.Entries[allTypeIdx[r.funcIdx]]
		targetOff := funcOffsets[r.funcIdx]
		tramOff := len(*code)

		// ── Load MemBase into X28 ─────────────────────────────────────────────
		// adrp x9, __wasm_mem_base   [reloc]
		// ldr  x28, [x9]             [reloc]
		adrpOff := len(*code)
		*code = append(*code, 0, 0, 0, 0) // ADRP X9 placeholder
		asm.Put32LE((*code)[adrpOff:], 0x90000009) // adrp x9, 0
		*pending = append(*pending, funcReloc{codeOff: adrpOff + tramOff - tramOff, kind: rLoadSym, funcIdx: -1})
		// Note: codeOff is relative to the start of *code (not textBase).
		// Correct: adrpOff is already relative to *code start.
		(*pending)[len(*pending)-1].codeOff = adrpOff

		*code = append(*code, 0, 0, 0, 0) // LDR X28 placeholder
		ldrOff := len(*code) - 4
		asm.Put32LE((*code)[ldrOff:], 0xF9400009|(uint32(asm.MemBase)&0x1F)) // ldr x28, [x9]
		// The rLoadSym reloc covers both instructions (ADRP at adrpOff, LDR at adrpOff+4).

		// ── Reverse-translate i32 pointer parameters ──────────────────────────
		nParams := len(ft.Params)
		if nParams > 8 {
			nParams = 8
		}
		for j := 0; j < nParams; j++ {
			if ft.Params[j] == wasm.I32 {
				// sub xArg, xArg, x28  (native ptr → wasm offset)
				var a asm.Assembler
				a.SUB(asm.ArgRegs[j], asm.ArgRegs[j], asm.MemBase)
				*code = append(*code, a.Bytes()...)
			}
		}

		// ── Tail-branch to function body ──────────────────────────────────────
		blOff := len(*code)
		*code = append(*code, 0, 0, 0, 0) // B placeholder
		asm.Put32LE((*code)[blOff:], 0x14000000) // B #0
		// Patch inline: delta from this B to targetOff.
		delta := int32((targetOff - blOff) / 4)
		insn := uint32(0x14000000) | asm.Imm26Field(delta)
		asm.Put32LE((*code)[blOff:], insn)

		trampolineSyms[localIdx] = tramOff
	}

	return trampolineSyms
}

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