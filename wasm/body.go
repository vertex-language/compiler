package wasm

import (
	"encoding/binary"
	"math"

	"github.com/vertex-language/compiler/encode"
)

// LocalGroup is a run-length-encoded group of locals sharing the same type.
// Spec: locals ::= n:u32  t:valtype  ⇒  (t)^n
type LocalGroup struct {
	Count uint32
	Type  ValType
}

// FunctionBody is a builder for a single Wasm function body.
// Locals are declared with AddLocals; instructions are emitted via the
// builder methods. The encoder reads the result via Locals() and Code().
// The decoder constructs a FunctionBody via NewFunctionBodyRaw.
type FunctionBody struct {
	locals []LocalGroup
	buf    []byte
}

// NewFunctionBody returns an empty FunctionBody for building from scratch.
func NewFunctionBody() *FunctionBody { return &FunctionBody{} }

// NewFunctionBodyRaw constructs a FunctionBody from decoded data.
// locals are the local variable declarations; code is the raw instruction
// byte stream (everything after the local declarations, up to and including
// the end opcode). Used by the decoder.
func NewFunctionBodyRaw(locals []LocalGroup, code []byte) *FunctionBody {
	cp := make([]byte, len(code))
	copy(cp, code)
	return &FunctionBody{locals: locals, buf: cp}
}

// AddLocals declares count locals of type vt.
// Locals are indexed after the function's parameters.
func (f *FunctionBody) AddLocals(count uint32, vt ValType) {
	f.locals = append(f.locals, LocalGroup{count, vt})
}

// Locals returns the local variable declarations. Used by the encoder.
func (f *FunctionBody) Locals() []LocalGroup { return f.locals }

// Code returns the raw instruction byte stream. Used by the encoder and compiler.
func (f *FunctionBody) Code() []byte { return f.buf }

// ── Internal emit helpers ─────────────────────────────────────────────────────

func (f *FunctionBody) emit(b ...byte) *FunctionBody {
	f.buf = append(f.buf, b...)
	return f
}

func (f *FunctionBody) emitU32(v uint32) *FunctionBody {
	f.buf = encode.AppendU32(f.buf, v)
	return f
}

func (f *FunctionBody) emitS32(v int32) *FunctionBody {
	f.buf = encode.AppendS32(f.buf, v)
	return f
}

func (f *FunctionBody) emitS64(v int64) *FunctionBody {
	f.buf = encode.AppendSLEB128(f.buf, v)
	return f
}

func (f *FunctionBody) emitBlockType(bt BlockType) *FunctionBody {
	f.buf = bt.AppendTo(f.buf)
	return f
}

func (f *FunctionBody) emitMemArg(align, offset uint32) *FunctionBody {
	f.buf = encode.AppendU32(f.buf, align)
	f.buf = encode.AppendU32(f.buf, offset)
	return f
}

func (f *FunctionBody) emitFC(subop uint32) *FunctionBody {
	f.buf = append(f.buf, 0xFC)
	f.buf = encode.AppendU32(f.buf, subop)
	return f
}

// ── Control instructions  §  0x00–0x13 ───────────────────────────────────────

func (f *FunctionBody) Unreachable() *FunctionBody { return f.emit(0x00) }
func (f *FunctionBody) Nop() *FunctionBody          { return f.emit(0x01) }

func (f *FunctionBody) Block(bt BlockType) *FunctionBody { return f.emit(0x02).emitBlockType(bt) }
func (f *FunctionBody) Loop(bt BlockType) *FunctionBody  { return f.emit(0x03).emitBlockType(bt) }
func (f *FunctionBody) If(bt BlockType) *FunctionBody    { return f.emit(0x04).emitBlockType(bt) }
func (f *FunctionBody) Else() *FunctionBody              { return f.emit(0x05) }
func (f *FunctionBody) End() *FunctionBody               { return f.emit(0x0B) }

