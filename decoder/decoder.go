// Package decoder deserialises a WebAssembly binary into a wasm.Module.
package decoder

import (
	"errors"
	"fmt"

	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/wasm"
)

var (
	wasmMagic   = [4]byte{0x00, 0x61, 0x73, 0x6D}
	wasmVersion = [4]byte{0x01, 0x00, 0x00, 0x00}
)

// Decode parses a WebAssembly binary and returns the corresponding Module.
func Decode(data []byte) (*wasm.Module, error) {
	r := decode.NewReader(data)

	// ── Preamble ──────────────────────────────────────────────────────────────
	magic, err := r.ReadFixedBytes(4)
	if err != nil {
		return nil, fmt.Errorf("decoder: reading magic: %w", err)
	}
	if [4]byte(magic) != wasmMagic {
		return nil, errors.New("decoder: invalid magic bytes")
	}
	version, err := r.ReadFixedBytes(4)
	if err != nil {
		return nil, fmt.Errorf("decoder: reading version: %w", err)
	}
	if [4]byte(version) != wasmVersion {
		return nil, fmt.Errorf("decoder: unsupported version %v", version)
	}

	m := wasm.NewModule()

	// ── Sections ──────────────────────────────────────────────────────────────
	for !r.EOF() {
		id, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("decoder: reading section id: %w", err)
		}
		size, err := r.ReadU32()
		if err != nil {
			return nil, fmt.Errorf("decoder: reading section size: %w", err)
		}
		sr, err := r.Sub(size)
		if err != nil {
			return nil, fmt.Errorf("decoder: reading section body (id=%d): %w", id, err)
		}

		switch id {
		case 0:
			if err := decodeCustomSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: custom section: %w", err)
			}
		case 1:
			if err := decodeTypeSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: type section: %w", err)
			}
		case 2:
			if err := decodeImportSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: import section: %w", err)
			}
		case 3:
			if err := decodeFunctionSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: function section: %w", err)
			}
		case 4:
			if err := decodeTableSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: table section: %w", err)
			}
		case 5:
			if err := decodeMemorySection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: memory section: %w", err)
			}
		case 6:
			if err := decodeGlobalSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: global section: %w", err)
			}
		case 7:
			if err := decodeExportSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: export section: %w", err)
			}
		case 8:
			if err := decodeStartSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: start section: %w", err)
			}
		case 9:
			if err := decodeElementSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: element section: %w", err)
			}
		case 10:
			if err := decodeCodeSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: code section: %w", err)
			}
		case 11:
			if err := decodeDataSection(sr, m); err != nil {
				return nil, fmt.Errorf("decoder: data section: %w", err)
			}
		case 12:
			// DataCount — read and discard; we reconstruct the count from
			// the data section directly.
			if _, err := sr.ReadU32(); err != nil {
				return nil, fmt.Errorf("decoder: datacount section: %w", err)
			}
		default:
			// Unknown section — skip. The sub-reader already consumed the bytes.
		}
	}

	return m, nil
}

// ── Type helpers ──────────────────────────────────────────────────────────────

func decodeLimits(r *decode.Reader) (wasm.Limits, error) {
	flag, err := r.ReadByte()
	if err != nil {
		return wasm.Limits{}, err
	}
	min, err := r.ReadU32()
	if err != nil {
		return wasm.Limits{}, err
	}
	l := wasm.Limits{Min: min}
	if flag == 0x01 {
		max, err := r.ReadU32()
		if err != nil {
			return wasm.Limits{}, err
		}
		l.Max = &max
	}
	return l, nil
}

func decodeTableType(r *decode.Reader) (wasm.TableType, error) {
	elem, err := r.ReadByte()
	if err != nil {
		return wasm.TableType{}, err
	}
	lim, err := decodeLimits(r)
	if err != nil {
		return wasm.TableType{}, err
	}
	return wasm.TableType{Element: wasm.ValType(elem), Lim: lim}, nil
}

