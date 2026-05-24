package x86_64

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/abi"
	linuxabi "github.com/vertex-language/compiler/abi/linux"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Target is the x86-64 code generation backend.
type Target struct{ QualifiedSymbols bool }

func (t *Target) ID() string { return "amd64" }

func (t *Target) Emit(ctx *context.BuildContext, funcIndices []int) error {
	// 1. Size the data buffer, copy active data segments, lay out globals,
	//    and define __wasm_data_base. Must run before compileFuncBody so
	//    that prologues can emit relocs against it.
	if err := t.emitDataSegments(ctx); err != nil {
		return err
	}

	// 2. Build the inlined-syscall map and the import symbol-name slice.
	inlined, importNames, numImports, err := t.buildImportInfo(ctx)
	if err != nil {
		return err
	}

	// 3. Register every non-inlined import as an undefined symbol so the
	//    linker knows to generate PLT/GOT stubs for them.
	for _, sym := range importNames {
		if sym != "" {
			ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
				Name: sym,
				Kind: object.SymUndefined,
			})
		}
	}

	// 4. Compile each function body, recording its start offset so that
	//    local-to-local calls can be patched inline.
	funcOffsets := make(map[int]int) // wasm index → byte offset in ctx.Obj.Code
	var pending []funcReloc

	for _, idx := range funcIndices {
		codeBase := len(ctx.Obj.Code)
		funcOffsets[idx] = codeBase

		code, relocs, err := compileFuncBody(ctx, idx, inlined)
		if err != nil {
			return fmt.Errorf("x86_64: function %d: %w", idx, err)
		}
		ctx.Obj.Code = append(ctx.Obj.Code, code...)

		for _, r := range relocs {
			r.codeOff += codeBase
			pending = append(pending, r)
		}
	}

	// 5. Apply all relocs now that every function offset is known.
	for _, r := range pending {
		switch {
		case r.funcIdx == -1:
			// R15 prologue load → __wasm_mem_base
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__wasm_mem_base",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -2:
			// Lazy-init call → __vertex_memory_init
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_memory_init",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -3:
			// Handle Table base → __vertex_handle_table
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff,
				Symbol: "__vertex_handle_table",
				Kind:   object.RelocRel32,
			})

		case r.funcIdx == -4:
			// Handle bump counter → __vertex_handle_count
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

	// 6. Emit symbols for every compiled function. Export names take priority;
	//    unexported locals get __local_func_N so ref.func abs64 relocs resolve.
	emittedLocal := make(map[int]bool)
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		off, ok := funcOffsets[int(e.Idx)]
		if !ok {
			continue // routed to a different backend (GPU)
		}
		export := abi.ParseExport(e.Name)
		ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
			Name:    export.Name, // stripped of @-suffix
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
//   - inlined:     funcIdx → inlinedImport for syscalls emitted inline
//   - importNames: linker symbol name per imported function; "" for inlined
//   - numImports:  total imported function count
func (t *Target) buildImportInfo(ctx *context.BuildContext) (map[int]inlinedImport, []string, int, error) {
	inlined := make(map[int]inlinedImport)
	var importNames []string

	funcIdx := 0
	for _, e := range ctx.Module.Imports.Entries {
		if e.Kind != wasm.ImportFunc {
			continue
		}

		route := abi.Parse(e.Module)
		sig := abi.ParseSig(e.Name)

		switch route.Kind {

		case abi.LinuxSyscall:
			n, ok := linuxabi.SyscallNumber(sig.Name, "amd64")
			if ok {
				inlined[funcIdx] = inlinedImport{
					module:  e.Module,
					name:    sig.Name,
					number:  n,
					ptrMask: sig.PtrMask,
				}
			}
			importNames = append(importNames, "") // inlined; reloc skipped

		case abi.LinuxLib:
			// Symbol qualified with the full module path so the linker can
			// distinguish linux/libc::fopen from any other fopen.
			importNames = append(importNames, e.Module+"::"+sig.Name)

		case abi.VcpkgLib:
			// lib/* libraries are resolved by the vcpkg-driven link step.
			// Unqualified by default so the archive's own symbol names match.
			sym := sig.Name
			if t.QualifiedSymbols {
				sym = e.Module + "::" + sig.Name
			}
			importNames = append(importNames, sym)

		case abi.VertexMemory:
			// Resolved to __vertex_memory_* stubs injected by memory.Emit.
			// Dots in allocator sub-names (e.g. "heap.alloc") become underscores.
			sym := "__vertex_memory_" + strings.ReplaceAll(sig.Name, ".", "_")
			importNames = append(importNames, sym)

		// ── Not yet implemented ───────────────────────────────────────────────

		case abi.WindowsDLL:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — windows/* imports are not valid on this target",
				e.Module, sig.Name,
			)

		case abi.DarwinLib:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — darwin/* imports are not valid on this target",
				e.Module, sig.Name,
			)

		case abi.BIOSService:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — hw/bios backend not yet implemented",
				e.Module, sig.Name,
			)

		case abi.UEFIService:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — hw/uefi backend not yet implemented",
				e.Module, sig.Name,
			)

		case abi.GPUIntrinsic:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — gpu/* intrinsics are only valid inside "+
					"@cuda/@msl/@vulkan-exported functions; add the export suffix",
				e.Module, sig.Name,
			)

		default:
			return nil, nil, 0, fmt.Errorf(
				"x86_64: %s::%s — unrecognised import namespace",
				e.Module, sig.Name,
			)
		}

		funcIdx++
	}
	return inlined, importNames, funcIdx, nil
}

// funcSymbolName returns the linker symbol for funcIdx, used when emitting
// abs64 ref.func relocs. Export name takes priority; unexported functions
// fall back to __local_func_N.
func (t *Target) funcSymbolName(ctx *context.BuildContext, funcIdx, numImports int, importNames []string) string {
	if funcIdx < numImports {
		if funcIdx < len(importNames) {
			return importNames[funcIdx]
		}
		return ""
	}
	for _, e := range ctx.Module.Exports.Entries {
		if e.Kind == wasm.ExportFunc && int(e.Idx) == funcIdx {
			return abi.ParseExport(e.Name).Name
		}
	}
	return fmt.Sprintf("__local_func_%d", funcIdx-numImports)
}

// emitDataSegments sizes ctx.Obj.Data to cover the full linear-memory layout,
// copies active data segments in, lays out globals, and defines
// __wasm_data_base. Idempotent: a second call is a no-op.
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

	// Publish the static data byte count for __vertex_memory_init.
	var staticBytes uint32
	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			if off >= 0 {
				if end := uint32(off) + uint32(len(d.Data)); end > staticBytes {
					staticBytes = end
				}
			}
		}
	}
	for _, sym := range ctx.Obj.Symbols {
		if sym.Name == "__wasm_static_bytes" && sym.Section == object.SymSecData {
			if int(sym.Offset)+4 <= len(ctx.Obj.Data) {
				binary.LittleEndian.PutUint32(ctx.Obj.Data[sym.Offset:], staticBytes)
			}
			break
		}
	}

	// Inject the global Handle Table (64 KB / 8,192 slots + 8-byte counter).
	hasHandleTable := false
	for _, s := range ctx.Obj.Symbols {
		if s.Name == "__vertex_handle_table" {
			hasHandleTable = true
			break
		}
	}
	if !hasHandleTable {
		tableOff := len(ctx.Obj.Data)
		ctx.Obj.Data = append(ctx.Obj.Data, make([]byte, 8192*8+8)...)

		ctx.Obj.Symbols = append(ctx.Obj.Symbols,
			object.Symbol{
				Name:    "__vertex_handle_table",
				Kind:    object.SymDefined,
				Section: object.SymSecData,
				Offset:  tableOff,
			},
			object.Symbol{
				Name:    "__vertex_handle_count",
				Kind:    object.SymDefined,
				Section: object.SymSecData,
				Offset:  tableOff + 8192*8,
			},
		)
		// Bump counter starts at 1; index 0 acts as a safe NULL.
		binary.LittleEndian.PutUint32(ctx.Obj.Data[tableOff+8192*8:], 1)
	}

	return nil
}