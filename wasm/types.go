package wasm

import (
	"encoding/binary"
	"math"

	"github.com/vertex-language/compiler/encode"
)

// ── Value Types ───────────────────────────────────────────────────────────────

type ValType byte

const (
	I32       ValType = 0x7F
	I64       ValType = 0x7E
	F32       ValType = 0x7D
	F64       ValType = 0x7C
	V128      ValType = 0x7B
	FuncRef   ValType = 0x70
	ExternRef ValType = 0x6F
)

// ── Heap Types ────────────────────────────────────────────────────────────────

type HeapType byte

const (
	HeapFunc   HeapType = 0x70
	HeapExtern HeapType = 0x6F
)

// ── Function Type ─────────────────────────────────────────────────────────────

type FuncType struct {
	Params  []ValType
	Results []ValType
}

// ── Limits ────────────────────────────────────────────────────────────────────

type Limits struct {
	Min uint32
	Max *uint32 // nil = unbounded
}

// ── Composite Types ───────────────────────────────────────────────────────────

type TableType struct {
	Element ValType // must be a reference type
	Lim     Limits
}

type MemoryType struct {
	Lim    Limits
	Shared bool // threads proposal; requires a Max
}

type GlobalType struct {
	Val     ValType
	Mutable bool
}

// ── Block Type ────────────────────────────────────────────────────────────────
// BlockType is the immediate operand of block / loop / if instructions.
// AppendTo encodes the type into dst and returns the extended slice.

type BlockType interface {
	AppendTo(dst []byte) []byte
}

// BlockEmpty is a block with no parameters and no results (0x40).
type BlockEmpty struct{}

func (BlockEmpty) AppendTo(dst []byte) []byte { return append(dst, 0x40) }

// BlockVal is a block with a single result value type.
type BlockVal struct{ Val ValType }

func (b BlockVal) AppendTo(dst []byte) []byte { return append(dst, byte(b.Val)) }

// BlockIdx references a function type by index (multi-value proposal).
// Encoded as a positive s33.
type BlockIdx struct{ Idx uint32 }

func (b BlockIdx) AppendTo(dst []byte) []byte { return encode.AppendS33(dst, int64(b.Idx)) }

// ── Constant Expressions ──────────────────────────────────────────────────────
// ConstExpr stores a pre-encoded wasm constant expression including the
// trailing end opcode (0x0B). Both the encoder (writes bytes directly) and the
// decoder (captures raw bytes) use this representation.

type ConstExpr struct{ bytes []byte }

// Bytes returns the raw encoded bytes, including the end opcode.
func (c ConstExpr) Bytes() []byte { return c.bytes }

// NewConstExprRaw constructs a ConstExpr from already-encoded bytes.
// The caller must include the trailing 0x0B end opcode. Used by the decoder.
func NewConstExprRaw(b []byte) ConstExpr {
	cp := make([]byte, len(b))
	copy(cp, b)
	return ConstExpr{cp}
}

func ConstI32(v int32) ConstExpr {
	return ConstExpr{append(encode.AppendS32([]byte{0x41}, v), 0x0B)}
}

func ConstI64(v int64) ConstExpr {
	return ConstExpr{append(encode.AppendSLEB128([]byte{0x42}, v), 0x0B)}
}

func ConstF32(v float32) ConstExpr {
	b := make([]byte, 5)
	b[0] = 0x43
	binary.LittleEndian.PutUint32(b[1:], math.Float32bits(v))
	return ConstExpr{append(b, 0x0B)}
}

func ConstF64(v float64) ConstExpr {
	b := make([]byte, 9)
	b[0] = 0x44
	binary.LittleEndian.PutUint64(b[1:], math.Float64bits(v))
	return ConstExpr{append(b, 0x0B)}
}

func ConstGlobalGet(idx uint32) ConstExpr {
	return ConstExpr{append(encode.AppendU32([]byte{0x23}, idx), 0x0B)}
}

func ConstRefNull(ht HeapType) ConstExpr {
	return ConstExpr{[]byte{0xD0, byte(ht), 0x0B}}
}

func ConstRefFunc(idx uint32) ConstExpr {
	return ConstExpr{append(encode.AppendU32([]byte{0xD2}, idx), 0x0B)}
}