func (f *FunctionBody) Br(labelIdx uint32) *FunctionBody   { return f.emit(0x0C).emitU32(labelIdx) }
func (f *FunctionBody) BrIf(labelIdx uint32) *FunctionBody { return f.emit(0x0D).emitU32(labelIdx) }

func (f *FunctionBody) BrTable(targets []uint32, defaultTarget uint32) *FunctionBody {
	f.emit(0x0E).emitU32(uint32(len(targets)))
	for _, t := range targets {
		f.emitU32(t)
	}
	return f.emitU32(defaultTarget)
}

func (f *FunctionBody) Return() *FunctionBody { return f.emit(0x0F) }

func (f *FunctionBody) Call(funcIdx uint32) *FunctionBody { return f.emit(0x10).emitU32(funcIdx) }

func (f *FunctionBody) CallIndirect(typeIdx, tableIdx uint32) *FunctionBody {
	return f.emit(0x11).emitU32(typeIdx).emitU32(tableIdx)
}

func (f *FunctionBody) ReturnCall(funcIdx uint32) *FunctionBody {
	return f.emit(0x12).emitU32(funcIdx)
}

func (f *FunctionBody) ReturnCallIndirect(typeIdx, tableIdx uint32) *FunctionBody {
	return f.emit(0x13).emitU32(typeIdx).emitU32(tableIdx)
}

// ── Parametric instructions  §  0x1A–0x1C ────────────────────────────────────

func (f *FunctionBody) Drop() *FunctionBody   { return f.emit(0x1A) }
func (f *FunctionBody) Select() *FunctionBody { return f.emit(0x1B) }

func (f *FunctionBody) SelectTyped(vt ValType) *FunctionBody {
	return f.emit(0x1C).emitU32(1).emit(byte(vt))
}

// ── Variable instructions  §  0x20–0x26 ──────────────────────────────────────

func (f *FunctionBody) LocalGet(idx uint32) *FunctionBody  { return f.emit(0x20).emitU32(idx) }
func (f *FunctionBody) LocalSet(idx uint32) *FunctionBody  { return f.emit(0x21).emitU32(idx) }
func (f *FunctionBody) LocalTee(idx uint32) *FunctionBody  { return f.emit(0x22).emitU32(idx) }
func (f *FunctionBody) GlobalGet(idx uint32) *FunctionBody { return f.emit(0x23).emitU32(idx) }
func (f *FunctionBody) GlobalSet(idx uint32) *FunctionBody { return f.emit(0x24).emitU32(idx) }
func (f *FunctionBody) TableGet(idx uint32) *FunctionBody  { return f.emit(0x25).emitU32(idx) }
func (f *FunctionBody) TableSet(idx uint32) *FunctionBody  { return f.emit(0x26).emitU32(idx) }

// ── Memory load  §  0x28–0x35 ────────────────────────────────────────────────