func decodeMemoryType(r *decode.Reader) (wasm.MemoryType, error) {
	flag, err := r.ReadByte()
	if err != nil {
		return wasm.MemoryType{}, err
	}
	min, err := r.ReadU32()
	if err != nil {
		return wasm.MemoryType{}, err
	}
	switch flag {
	case 0x00:
		return wasm.MemoryType{Lim: wasm.Limits{Min: min}}, nil
	case 0x01:
		max, err := r.ReadU32()
		if err != nil {
			return wasm.MemoryType{}, err
		}
		return wasm.MemoryType{Lim: wasm.Limits{Min: min, Max: &max}}, nil
	case 0x02:
		max, err := r.ReadU32()
		if err != nil {
			return wasm.MemoryType{}, err
		}
		return wasm.MemoryType{Lim: wasm.Limits{Min: min, Max: &max}, Shared: true}, nil
	}
	return wasm.MemoryType{}, fmt.Errorf("decoder: unknown memory flag 0x%02X", flag)
}

func decodeGlobalType(r *decode.Reader) (wasm.GlobalType, error) {
	val, err := r.ReadByte()
	if err != nil {
		return wasm.GlobalType{}, err
	}
	mut, err := r.ReadByte()
	if err != nil {
		return wasm.GlobalType{}, err
	}
	return wasm.GlobalType{Val: wasm.ValType(val), Mutable: mut == 0x01}, nil
}

// decodeConstExpr reads a constant expression by parsing each instruction's
// immediate operands until the end opcode (0x0B), then captures the raw bytes.
// ConstExpr is used for globals, data segment offsets, and element segments.
func decodeConstExpr(r *decode.Reader) (wasm.ConstExpr, error) {
	start := r.Pos()
	for {
		op, err := r.ReadByte()
		if err != nil {
			return wasm.ConstExpr{}, err
		}
		switch op {
		case 0x0B: // end
			return wasm.NewConstExprRaw(r.RawSlice(start, r.Pos())), nil
		case 0x41: // i32.const
			if _, err := r.ReadS32(); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0x42: // i64.const
			if _, err := r.ReadSLEB128(); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0x43: // f32.const
			if _, err := r.ReadFixedBytes(4); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0x44: // f64.const
			if _, err := r.ReadFixedBytes(8); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0x23: // global.get
			if _, err := r.ReadU32(); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0xD0: // ref.null
			if _, err := r.ReadByte(); err != nil {
				return wasm.ConstExpr{}, err
			}
		case 0xD2: // ref.func
			if _, err := r.ReadU32(); err != nil {
				return wasm.ConstExpr{}, err
			}
		default:
			return wasm.ConstExpr{}, fmt.Errorf("decoder: unexpected opcode 0x%02X in constexpr", op)
		}
	}
}

// ── Section decoders ──────────────────────────────────────────────────────────

func decodeCustomSection(r *decode.Reader, m *wasm.Module) error {
	name, err := r.ReadString()
	if err != nil {
		return err
	}
	data, err := r.ReadFixedBytes(uint32(r.Len()))
	if err != nil {
		return err
	}
	m.Customs = append(m.Customs, wasm.CustomSection{Name: name, Data: data})
	return nil
}

func decodeTypeSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		tag, err := r.ReadByte()
		if err != nil {
			return err
		}
		if tag != 0x60 {
			return fmt.Errorf("decoder: expected functype tag 0x60, got 0x%02X", tag)
		}

		paramCount, err := r.ReadVecHeader()
		if err != nil {
			return err
		}
		params := make([]wasm.ValType, paramCount)
		for j := range params {
			b, err := r.ReadByte()
			if err != nil {
				return err
			}
			params[j] = wasm.ValType(b)
		}

		resultCount, err := r.ReadVecHeader()
		if err != nil {
			return err
		}
		results := make([]wasm.ValType, resultCount)
		for j := range results {
			b, err := r.ReadByte()
			if err != nil {
				return err
			}
			results[j] = wasm.ValType(b)
		}

		m.Types.AddFuncType(wasm.FuncType{Params: params, Results: results})
	}
	return nil
}

func decodeImportSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		mod, err := r.ReadString()
		if err != nil {
			return err
		}
		name, err := r.ReadString()
		if err != nil {
			return err
		}
		kind, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch wasm.ImportKind(kind) {
		case wasm.ImportFunc:
			typeIdx, err := r.ReadU32()
			if err != nil {
				return err
			}
			m.Imports.AddFunc(mod, name, typeIdx)
		case wasm.ImportTable:
			tt, err := decodeTableType(r)
			if err != nil {
				return err
			}
			m.Imports.AddTable(mod, name, tt)
		case wasm.ImportMem:
			mt, err := decodeMemoryType(r)
			if err != nil {
				return err
			}
			m.Imports.AddMemory(mod, name, mt)
		case wasm.ImportGlobal:
			gt, err := decodeGlobalType(r)
			if err != nil {
				return err
			}
			m.Imports.AddGlobal(mod, name, gt)
		default:
			return fmt.Errorf("decoder: unknown import kind 0x%02X", kind)
		}
	}
	return nil
}

func decodeFunctionSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		typeIdx, err := r.ReadU32()
		if err != nil {
			return err
		}
		m.Functions.Add(typeIdx)
	}
	return nil
}

func decodeTableSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		tt, err := decodeTableType(r)
		if err != nil {
			return err
		}
		m.Tables.Add(tt)
	}
	return nil
}

func decodeMemorySection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		mt, err := decodeMemoryType(r)
		if err != nil {
			return err
		}
		m.Memories.Add(mt)
	}
	return nil
}

func decodeGlobalSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		gt, err := decodeGlobalType(r)
		if err != nil {
			return err
		}
		init, err := decodeConstExpr(r)
		if err != nil {
			return err
		}
		m.Globals.Add(gt, init)
	}
	return nil
}

func decodeExportSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		name, err := r.ReadString()
		if err != nil {
			return err
		}
		kind, err := r.ReadByte()
		if err != nil {
			return err
		}
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		m.Exports.Add(name, wasm.ExportKind(kind), idx)
	}
	return nil
}

func decodeStartSection(r *decode.Reader, m *wasm.Module) error {
	idx, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.SetStart(idx)
	return nil
}

func decodeElementSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		seg, err := decodeElemSegment(r)
		if err != nil {
			return fmt.Errorf("element segment %d: %w", i, err)
		}
		m.Elements.Add(seg)
	}
	return nil
}

func decodeElemSegment(r *decode.Reader) (wasm.ElemSegment, error) {
	flag, err := r.ReadU32()
	if err != nil {
		return wasm.ElemSegment{}, err
	}

	readFuncIndices := func() (wasm.ElemFuncIndices, error) {
		n, err := r.ReadVecHeader()
		if err != nil {
			return nil, err
		}
		indices := make(wasm.ElemFuncIndices, n)
		for i := range indices {
			idx, err := r.ReadU32()
			if err != nil {
				return nil, err
			}
			indices[i] = idx
		}
		return indices, nil
	}

	readExprs := func(refType wasm.ValType) (wasm.ElemExpressions, error) {
		n, err := r.ReadVecHeader()
		if err != nil {
			return wasm.ElemExpressions{}, err
		}
		exprs := make([]wasm.ConstExpr, n)
		for i := range exprs {
			expr, err := decodeConstExpr(r)
			if err != nil {
				return wasm.ElemExpressions{}, err
			}
			exprs[i] = expr
		}
		return wasm.ElemExpressions{RefType: refType, Exprs: exprs}, nil
	}

	switch flag {
	case 0: // active, table 0, funcref, funcindices
		offset, err := decodeConstExpr(r)
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readFuncIndices()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeActive, TableIdx: 0, Offset: offset, Items: items}, nil

	case 1: // passive, elemkind, funcindices
		if _, err := r.ReadByte(); err != nil { // elemkind (0x00 = funcref)
			return wasm.ElemSegment{}, err
		}
		items, err := readFuncIndices()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModePassive, Items: items}, nil

	case 2: // active, explicit tableidx, elemkind, funcindices
		tableIdx, err := r.ReadU32()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		offset, err := decodeConstExpr(r)
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		if _, err := r.ReadByte(); err != nil { // elemkind
			return wasm.ElemSegment{}, err
		}
		items, err := readFuncIndices()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeActive, TableIdx: tableIdx, Offset: offset, Items: items}, nil

	case 3: // declarative, elemkind, funcindices
		if _, err := r.ReadByte(); err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readFuncIndices()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeDeclarative, Items: items}, nil

	case 4: // active, table 0, constexpr list (funcref implied)
		offset, err := decodeConstExpr(r)
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readExprs(wasm.FuncRef)
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeActive, TableIdx: 0, Offset: offset, Items: items}, nil

	case 5: // passive, reftype, constexpr list
		refType, err := r.ReadByte()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readExprs(wasm.ValType(refType))
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModePassive, Items: items}, nil

	case 6: // active, explicit tableidx, reftype, constexpr list
		tableIdx, err := r.ReadU32()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		offset, err := decodeConstExpr(r)
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		refType, err := r.ReadByte()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readExprs(wasm.ValType(refType))
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeActive, TableIdx: tableIdx, Offset: offset, Items: items}, nil

	case 7: // declarative, reftype, constexpr list
		refType, err := r.ReadByte()
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		items, err := readExprs(wasm.ValType(refType))
		if err != nil {
			return wasm.ElemSegment{}, err
		}
		return wasm.ElemSegment{Mode: wasm.ElemModeDeclarative, Items: items}, nil
	}

	return wasm.ElemSegment{}, fmt.Errorf("decoder: unknown element segment format %d", flag)
}

func decodeCodeSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		fb, err := decodeFunctionBody(r)
		if err != nil {
			return fmt.Errorf("function body %d: %w", i, err)
		}
		m.Codes.Add(fb)
	}
	return nil
}

func decodeFunctionBody(r *decode.Reader) (*wasm.FunctionBody, error) {
	size, err := r.ReadU32()
	if err != nil {
		return nil, err
	}
	sr, err := r.Sub(size)
	if err != nil {
		return nil, err
	}

	localCount, err := sr.ReadVecHeader()
	if err != nil {
		return nil, err
	}
	locals := make([]wasm.LocalGroup, localCount)
	for i := range locals {
		count, err := sr.ReadU32()
		if err != nil {
			return nil, err
		}
		typ, err := sr.ReadByte()
		if err != nil {
			return nil, err
		}
		locals[i] = wasm.LocalGroup{Count: count, Type: wasm.ValType(typ)}
	}

	// Everything remaining is the raw instruction stream.
	code, err := sr.ReadFixedBytes(uint32(sr.Len()))
	if err != nil {
		return nil, err
	}

	return wasm.NewFunctionBodyRaw(locals, code), nil
}

func decodeDataSection(r *decode.Reader, m *wasm.Module) error {
	count, err := r.ReadVecHeader()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		flag, err := r.ReadU32()
		if err != nil {
			return fmt.Errorf("data segment %d flag: %w", i, err)
		}
		switch flag {
		case 0: // active, memory 0
			offset, err := decodeConstExpr(r)
			if err != nil {
				return fmt.Errorf("data segment %d offset: %w", i, err)
			}
			data, err := r.ReadByteVec()
			if err != nil {
				return fmt.Errorf("data segment %d data: %w", i, err)
			}
			m.Datas.Add(wasm.DataModeActive{MemIdx: 0, Offset: offset}, data)

		case 1: // passive
			data, err := r.ReadByteVec()
			if err != nil {
				return fmt.Errorf("data segment %d data: %w", i, err)
			}
			m.Datas.Add(wasm.DataModePassive{}, data)

		case 2: // active, explicit memory index
			memIdx, err := r.ReadU32()
			if err != nil {
				return fmt.Errorf("data segment %d memidx: %w", i, err)
			}
			offset, err := decodeConstExpr(r)
			if err != nil {
				return fmt.Errorf("data segment %d offset: %w", i, err)
			}
			data, err := r.ReadByteVec()
			if err != nil {
				return fmt.Errorf("data segment %d data: %w", i, err)
			}
			m.Datas.Add(wasm.DataModeActive{MemIdx: memIdx, Offset: offset}, data)

		default:
			return fmt.Errorf("decoder: unknown data segment flag %d", flag)
		}
	}
	return nil
}