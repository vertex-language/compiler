// Package encoder serialises a wasm.Module into a valid WebAssembly binary.
package encoder

import (
	"errors"

	"github.com/vertex-language/compiler/encode"
	"github.com/vertex-language/compiler/wasm"
)

var (
	wasmMagic   = [4]byte{0x00, 0x61, 0x73, 0x6D}
	wasmVersion = [4]byte{0x01, 0x00, 0x00, 0x00}
)

// Encode serialises m into a valid WebAssembly binary.
// Returns an error if structural invariants are violated (e.g. function/code
// section length mismatch).
func Encode(m *wasm.Module) ([]byte, error) {
	if m.Functions.Len() != m.Codes.Len() {
		return nil, errors.New(
			"encoder: Functions.Len() != Codes.Len() — " +
				"each function declaration must have exactly one code body",
		)
	}

	out := make([]byte, 0, 256)
	out = append(out, wasmMagic[:]...)
	out = append(out, wasmVersion[:]...)

	out = append(out, encodeTypeSection(m.Types)...)
	out = append(out, encodeImportSection(m.Imports)...)
	out = append(out, encodeFunctionSection(m.Functions)...)
	out = append(out, encodeTableSection(m.Tables)...)
	out = append(out, encodeMemorySection(m.Memories)...)
	out = append(out, encodeGlobalSection(m.Globals)...)
	out = append(out, encodeExportSection(m.Exports)...)

	if m.Start != nil {
		out = append(out, encodeSection(8, encode.AppendU32(nil, *m.Start))...)
	}

	out = append(out, encodeElementSection(m.Elements)...)

	if m.Datas.Len() > 0 {
		out = append(out, encodeSection(12, encode.AppendU32(nil, uint32(m.Datas.Len())))...)
	}

	out = append(out, encodeCodeSection(m.Codes)...)
	out = append(out, encodeDataSection(m.Datas)...)

	for _, cs := range m.Customs {
		out = append(out, encodeCustomSection(cs)...)
	}

	return out, nil
}

// ── Section framing ───────────────────────────────────────────────────────────

func encodeSection(id byte, body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	out := make([]byte, 0, 1+encode.ULEBSize(uint64(len(body)))+len(body))
	out = append(out, id)
	out = encode.AppendU32(out, uint32(len(body)))
	return append(out, body...)
}

// ── Type helpers ──────────────────────────────────────────────────────────────

func encodeLimits(dst []byte, l wasm.Limits) []byte {
	if l.Max == nil {
		dst = append(dst, 0x00)
		return encode.AppendU32(dst, l.Min)
	}
	dst = append(dst, 0x01)
	dst = encode.AppendU32(dst, l.Min)
	return encode.AppendU32(dst, *l.Max)
}

func encodeTableType(dst []byte, t wasm.TableType) []byte {
	dst = append(dst, byte(t.Element))
	return encodeLimits(dst, t.Lim)
}

func encodeMemoryType(dst []byte, m wasm.MemoryType) []byte {
	if m.Shared {
		max := uint32(0)
		if m.Lim.Max != nil {
			max = *m.Lim.Max
		}
		dst = append(dst, 0x02)
		dst = encode.AppendU32(dst, m.Lim.Min)
		return encode.AppendU32(dst, max)
	}
	return encodeLimits(dst, m.Lim)
}

func encodeGlobalType(dst []byte, g wasm.GlobalType) []byte {
	dst = append(dst, byte(g.Val))
	if g.Mutable {
		return append(dst, 0x01)
	}
	return append(dst, 0x00)
}

// ── Sections ──────────────────────────────────────────────────────────────────

func encodeTypeSection(s wasm.TypeSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, ft := range s.Entries {
		body = append(body, 0x60)
		body = encode.AppendVecHeader(body, uint32(len(ft.Params)))
		for _, p := range ft.Params {
			body = append(body, byte(p))
		}
		body = encode.AppendVecHeader(body, uint32(len(ft.Results)))
		for _, r := range ft.Results {
			body = append(body, byte(r))
		}
	}
	return encodeSection(1, body)
}

func encodeImportSection(s wasm.ImportSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, e := range s.Entries {
		body = encode.AppendString(body, e.Module)
		body = encode.AppendString(body, e.Name)
		body = append(body, byte(e.Kind))
		switch e.Kind {
		case wasm.ImportFunc:
			body = encode.AppendU32(body, e.TypeIdx)
		case wasm.ImportTable:
			body = encodeTableType(body, e.Table)
		case wasm.ImportMem:
			body = encodeMemoryType(body, e.Mem)
		case wasm.ImportGlobal:
			body = encodeGlobalType(body, e.Global)
		}
	}
	return encodeSection(2, body)
}

func encodeFunctionSection(s wasm.FunctionSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, ti := range s.TypeIndices {
		body = encode.AppendU32(body, ti)
	}
	return encodeSection(3, body)
}

func encodeTableSection(s wasm.TableSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, tt := range s.Entries {
		body = encodeTableType(body, tt)
	}
	return encodeSection(4, body)
}

func encodeMemorySection(s wasm.MemorySection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, mt := range s.Entries {
		body = encodeMemoryType(body, mt)
	}
	return encodeSection(5, body)
}

