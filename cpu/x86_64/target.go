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

	// ── FIX: Explicitly register external imports as Undefined Symbols ──
	// This ensures the linker knows these are external dependencies, triggering
	// buildDynamic() to generate the necessary PLT/GOT stubs.
	for _, sym := range importNames {
		if sym != "" {
			ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
				Name: sym,
				Kind: object.SymUndefined,
			})
		}
	}
	// ────────────────────────────────────────────────────────────────────

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
			// R15 load — resolves to __wasm_mem_base (the mmap'd wasm base).
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__wasm_mem_base",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -2:
			// Lazy-init call — resolves to __vertex_memory_init.
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_memory_init",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -3:
			// Handle Table Load — resolves to __vertex_handle_table.
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_handle_table",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -4:
			// Handle Count Load — resolves to __vertex_handle_count.
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_handle_count",
				Kind:   object.RelocRel32,
			})

		case r.isAbs64:
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
			if _, isInlined := inlined[r.funcIdx]; isInlined {
				continue
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
		realName, ptrMask, _, _ := parseImportSig(e.Name)

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
func (t *Target) emitDataSegments(ctx *context.BuildContext) error {
	for _, s := range ctx.Obj.Symbols {
		if s.Name == "__wasm_data_base" {
			return nil
		}
	}

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

	if int(maxDataSize) > len(ctx.Obj.Data) {
		grown := make([]byte, maxDataSize)
		copy(grown, ctx.Obj.Data)
		ctx.Obj.Data = grown
	}

	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			dst := int(off) + 65536
			if dst+len(d.Data) <= len(ctx.Obj.Data) {
				copy(ctx.Obj.Data[dst:], d.Data)
			}
		}
	}

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

	ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
		Name:    "__wasm_data_base",
		Kind:    object.SymDefined,
		Section: object.SymSecData,
		Offset:  0,
	})

	// ── Publish static data size for __vertex_memory_init ─────────────────────
	// Compute the maximum wasm byte offset covered by active data segments; this
	// is the exact number of bytes emitInit needs to rep-movsb from .data into
	// the freshly mmap'd wasm address space.
	var staticBytes uint32
	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			if off >= 0 {
				end := uint32(off) + uint32(len(d.Data))
				if end > staticBytes {
					staticBytes = end
				}
			}
		}
	}
	// Find the __wasm_static_bytes slot (defined by memory.Emit) and write the
	// value directly into ctx.Obj.Data now that we know the final size.
	for _, sym := range ctx.Obj.Symbols {
		if sym.Name == "__wasm_static_bytes" && sym.Section == object.SymSecData {
			if int(sym.Offset)+4 <= len(ctx.Obj.Data) {
				binary.LittleEndian.PutUint32(ctx.Obj.Data[sym.Offset:], staticBytes)
			}
			break
		}
	}

	// ── NEW: Inject the Global Handle Table ───────────────────────────────────
	// 64 KB table (8,192 slots) + 8-byte atomic bump counter
	hasHandleTable := false
	for _, s := range ctx.Obj.Symbols {
		if s.Name == "__vertex_handle_table" {
			hasHandleTable = true
			break
		}
	}
	if !hasHandleTable {
		handleTableOff := len(ctx.Obj.Data)
		ctx.Obj.Data = append(ctx.Obj.Data, make([]byte, 8192*8+8)...)

		ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
			Name:    "__vertex_handle_table",
			Kind:    object.SymDefined,
			Section: object.SymSecData,
			Offset:  handleTableOff,
		})
		ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
			Name:    "__vertex_handle_count",
			Kind:    object.SymDefined,
			Section: object.SymSecData,
			Offset:  handleTableOff + 8192*8,
		})
		// Initialize the bump counter to 1 (so handle index 0 acts as a safe NULL)
		binary.LittleEndian.PutUint32(ctx.Obj.Data[handleTableOff+8192*8:], 1)
	}

	return nil
}