func (f *FunctionBody) I32Load(align, offset uint32) *FunctionBody    { return f.emit(0x28).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load(align, offset uint32) *FunctionBody    { return f.emit(0x29).emitMemArg(align, offset) }
func (f *FunctionBody) F32Load(align, offset uint32) *FunctionBody    { return f.emit(0x2A).emitMemArg(align, offset) }
func (f *FunctionBody) F64Load(align, offset uint32) *FunctionBody    { return f.emit(0x2B).emitMemArg(align, offset) }
func (f *FunctionBody) I32Load8S(align, offset uint32) *FunctionBody  { return f.emit(0x2C).emitMemArg(align, offset) }
func (f *FunctionBody) I32Load8U(align, offset uint32) *FunctionBody  { return f.emit(0x2D).emitMemArg(align, offset) }
func (f *FunctionBody) I32Load16S(align, offset uint32) *FunctionBody { return f.emit(0x2E).emitMemArg(align, offset) }
func (f *FunctionBody) I32Load16U(align, offset uint32) *FunctionBody { return f.emit(0x2F).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load8S(align, offset uint32) *FunctionBody  { return f.emit(0x30).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load8U(align, offset uint32) *FunctionBody  { return f.emit(0x31).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load16S(align, offset uint32) *FunctionBody { return f.emit(0x32).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load16U(align, offset uint32) *FunctionBody { return f.emit(0x33).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load32S(align, offset uint32) *FunctionBody { return f.emit(0x34).emitMemArg(align, offset) }
func (f *FunctionBody) I64Load32U(align, offset uint32) *FunctionBody { return f.emit(0x35).emitMemArg(align, offset) }

// ── Memory store  §  0x36–0x3E ───────────────────────────────────────────────

func (f *FunctionBody) I32Store(align, offset uint32) *FunctionBody   { return f.emit(0x36).emitMemArg(align, offset) }
func (f *FunctionBody) I64Store(align, offset uint32) *FunctionBody   { return f.emit(0x37).emitMemArg(align, offset) }
func (f *FunctionBody) F32Store(align, offset uint32) *FunctionBody   { return f.emit(0x38).emitMemArg(align, offset) }
func (f *FunctionBody) F64Store(align, offset uint32) *FunctionBody   { return f.emit(0x39).emitMemArg(align, offset) }
func (f *FunctionBody) I32Store8(align, offset uint32) *FunctionBody  { return f.emit(0x3A).emitMemArg(align, offset) }
func (f *FunctionBody) I32Store16(align, offset uint32) *FunctionBody { return f.emit(0x3B).emitMemArg(align, offset) }
func (f *FunctionBody) I64Store8(align, offset uint32) *FunctionBody  { return f.emit(0x3C).emitMemArg(align, offset) }
func (f *FunctionBody) I64Store16(align, offset uint32) *FunctionBody { return f.emit(0x3D).emitMemArg(align, offset) }
func (f *FunctionBody) I64Store32(align, offset uint32) *FunctionBody { return f.emit(0x3E).emitMemArg(align, offset) }

// ── Memory control  §  0x3F–0x40 ─────────────────────────────────────────────

func (f *FunctionBody) MemorySize() *FunctionBody { return f.emit(0x3F, 0x00) }
func (f *FunctionBody) MemoryGrow() *FunctionBody { return f.emit(0x40, 0x00) }

// ── Numeric constants  §  0x41–0x44 ──────────────────────────────────────────

func (f *FunctionBody) I32Const(v int32) *FunctionBody { return f.emit(0x41).emitS32(v) }
func (f *FunctionBody) I64Const(v int64) *FunctionBody { return f.emit(0x42).emitS64(v) }

func (f *FunctionBody) F32Const(v float32) *FunctionBody {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	return f.emit(0x43).emit(b[:]...)
}

func (f *FunctionBody) F64Const(v float64) *FunctionBody {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	return f.emit(0x44).emit(b[:]...)
}

// ── i32 comparison  §  0x45–0x4F ─────────────────────────────────────────────

func (f *FunctionBody) I32Eqz() *FunctionBody { return f.emit(0x45) }
func (f *FunctionBody) I32Eq() *FunctionBody  { return f.emit(0x46) }
func (f *FunctionBody) I32Ne() *FunctionBody  { return f.emit(0x47) }
func (f *FunctionBody) I32LtS() *FunctionBody { return f.emit(0x48) }
func (f *FunctionBody) I32LtU() *FunctionBody { return f.emit(0x49) }
func (f *FunctionBody) I32GtS() *FunctionBody { return f.emit(0x4A) }
func (f *FunctionBody) I32GtU() *FunctionBody { return f.emit(0x4B) }
func (f *FunctionBody) I32LeS() *FunctionBody { return f.emit(0x4C) }
func (f *FunctionBody) I32LeU() *FunctionBody { return f.emit(0x4D) }
func (f *FunctionBody) I32GeS() *FunctionBody { return f.emit(0x4E) }
func (f *FunctionBody) I32GeU() *FunctionBody { return f.emit(0x4F) }

// ── i64 comparison  §  0x50–0x5A ─────────────────────────────────────────────

func (f *FunctionBody) I64Eqz() *FunctionBody { return f.emit(0x50) }
func (f *FunctionBody) I64Eq() *FunctionBody  { return f.emit(0x51) }
func (f *FunctionBody) I64Ne() *FunctionBody  { return f.emit(0x52) }
func (f *FunctionBody) I64LtS() *FunctionBody { return f.emit(0x53) }
func (f *FunctionBody) I64LtU() *FunctionBody { return f.emit(0x54) }
func (f *FunctionBody) I64GtS() *FunctionBody { return f.emit(0x55) }
func (f *FunctionBody) I64GtU() *FunctionBody { return f.emit(0x56) }
func (f *FunctionBody) I64LeS() *FunctionBody { return f.emit(0x57) }
func (f *FunctionBody) I64LeU() *FunctionBody { return f.emit(0x58) }
func (f *FunctionBody) I64GeS() *FunctionBody { return f.emit(0x59) }
func (f *FunctionBody) I64GeU() *FunctionBody { return f.emit(0x5A) }

// ── f32 comparison  §  0x5B–0x60 ─────────────────────────────────────────────

func (f *FunctionBody) F32Eq() *FunctionBody { return f.emit(0x5B) }
func (f *FunctionBody) F32Ne() *FunctionBody { return f.emit(0x5C) }
func (f *FunctionBody) F32Lt() *FunctionBody { return f.emit(0x5D) }
func (f *FunctionBody) F32Gt() *FunctionBody { return f.emit(0x5E) }
func (f *FunctionBody) F32Le() *FunctionBody { return f.emit(0x5F) }
func (f *FunctionBody) F32Ge() *FunctionBody { return f.emit(0x60) }

// ── f64 comparison  §  0x61–0x66 ─────────────────────────────────────────────

func (f *FunctionBody) F64Eq() *FunctionBody { return f.emit(0x61) }
func (f *FunctionBody) F64Ne() *FunctionBody { return f.emit(0x62) }
func (f *FunctionBody) F64Lt() *FunctionBody { return f.emit(0x63) }
func (f *FunctionBody) F64Gt() *FunctionBody { return f.emit(0x64) }
func (f *FunctionBody) F64Le() *FunctionBody { return f.emit(0x65) }
func (f *FunctionBody) F64Ge() *FunctionBody { return f.emit(0x66) }

// ── i32 arithmetic  §  0x67–0x78 ─────────────────────────────────────────────

func (f *FunctionBody) I32Clz() *FunctionBody    { return f.emit(0x67) }
func (f *FunctionBody) I32Ctz() *FunctionBody    { return f.emit(0x68) }
func (f *FunctionBody) I32Popcnt() *FunctionBody { return f.emit(0x69) }
func (f *FunctionBody) I32Add() *FunctionBody    { return f.emit(0x6A) }
func (f *FunctionBody) I32Sub() *FunctionBody    { return f.emit(0x6B) }
func (f *FunctionBody) I32Mul() *FunctionBody    { return f.emit(0x6C) }
func (f *FunctionBody) I32DivS() *FunctionBody   { return f.emit(0x6D) }
func (f *FunctionBody) I32DivU() *FunctionBody   { return f.emit(0x6E) }
func (f *FunctionBody) I32RemS() *FunctionBody   { return f.emit(0x6F) }
func (f *FunctionBody) I32RemU() *FunctionBody   { return f.emit(0x70) }
func (f *FunctionBody) I32And() *FunctionBody    { return f.emit(0x71) }
func (f *FunctionBody) I32Or() *FunctionBody     { return f.emit(0x72) }
func (f *FunctionBody) I32Xor() *FunctionBody    { return f.emit(0x73) }
func (f *FunctionBody) I32Shl() *FunctionBody    { return f.emit(0x74) }
func (f *FunctionBody) I32ShrS() *FunctionBody   { return f.emit(0x75) }
func (f *FunctionBody) I32ShrU() *FunctionBody   { return f.emit(0x76) }
func (f *FunctionBody) I32Rotl() *FunctionBody   { return f.emit(0x77) }
func (f *FunctionBody) I32Rotr() *FunctionBody   { return f.emit(0x78) }

// ── i64 arithmetic  §  0x79–0x8A ─────────────────────────────────────────────

func (f *FunctionBody) I64Clz() *FunctionBody    { return f.emit(0x79) }
func (f *FunctionBody) I64Ctz() *FunctionBody    { return f.emit(0x7A) }
func (f *FunctionBody) I64Popcnt() *FunctionBody { return f.emit(0x7B) }
func (f *FunctionBody) I64Add() *FunctionBody    { return f.emit(0x7C) }
func (f *FunctionBody) I64Sub() *FunctionBody    { return f.emit(0x7D) }
func (f *FunctionBody) I64Mul() *FunctionBody    { return f.emit(0x7E) }
func (f *FunctionBody) I64DivS() *FunctionBody   { return f.emit(0x7F) }
func (f *FunctionBody) I64DivU() *FunctionBody   { return f.emit(0x80) }
func (f *FunctionBody) I64RemS() *FunctionBody   { return f.emit(0x81) }
func (f *FunctionBody) I64RemU() *FunctionBody   { return f.emit(0x82) }
func (f *FunctionBody) I64And() *FunctionBody    { return f.emit(0x83) }
func (f *FunctionBody) I64Or() *FunctionBody     { return f.emit(0x84) }
func (f *FunctionBody) I64Xor() *FunctionBody    { return f.emit(0x85) }
func (f *FunctionBody) I64Shl() *FunctionBody    { return f.emit(0x86) }
func (f *FunctionBody) I64ShrS() *FunctionBody   { return f.emit(0x87) }
func (f *FunctionBody) I64ShrU() *FunctionBody   { return f.emit(0x88) }
func (f *FunctionBody) I64Rotl() *FunctionBody   { return f.emit(0x89) }
func (f *FunctionBody) I64Rotr() *FunctionBody   { return f.emit(0x8A) }

// ── f32 arithmetic  §  0x8B–0x98 ─────────────────────────────────────────────

func (f *FunctionBody) F32Abs() *FunctionBody      { return f.emit(0x8B) }
func (f *FunctionBody) F32Neg() *FunctionBody      { return f.emit(0x8C) }
func (f *FunctionBody) F32Ceil() *FunctionBody     { return f.emit(0x8D) }
func (f *FunctionBody) F32Floor() *FunctionBody    { return f.emit(0x8E) }
func (f *FunctionBody) F32Trunc() *FunctionBody    { return f.emit(0x8F) }
func (f *FunctionBody) F32Nearest() *FunctionBody  { return f.emit(0x90) }
func (f *FunctionBody) F32Sqrt() *FunctionBody     { return f.emit(0x91) }
func (f *FunctionBody) F32Add() *FunctionBody      { return f.emit(0x92) }
func (f *FunctionBody) F32Sub() *FunctionBody      { return f.emit(0x93) }
func (f *FunctionBody) F32Mul() *FunctionBody      { return f.emit(0x94) }
func (f *FunctionBody) F32Div() *FunctionBody      { return f.emit(0x95) }
func (f *FunctionBody) F32Min() *FunctionBody      { return f.emit(0x96) }
func (f *FunctionBody) F32Max() *FunctionBody      { return f.emit(0x97) }
func (f *FunctionBody) F32Copysign() *FunctionBody { return f.emit(0x98) }

// ── f64 arithmetic  §  0x99–0xA6 ─────────────────────────────────────────────

func (f *FunctionBody) F64Abs() *FunctionBody      { return f.emit(0x99) }
func (f *FunctionBody) F64Neg() *FunctionBody      { return f.emit(0x9A) }
func (f *FunctionBody) F64Ceil() *FunctionBody     { return f.emit(0x9B) }
func (f *FunctionBody) F64Floor() *FunctionBody    { return f.emit(0x9C) }
func (f *FunctionBody) F64Trunc() *FunctionBody    { return f.emit(0x9D) }
func (f *FunctionBody) F64Nearest() *FunctionBody  { return f.emit(0x9E) }
func (f *FunctionBody) F64Sqrt() *FunctionBody     { return f.emit(0x9F) }
func (f *FunctionBody) F64Add() *FunctionBody      { return f.emit(0xA0) }
func (f *FunctionBody) F64Sub() *FunctionBody      { return f.emit(0xA1) }
func (f *FunctionBody) F64Mul() *FunctionBody      { return f.emit(0xA2) }
func (f *FunctionBody) F64Div() *FunctionBody      { return f.emit(0xA3) }
func (f *FunctionBody) F64Min() *FunctionBody      { return f.emit(0xA4) }
func (f *FunctionBody) F64Max() *FunctionBody      { return f.emit(0xA5) }
func (f *FunctionBody) F64Copysign() *FunctionBody { return f.emit(0xA6) }

// ── Conversions  §  0xA7–0xBF ────────────────────────────────────────────────

func (f *FunctionBody) I32WrapI64() *FunctionBody        { return f.emit(0xA7) }
func (f *FunctionBody) I32TruncF32S() *FunctionBody      { return f.emit(0xA8) }
func (f *FunctionBody) I32TruncF32U() *FunctionBody      { return f.emit(0xA9) }
func (f *FunctionBody) I32TruncF64S() *FunctionBody      { return f.emit(0xAA) }
func (f *FunctionBody) I32TruncF64U() *FunctionBody      { return f.emit(0xAB) }
func (f *FunctionBody) I64ExtendI32S() *FunctionBody     { return f.emit(0xAC) }
func (f *FunctionBody) I64ExtendI32U() *FunctionBody     { return f.emit(0xAD) }
func (f *FunctionBody) I64TruncF32S() *FunctionBody      { return f.emit(0xAE) }
func (f *FunctionBody) I64TruncF32U() *FunctionBody      { return f.emit(0xAF) }
func (f *FunctionBody) I64TruncF64S() *FunctionBody      { return f.emit(0xB0) }
func (f *FunctionBody) I64TruncF64U() *FunctionBody      { return f.emit(0xB1) }
func (f *FunctionBody) F32ConvertI32S() *FunctionBody    { return f.emit(0xB2) }
func (f *FunctionBody) F32ConvertI32U() *FunctionBody    { return f.emit(0xB3) }
func (f *FunctionBody) F32ConvertI64S() *FunctionBody    { return f.emit(0xB4) }
func (f *FunctionBody) F32ConvertI64U() *FunctionBody    { return f.emit(0xB5) }
func (f *FunctionBody) F32DemoteF64() *FunctionBody      { return f.emit(0xB6) }
func (f *FunctionBody) F64ConvertI32S() *FunctionBody    { return f.emit(0xB7) }
func (f *FunctionBody) F64ConvertI32U() *FunctionBody    { return f.emit(0xB8) }
func (f *FunctionBody) F64ConvertI64S() *FunctionBody    { return f.emit(0xB9) }
func (f *FunctionBody) F64ConvertI64U() *FunctionBody    { return f.emit(0xBA) }
func (f *FunctionBody) F64PromoteF32() *FunctionBody     { return f.emit(0xBB) }
func (f *FunctionBody) I32ReinterpretF32() *FunctionBody { return f.emit(0xBC) }
func (f *FunctionBody) I64ReinterpretF64() *FunctionBody { return f.emit(0xBD) }
func (f *FunctionBody) F32ReinterpretI32() *FunctionBody { return f.emit(0xBE) }
func (f *FunctionBody) F64ReinterpretI64() *FunctionBody { return f.emit(0xBF) }

// ── Sign-extension (Wasm 2.0)  §  0xC0–0xC4 ──────────────────────────────────

func (f *FunctionBody) I32Extend8S() *FunctionBody  { return f.emit(0xC0) }
func (f *FunctionBody) I32Extend16S() *FunctionBody { return f.emit(0xC1) }
func (f *FunctionBody) I64Extend8S() *FunctionBody  { return f.emit(0xC2) }
func (f *FunctionBody) I64Extend16S() *FunctionBody { return f.emit(0xC3) }
func (f *FunctionBody) I64Extend32S() *FunctionBody { return f.emit(0xC4) }

// ── Reference-type instructions (Wasm 2.0)  §  0xD0–0xD2 ────────────────────

func (f *FunctionBody) RefNull(ht HeapType) *FunctionBody { return f.emit(0xD0, byte(ht)) }
func (f *FunctionBody) RefIsNull() *FunctionBody          { return f.emit(0xD1) }
func (f *FunctionBody) RefFunc(idx uint32) *FunctionBody  { return f.emit(0xD2).emitU32(idx) }

// ── 0xFC-prefixed instructions (Wasm 2.0) ────────────────────────────────────

func (f *FunctionBody) I32TruncSatF32S() *FunctionBody { return f.emitFC(0) }
func (f *FunctionBody) I32TruncSatF32U() *FunctionBody { return f.emitFC(1) }
func (f *FunctionBody) I32TruncSatF64S() *FunctionBody { return f.emitFC(2) }
func (f *FunctionBody) I32TruncSatF64U() *FunctionBody { return f.emitFC(3) }
func (f *FunctionBody) I64TruncSatF32S() *FunctionBody { return f.emitFC(4) }
func (f *FunctionBody) I64TruncSatF32U() *FunctionBody { return f.emitFC(5) }
func (f *FunctionBody) I64TruncSatF64S() *FunctionBody { return f.emitFC(6) }
func (f *FunctionBody) I64TruncSatF64U() *FunctionBody { return f.emitFC(7) }

func (f *FunctionBody) MemoryInit(dataIdx uint32) *FunctionBody {
	return f.emitFC(8).emitU32(dataIdx).emit(0x00)
}
func (f *FunctionBody) DataDrop(dataIdx uint32) *FunctionBody { return f.emitFC(9).emitU32(dataIdx) }
func (f *FunctionBody) MemoryCopy() *FunctionBody             { return f.emitFC(10).emit(0x00, 0x00) }
func (f *FunctionBody) MemoryFill() *FunctionBody             { return f.emitFC(11).emit(0x00) }

func (f *FunctionBody) TableInit(elemIdx, tableIdx uint32) *FunctionBody {
	return f.emitFC(12).emitU32(elemIdx).emitU32(tableIdx)
}
func (f *FunctionBody) ElemDrop(elemIdx uint32) *FunctionBody { return f.emitFC(13).emitU32(elemIdx) }
func (f *FunctionBody) TableCopy(dstIdx, srcIdx uint32) *FunctionBody {
	return f.emitFC(14).emitU32(dstIdx).emitU32(srcIdx)
}
func (f *FunctionBody) TableGrow(tableIdx uint32) *FunctionBody { return f.emitFC(15).emitU32(tableIdx) }
func (f *FunctionBody) TableSize(tableIdx uint32) *FunctionBody { return f.emitFC(16).emitU32(tableIdx) }
func (f *FunctionBody) TableFill(tableIdx uint32) *FunctionBody { return f.emitFC(17).emitU32(tableIdx) }