func encodeGlobalSection(s wasm.GlobalSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, e := range s.Entries {
		body = encodeGlobalType(body, e.Type)
		body = append(body, e.Init.Bytes()...)
	}
	return encodeSection(6, body)
}

func encodeExportSection(s wasm.ExportSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, e := range s.Entries {
		body = encode.AppendString(body, e.Name)
		body = append(body, byte(e.Kind))
		body = encode.AppendU32(body, e.Idx)
	}
	return encodeSection(7, body)
}

func encodeElementSection(s wasm.ElementSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, seg := range s.Entries {
		body = appendElemSegment(body, seg)
	}
	return encodeSection(9, body)
}

// appendElemSegment encodes one element segment using the 8 canonical formats
// defined in the Wasm 2.0 binary spec (§ 5.5.13).
func appendElemSegment(dst []byte, seg wasm.ElemSegment) []byte {
	appendFuncIndices := func(d []byte, indices wasm.ElemFuncIndices) []byte {
		d = encode.AppendVecHeader(d, uint32(len(indices)))
		for _, fi := range indices {
			d = encode.AppendU32(d, fi)
		}
		return d
	}
	appendExprs := func(d []byte, exprs []wasm.ConstExpr) []byte {
		d = encode.AppendVecHeader(d, uint32(len(exprs)))
		for _, e := range exprs {
			d = append(d, e.Bytes()...)
		}
		return d
	}

	switch items := seg.Items.(type) {
	case wasm.ElemFuncIndices:
		switch seg.Mode {
		case wasm.ElemModeActive:
			if seg.TableIdx == 0 {
				dst = encode.AppendU32(dst, 0)
				dst = append(dst, seg.Offset.Bytes()...)
				dst = appendFuncIndices(dst, items)
			} else {
				dst = encode.AppendU32(dst, 2)
				dst = encode.AppendU32(dst, seg.TableIdx)
				dst = append(dst, seg.Offset.Bytes()...)
				dst = append(dst, 0x00)
				dst = appendFuncIndices(dst, items)
			}
		case wasm.ElemModePassive:
			dst = encode.AppendU32(dst, 1)
			dst = append(dst, 0x00)
			dst = appendFuncIndices(dst, items)
		case wasm.ElemModeDeclarative:
			dst = encode.AppendU32(dst, 3)
			dst = append(dst, 0x00)
			dst = appendFuncIndices(dst, items)
		}

	case wasm.ElemExpressions:
		switch seg.Mode {
		case wasm.ElemModeActive:
			if seg.TableIdx == 0 && items.RefType == wasm.FuncRef {
				dst = encode.AppendU32(dst, 4)
				dst = append(dst, seg.Offset.Bytes()...)
				dst = appendExprs(dst, items.Exprs)
			} else {
				dst = encode.AppendU32(dst, 6)
				dst = encode.AppendU32(dst, seg.TableIdx)
				dst = append(dst, seg.Offset.Bytes()...)
				dst = append(dst, byte(items.RefType))
				dst = appendExprs(dst, items.Exprs)
			}
		case wasm.ElemModePassive:
			dst = encode.AppendU32(dst, 5)
			dst = append(dst, byte(items.RefType))
			dst = appendExprs(dst, items.Exprs)
		case wasm.ElemModeDeclarative:
			dst = encode.AppendU32(dst, 7)
			dst = append(dst, byte(items.RefType))
			dst = appendExprs(dst, items.Exprs)
		}
	}
	return dst
}

func encodeCodeSection(s wasm.CodeSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, fb := range s.Bodies {
		raw := encodeFunctionBody(fb)
		body = encode.AppendBytes(body, raw)
	}
	return encodeSection(10, body)
}

func encodeFunctionBody(fb *wasm.FunctionBody) []byte {
	out := encode.AppendVecHeader(nil, uint32(len(fb.Locals())))
	for _, lg := range fb.Locals() {
		out = encode.AppendU32(out, lg.Count)
		out = append(out, byte(lg.Type))
	}
	return append(out, fb.Code()...)
}

func encodeDataSection(s wasm.DataSection) []byte {
	if s.Len() == 0 {
		return nil
	}
	body := encode.AppendVecHeader(nil, uint32(s.Len()))
	for _, e := range s.Entries {
		switch m := e.Mode.(type) {
		case wasm.DataModePassive:
			body = encode.AppendU32(body, 1)
			body = encode.AppendBytes(body, e.Data)
		case wasm.DataModeActive:
			if m.MemIdx == 0 {
				body = encode.AppendU32(body, 0)
				body = append(body, m.Offset.Bytes()...)
				body = encode.AppendBytes(body, e.Data)
			} else {
				body = encode.AppendU32(body, 2)
				body = encode.AppendU32(body, m.MemIdx)
				body = append(body, m.Offset.Bytes()...)
				body = encode.AppendBytes(body, e.Data)
			}
		}
	}
	return encodeSection(11, body)
}

func encodeCustomSection(cs wasm.CustomSection) []byte {
	body := encode.AppendString(nil, cs.Name)
	body = append(body, cs.Data...)
	return encodeSection(0, body)
}