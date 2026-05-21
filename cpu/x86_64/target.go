package x86_64

import (
	"fmt"

	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

type Target struct{ QualifiedSymbols bool }

func (t *Target) ID() string { return "amd64" }

func (t *Target) Emit(ctx *context.BuildContext, funcIndices []int) error {
	if len(ctx.Obj.Data) == 0 && (ctx.Module.Datas.Len() > 0 || ctx.Module.Globals.Len() > 0) {
		if err := t.emitDataSegments(ctx); err != nil {
			return err
		}
	}

	var inlined map[int]inlinedImport
	for _, idx := range funcIndices {
		codeBase := len(ctx.Obj.Code)
		code, relocs, err := compileFuncBody(ctx, idx, inlined)
		if err != nil {
			return err
		}
		ctx.Obj.Code = append(ctx.Obj.Code, code...)
		for _, r := range relocs {
			r.codeOff += codeBase
			sym := fmt.Sprintf("__func_%d", r.funcIdx)
			if r.funcIdx == -1 {
				sym = "__wasm_data_base"
			}
			kind := object.RelocRel32
			if r.isAbs64 {
				kind = object.RelocAbs64
			}
			ctx.Obj.Relocs = append(ctx.Obj.Relocs, object.Reloc{
				Offset: r.codeOff, Symbol: sym, Kind: kind,
			})
		}
	}
	return nil
}

func (t *Target) emitDataSegments(ctx *context.BuildContext) error {
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
		if declared > minMemSize { minMemSize = declared }
	}
	if minMemSize > maxDataSize { maxDataSize = minMemSize }
	
	ctx.Obj.Data = make([]byte, maxDataSize)
	for _, d := range ctx.Module.Datas.Entries {
		if active, ok := d.Mode.(wasm.DataModeActive); ok {
			off, _ := evalConstExprI32(active.Offset)
			dst := off + 65536
			copy(ctx.Obj.Data[dst:], d.Data)
		}
	}

	ctx.Obj.Symbols = append(ctx.Obj.Symbols, object.Symbol{
		Name: "__wasm_data_base", Kind: object.SymDefined, Section: object.SymSecData, Offset: 0,
	})
	return nil
}