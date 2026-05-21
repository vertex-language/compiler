package x86_64

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/platform"
	linuxplatform "github.com/vertex-language/compiler/platform/linux"
	"github.com/vertex-language/compiler/wasm"
)

// Target is the x86-64 code generation backend.
type Target struct{ QualifiedSymbols bool }

func (t *Target) ID() string { return "amd64" }

func (t *Target) Emit(ctx *context.BuildContext, funcIndices []int) error {
	// 1. Ensure the data buffer is sized, data segments are copied in, globals
	//    are laid out, and __wasm_data_base is defined.  Must run before any
	//    call to compileFuncBody so that prologues can emit relocs against it.
	//    Also must run before memory/concurrency stubs write into ctx.Obj.Data,
	//    which is why emitDataSegments is idempotent and grows (not overwrites).
	if err := t.emitDataSegments(ctx); err != nil {
		return err
	}

	// 2. Build the inlinedImports map (syscalls → inline) and the importNames
	//    slice (all other imports → linker symbol name).
	//    Bug 1 fix: the original code passed nil here, so syscall imports were
	//    never resolved as inline and fell through to the native call path,
	//    generating "__func_N" relocs against symbols that were never defined.
	inlined, importNames, numImports := t.buildImportInfo(ctx)

	// 3. Compile each function body, tracking where in ctx.Obj.Code each
	//    function lands so that local-to-local calls can be patched inline.
	funcOffsets := make(map[int]int) // wasm function index → byte offset in ctx.Obj.Code
	var pending []funcReloc          // all relocs, with codeOff adjusted to global position

	for _, idx := range funcIndices {
		codeBase := len(ctx.Obj.Code)
		funcOffsets[idx] = codeBase

		code, relocs, err := compileFuncBody(ctx, idx, inlined)
		if err != nil {
			return fmt.Errorf("x86_64: function %d: %w", idx, err)
		}
		ctx.Obj.Code = append(ctx.Obj.Code, code...)

		for _, r := range relocs {
			r.codeOff += codeBase // make offset global within ctx.Obj.Code
			pending = append(pending, r)
		}
	}

	// 4. Apply relocs now that all function offsets are known.
	for _, r := range pending {
		switch {
		case r.funcIdx == -1:
			// Sentinel emitted by the function prologue for the R15 (MemBase)
			// load.  Always resolves to __wasm_data_base.
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__wasm_data_base",
				Kind:   object.RelocRel32,
			})

		case r.isAbs64:
			// ref.func: 64-bit absolute address of a function.
			sym := t.funcSymbolName(ctx, r.funcIdx, numImports, importNames)
			if sym == "" {
				continue
			}
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: sym,
				Kind:   object.RelocAbs64,
			})

		case r.funcIdx < numImports:
			// Import call.
			if _, isInlined := inlined[r.funcIdx]; isInlined {
				continue // syscall was inlined; the E8 placeholder is dead code
			}
			sym := importNames[r.funcIdx]
			if sym == "" {
				continue
			}
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: sym,
				Kind:   object.RelocRel32,
			})

		default:
			// Local-to-local direct call: patch the 32-bit PC-relative
			// displacement inline — no linker reloc needed.
			targetOff, ok := funcOffsets[r.funcIdx]
			if !ok {
				return fmt.Errorf("x86_64: call to unknown function index %d", r.funcIdx)
			}
			rel := int32(targetOff - (r.codeOff + 4))
			asm.Put32LE(ctx.Obj.Code[r.codeOff:], uint32(rel))
		}
	}

	// 5. Emit symbols for every compiled function.
	//    Export names take priority; unexported locals get a __local_func_N name
	//    so that abs64 ref.func relocs can resolve them via the linker.
	emittedLocal := make(map[int]bool)
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		off, ok := funcOffsets[int(e.Idx)]
		if !ok {
			continue // function was routed to a different backend (e.g. GPU)
		}
		ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
			Name:    e.Name,
			Kind:    object.SymDefined,
			Section: object.SymSecText,
			Offset:  off,
		})
		localIdx := int(e.Idx) - numImports
		if localIdx >= 0 {
			emittedLocal[localIdx] = true
		}
	}
	for wasmIdx, off := range funcOffsets {
		localIdx := wasmIdx - numImports
		if localIdx < 0 || emittedLocal[localIdx] {
			continue
		}
		ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
			Name:    fmt.Sprintf("__local_func_%d", localIdx),
			Kind:    object.SymDefined,
			Section: object.SymSecText,
			Offset:  off,
		})
	}

	return nil
}

