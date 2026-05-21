package x86_64

import "fmt"

// dispatchMath handles Arithmetic, Comparisons, and Conversions (0x45 -> 0xC4).
// These instructions don't read immediates from the instruction stream.
func (fc *funcCompiler) dispatchMath(op byte) error {
	switch op {

	// ── i32 comparisons ───────────────────────────────────────────────────────

	case 0x45: // i32.eqz
		fc.emitPopR(RAX)
		fc.Emit(0x85, 0xC0)       // test eax, eax
		fc.Emit(0x0F, 0x94, 0xC0) // sete al
		fc.Emit(0x0F, 0xB6, 0xC0) // movzx eax, al
		fc.emitPushR(RAX)

	case 0x46: fc.emitCmp32Push(0x94) // i32.eq  — sete
	case 0x47: fc.emitCmp32Push(0x95) // i32.ne  — setne
	case 0x48: fc.emitCmp32Push(0x9C) // i32.lt_s — setl
	case 0x49: fc.emitCmp32Push(0x92) // i32.lt_u — setb
	case 0x4A: fc.emitCmp32Push(0x9F) // i32.gt_s — setg
	case 0x4B: fc.emitCmp32Push(0x97) // i32.gt_u — seta
	case 0x4C: fc.emitCmp32Push(0x9E) // i32.le_s — setle
	case 0x4D: fc.emitCmp32Push(0x96) // i32.le_u — setbe
	case 0x4E: fc.emitCmp32Push(0x9D) // i32.ge_s — setge
	case 0x4F: fc.emitCmp32Push(0x93) // i32.ge_u — setae

	// ── i64 comparisons ───────────────────────────────────────────────────────

	case 0x50: // i64.eqz
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x85, 0xC0) // test rax, rax
		fc.Emit(0x0F, 0x94, 0xC0) // sete al
		fc.Emit(0x0F, 0xB6, 0xC0) // movzx eax, al
		fc.emitPushR(RAX)

	case 0x51: fc.emitCmp64Push(0x94) // i64.eq
	case 0x52: fc.emitCmp64Push(0x95) // i64.ne
	case 0x53: fc.emitCmp64Push(0x9C) // i64.lt_s
	case 0x54: fc.emitCmp64Push(0x92) // i64.lt_u
	case 0x55: fc.emitCmp64Push(0x9F) // i64.gt_s
	case 0x56: fc.emitCmp64Push(0x97) // i64.gt_u
	case 0x57: fc.emitCmp64Push(0x9E) // i64.le_s
	case 0x58: fc.emitCmp64Push(0x96) // i64.le_u
	case 0x59: fc.emitCmp64Push(0x9D) // i64.ge_s
	case 0x5A: fc.emitCmp64Push(0x93) // i64.ge_u

	case 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60,
		0x61, 0x62, 0x63, 0x64, 0x65, 0x66:
		return fmt.Errorf("floating-point comparisons not implemented (op 0x%02X)", op)

	// ── i32 arithmetic ────────────────────────────────────────────────────────

	case 0x67: // i32.clz
		fc.emitPopR(RAX)
		fc.emitClz32()
		fc.emitPushR(RAX)
	case 0x68: // i32.ctz
		fc.emitPopR(RAX)
		fc.emitCtz32()
		fc.emitPushR(RAX)
	case 0x69: // i32.popcnt
		fc.emitPopR(RAX)
		fc.Emit(0xF3, 0x0F, 0xB8, 0xC0) // popcnt eax, eax
		fc.emitPushR(RAX)

	case 0x6A: fc.emitI32BinArith(func() { fc.Emit(0x01, 0xC8) })        // i32.add
	case 0x6B: fc.emitI32BinArithOrdered(func() { fc.Emit(0x29, 0xC8) }) // i32.sub
	case 0x6C: fc.emitI32BinArith(func() { fc.Emit(0x0F, 0xAF, 0xC1) })  // i32.mul
	case 0x6D: // i32.div_s
		fc.emitI32BinArithOrdered(func() { fc.Emit(0x99); fc.Emit(0xF7, 0xF9) })
	case 0x6E: // i32.div_u
		fc.emitI32BinArithOrdered(func() { fc.Emit(0x31, 0xD2); fc.Emit(0xF7, 0xF1) })
	case 0x6F: // i32.rem_s
		fc.emitI32BinArithOrdered(func() { fc.Emit(0x99); fc.Emit(0xF7, 0xF9); fc.Emit(0x89, 0xD0) })
	case 0x70: // i32.rem_u
		fc.emitI32BinArithOrdered(func() { fc.Emit(0x31, 0xD2); fc.Emit(0xF7, 0xF1); fc.Emit(0x89, 0xD0) })
	case 0x71: fc.emitI32BinArith(func() { fc.Emit(0x21, 0xC8) })        // i32.and
	case 0x72: fc.emitI32BinArith(func() { fc.Emit(0x09, 0xC8) })        // i32.or
	case 0x73: fc.emitI32BinArith(func() { fc.Emit(0x31, 0xC8) })        // i32.xor
	case 0x74: fc.emitI32Shift(func() { fc.Emit(0xD3, 0xE0) })           // i32.shl
	case 0x75: fc.emitI32Shift(func() { fc.Emit(0xD3, 0xF8) })           // i32.shr_s
	case 0x76: fc.emitI32Shift(func() { fc.Emit(0xD3, 0xE8) })           // i32.shr_u
	case 0x77: fc.emitI32Shift(func() { fc.Emit(0xD3, 0xC0) })           // i32.rotl
	case 0x78: fc.emitI32Shift(func() { fc.Emit(0xD3, 0xC8) })           // i32.rotr

	// ── i64 arithmetic ────────────────────────────────────────────────────────

	case 0x79: // i64.clz
		fc.emitPopR(RAX)
		fc.emitClz64()
		fc.emitPushR(RAX)
	case 0x7A: // i64.ctz
		fc.emitPopR(RAX)
		fc.emitCtz64()
		fc.emitPushR(RAX)
	case 0x7B: // i64.popcnt
		fc.emitPopR(RAX)
		fc.Emit(0xF3, 0x48, 0x0F, 0xB8, 0xC0) // popcnt rax, rax
		fc.emitPushR(RAX)

	case 0x7C: fc.emitI64BinArith(func() { fc.Emit(0x48, 0x01, 0xC8) })        // i64.add
	case 0x7D: fc.emitI64BinArithOrdered(func() { fc.Emit(0x48, 0x29, 0xC8) }) // i64.sub
	case 0x7E: fc.emitI64BinArith(func() { fc.Emit(0x48, 0x0F, 0xAF, 0xC1) })  // i64.mul
	case 0x7F: // i64.div_s
		fc.emitI64BinArithOrdered(func() { fc.Emit(0x48, 0x99); fc.Emit(0x48, 0xF7, 0xF9) })
	case 0x80: // i64.div_u
		fc.emitI64BinArithOrdered(func() { fc.Emit(0x48, 0x31, 0xD2); fc.Emit(0x48, 0xF7, 0xF1) })
	case 0x81: // i64.rem_s
		fc.emitI64BinArithOrdered(func() {
			fc.Emit(0x48, 0x99); fc.Emit(0x48, 0xF7, 0xF9); fc.Emit(0x48, 0x89, 0xD0)
		})
	case 0x82: // i64.rem_u
		fc.emitI64BinArithOrdered(func() {
			fc.Emit(0x48, 0x31, 0xD2); fc.Emit(0x48, 0xF7, 0xF1); fc.Emit(0x48, 0x89, 0xD0)
		})
	case 0x83: fc.emitI64BinArith(func() { fc.Emit(0x48, 0x21, 0xC8) })      // i64.and
	case 0x84: fc.emitI64BinArith(func() { fc.Emit(0x48, 0x09, 0xC8) })      // i64.or
	case 0x85: fc.emitI64BinArith(func() { fc.Emit(0x48, 0x31, 0xC8) })      // i64.xor
	case 0x86: fc.emitI64Shift(func() { fc.Emit(0x48, 0xD3, 0xE0) })         // i64.shl
	case 0x87: fc.emitI64Shift(func() { fc.Emit(0x48, 0xD3, 0xF8) })         // i64.shr_s
	case 0x88: fc.emitI64Shift(func() { fc.Emit(0x48, 0xD3, 0xE8) })         // i64.shr_u
	case 0x89: fc.emitI64Shift(func() { fc.Emit(0x48, 0xD3, 0xC0) })         // i64.rotl
	case 0x8A: fc.emitI64Shift(func() { fc.Emit(0x48, 0xD3, 0xC8) })         // i64.rotr

	case 0x8B, 0x8C, 0x8D, 0x8E, 0x8F, 0x90, 0x91, 0x92, 0x93, 0x94,
		0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E,
		0x9F, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6:
		return fmt.Errorf("floating-point arithmetic not implemented (op 0x%02X)", op)

	// ── Conversions ───────────────────────────────────────────────────────────

	case 0xA7: // i32.wrap_i64
		fc.emitPopR(RAX)
		fc.Emit(0x89, 0xC0) // mov eax, eax  (zero-extends to rax)
		fc.emitPushR(RAX)

	case 0xAC: // i64.extend_i32_s
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x63, 0xC0) // movsxd rax, eax
		fc.emitPushR(RAX)

	case 0xAD: // i64.extend_i32_u
		fc.emitPopR(RAX)
		fc.Emit(0x89, 0xC0) // mov eax, eax  (zero-extends)
		fc.emitPushR(RAX)

	case 0xA8, 0xA9, 0xAA, 0xAB,
		0xAE, 0xAF, 0xB0, 0xB1,
		0xB2, 0xB3, 0xB4, 0xB5, 0xB6,
		0xB7, 0xB8, 0xB9, 0xBA, 0xBB,
		0xBC, 0xBD, 0xBE, 0xBF:
		return fmt.Errorf("float/int conversion not implemented (op 0x%02X)", op)

	// ── Sign-extension (Wasm 2.0) ─────────────────────────────────────────────

	case 0xC0: // i32.extend8_s
		fc.emitPopR(RAX)
		fc.Emit(0x0F, 0xBE, 0xC0) // movsx eax, al
		fc.emitPushR(RAX)
	case 0xC1: // i32.extend16_s
		fc.emitPopR(RAX)
		fc.Emit(0x0F, 0xBF, 0xC0) // movsx eax, ax
		fc.emitPushR(RAX)
	case 0xC2: // i64.extend8_s
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x0F, 0xBE, 0xC0) // movsx rax, al
		fc.emitPushR(RAX)
	case 0xC3: // i64.extend16_s
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x0F, 0xBF, 0xC0) // movsx rax, ax
		fc.emitPushR(RAX)
	case 0xC4: // i64.extend32_s
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x63, 0xC0) // movsxd rax, eax
		fc.emitPushR(RAX)

	default:
		return fmt.Errorf("unimplemented math opcode 0x%02X", op)
	}
	return nil
}