// buildImportInfo walks the module's import section and returns:
//   - inlined: map from funcIdx → inlinedImport for syscalls that should be
//     emitted inline rather than via a call instruction.
//   - importNames: parallel slice of linker symbol names; empty string for
//     inlined imports (the reloc is skipped at apply time).
//   - numImports: total number of imported functions.
func (t *Target) buildImportInfo(ctx *context.BuildContext) (map[int]inlinedImport, []string, int) {
	inlined := make(map[int]inlinedImport)
	var importNames []string

	funcIdx := 0
	for _, e := range ctx.Module.Imports.Entries {
		if e.Kind != wasm.ImportFunc {
			continue
		}

		route := platform.Parse(e.Module)
		realName, ptrMask := parseImportSig(e.Name)

		switch {
		case e.Module == "memory":
			// Resolved by the allocator stubs injected by memory.Emit.
			// The symbol name mirrors what memory.Emit defines.
			sym := "__vertex_memory_" + strings.ReplaceAll(realName, ".", "_")
			importNames = append(importNames, sym)

		case route.Kind == platform.SyscallTrampoline:
			// Emit the syscall inline; record an empty name so the call-site
			// reloc is skipped cleanly.
			n, ok := linuxplatform.SyscallNumber(realName, "amd64")
			if ok {
				inlined[funcIdx] = inlinedImport{
					module:  e.Module,
					name:    realName,
					number:  n,
					ptrMask: ptrMask,
				}
			}
			importNames = append(importNames, "") // reloc will be skipped

		case route.Kind == platform.PlatformLib:
			importNames = append(importNames, e.Module+"::"+realName)

		default: // CrossPlatformLib or any bare shared library
			sym := realName
			if t.QualifiedSymbols {
				sym = e.Module + "::" + realName
			}
			importNames = append(importNames, sym)
		}
		funcIdx++
	}
	return inlined, importNames, funcIdx
}

// funcSymbolName returns the linker symbol name for funcIdx, used when
// emitting abs64 ref.func relocs.  For local functions the export name is
// preferred; unexported functions fall back to __local_func_N.
func (t *Target) funcSymbolName(ctx *context.BuildContext, funcIdx, numImports int, importNames []string) string {
	if funcIdx < numImports {
		if funcIdx < len(importNames) {
			return importNames[funcIdx]
		}
		return ""
	}
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind == wasm.ExportFunc && int(e.Idx) == funcIdx {
			return e.Name
		}
	}
	return fmt.Sprintf("__local_func_%d", funcIdx-numImports)
}

// emitDataSegments sizes ctx.Obj.Data to cover the full linear-memory layout,
// copies active data segments into it, lays globals into the reserved 64 KiB
// header, and defines the __wasm_data_base anchor symbol.
//
// Idempotent: returns immediately if __wasm_data_base is already present.
// Additive: grows ctx.Obj.Data rather than overwriting it, so bytes written
// by memory.Emit or concurrency.Emit before this call are preserved.
// Bug 2 fix: the original guard (len(ctx.Obj.Data) == 0) caused this function
// to be skipped when memory.Emit had already written into ctx.Obj.Data, leaving
// __wasm_data_base undefined and failing every reloc that referenced it.
func (t *Target) emitDataSegments(ctx *context.BuildContext) error {
	// Idempotency: if a prior Emit call or the memory/concurrency injector
	// already defined __wasm_data_base, there is nothing to do.
	for _, s := range ctx.Obj.Symbols {
		if s.Name == "__wasm_data_base" {
			return nil
		}
	}

	// Compute the minimum required size of the data buffer.
	var maxDataSize int32
	for _, d := range ctx.Module.Datas.Entries {
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
	if ctx.Module.Memories.Len() > 0 {
		declared := int32(ctx.Module.Memories.Entries[0].Lim.Min*65536) + 65536
		if declared > minMemSize {
			minMemSize = declared
		}
	}
	if minMemSize > maxDataSize {
		maxDataSize = minMemSize
	}

	// Grow (never shrink or clobber) ctx.Obj.Data.
	if int(maxDataSize) > len(ctx.Obj.Data) {
		grown := make([]byte, maxDataSize)
		copy(grown, ctx.Obj.Data) // preserve anything memory.Emit wrote
		ctx.Obj.Data = grown
	}

	// Copy active data segments into the buffer.
	// Each segment lives at (wasm offset + 65536) because the first 64 KiB
	// of the native data section is reserved for the global-variable slots
	// and the shadow stack.
	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			dst := int(off) + 65536
			if dst+len(d.Data) <= len(ctx.Obj.Data) {
				copy(ctx.Obj.Data[dst:], d.Data)
			}
		}
	}

	// Lay global initial values into the reserved header (slots at
	// 65536 - 8*(idx+1), growing downward from the 64 KiB boundary).
	for idx, g := range ctx.Module.Globals.Entries {
		slot := 65536 - 8*(idx+1)
		if slot < 0 || slot+8 > len(ctx.Obj.Data) {
			return fmt.Errorf("x86_64: global %d slot %d out of data buffer bounds (%d bytes)",
				idx, slot, len(ctx.Obj.Data))
		}
		switch g.Type.Val {
		case wasm.I32:
			iv, err := evalConstExprI32(g.Init)
			if err != nil {
				return fmt.Errorf("x86_64: global %d (i32): %w", idx, err)
			}
			binary.LittleEndian.PutUint64(ctx.Obj.Data[slot:], uint64(int64(iv)))
		case wasm.I64:
			iv, err := evalConstExprI64(g.Init)
			if err != nil {
				return fmt.Errorf("x86_64: global %d (i64): %w", idx, err)
			}
			binary.LittleEndian.PutUint64(ctx.Obj.Data[slot:], uint64(iv))
		}
	}

	// Define the anchor symbol.  All per-function prologues emit a
	// RelocRel32 against this name so the linker can fill in the
	//   lea r15, [rip + __wasm_data_base]
	// displacement at link time.
	ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
		Name:    "__wasm_data_base",
		Kind:    object.SymDefined,
		Section: object.SymSecData,
		Offset:  0,
	})
	return nil
}