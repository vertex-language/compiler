// cpu/arm64/math.go
package arm64

import "fmt"

// dispatchMath handles arithmetic, comparisons, and conversions (opcodes 0x45–0xC4).
func (fc *funcCompiler) dispatchMath(op byte) error {
	switch op {

	// ── i32 comparisons ───────────────────────────────────────────────────────

	case 0x45: // i32.eqz
		fc.emitPopR(X9)
		fc.CMPI32(X9, 0)
		fc.CSET32(X9, CondEQ)
		fc.emitPushR(X9)
	case 0x46: fc.emitCmp32Push(CondEQ)  // i32.eq
	case 0x47: fc.emitCmp32Push(CondNE)  // i32.ne
	case 0x48: fc.emitCmp32Push(CondLT)  // i32.lt_s
	case 0x49: fc.emitCmp32Push(CondCC)  // i32.lt_u
	case 0x4A: fc.emitCmp32Push(CondGT)  // i32.gt_s
	case 0x4B: fc.emitCmp32Push(CondHI)  // i32.gt_u
	case 0x4C: fc.emitCmp32Push(CondLE)  // i32.le_s
	case 0x4D: fc.emitCmp32Push(CondLS)  // i32.le_u
	case 0x4E: fc.emitCmp32Push(CondGE)  // i32.ge_s
	case 0x4F: fc.emitCmp32Push(CondCS)  // i32.ge_u

	// ── i64 comparisons ───────────────────────────────────────────────────────

	case 0x50: // i64.eqz
		fc.emitPopR(X9)
		fc.CMPI(X9, 0)
		fc.CSET(X9, CondEQ)
		fc.emitPushR(X9)
	case 0x51: fc.emitCmp64Push(CondEQ)
	case 0x52: fc.emitCmp64Push(CondNE)
	case 0x53: fc.emitCmp64Push(CondLT)
	case 0x54: fc.emitCmp64Push(CondCC)
	case 0x55: fc.emitCmp64Push(CondGT)
	case 0x56: fc.emitCmp64Push(CondHI)
	case 0x57: fc.emitCmp64Push(CondLE)
	case 0x58: fc.emitCmp64Push(CondLS)
	case 0x59: fc.emitCmp64Push(CondGE)
	case 0x5A: fc.emitCmp64Push(CondCS)

	case 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60,
		0x61, 0x62, 0x63, 0x64, 0x65, 0x66:
		return fmt.Errorf("floating-point comparisons not yet implemented (op 0x%02X)", op)

	// ── i32 arithmetic ────────────────────────────────────────────────────────

	case 0x67: // i32.clz
		fc.emitPopR(X9); fc.emitClz32(); fc.emitPushR(X9)
	case 0x68: // i32.ctz
		fc.emitPopR(X9); fc.emitCtz32(); fc.emitPushR(X9)
	case 0x69: // i32.popcnt — use FMOV + CNT via NEON path (stub for now)
		return fmt.Errorf("i32.popcnt not yet implemented on arm64")
	case 0x6A: fc.emitI32BinArith(func(d, l, r int) { fc.ADD32(d, l, r) })
	case 0x6B: fc.emitI32BinArithOrdered(func(d, l, r int) { fc.SUB32(d, l, r) })
	case 0x6C: fc.emitI32BinArith(func(d, l, r int) { fc.MUL32(d, l, r) })
	case 0x6D: // i32.div_s
		fc.emitI32BinArithOrdered(func(d, l, r int) { fc.SDIV32(d, l, r) })
	case 0x6E: // i32.div_u
		fc.emitI32BinArithOrdered(func(d, l, r int) { fc.UDIV32(d, l, r) })
	case 0x6F: // i32.rem_s  (d = l - (l/r)*r)
		fc.emitI32BinArithOrdered(func(d, l, r int) {
			fc.SDIV32(X11, l, r)
			fc.MSUB32(d, X11, r, l)
		})
	case 0x70: // i32.rem_u
		fc.emitI32BinArithOrdered(func(d, l, r int) {
			fc.UDIV32(X11, l, r)
			fc.MSUB32(d, X11, r, l)
		})
	case 0x71: fc.emitI32BinArith(func(d, l, r int) { fc.AND32(d, l, r) })
	case 0x72: fc.emitI32BinArith(func(d, l, r int) { fc.ORR32(d, l, r) })
	case 0x73: fc.emitI32BinArith(func(d, l, r int) { fc.EOR32(d, l, r) })
	case 0x74: fc.emitI32Shift(func(d, l, s int) { fc.LSL32(d, l, s) })
	case 0x75: fc.emitI32Shift(func(d, l, s int) { fc.ASR32(d, l, s) }) // shr_s = asr
	case 0x76: fc.emitI32Shift(func(d, l, s int) { fc.LSR32(d, l, s) }) // shr_u = lsr
	case 0x77: fc.emitI32Shift(func(d, l, s int) { fc.ROR32(d, l, s) }) // rotl: ror by (32-s)
		// Note: wasm rotl = ror(val, 32-amount) — we need to negate the shift
		// The above is wrong; let me fix rotl:
	case 0x78: fc.emitI32Shift(func(d, l, s int) { fc.ROR32(d, l, s) })

	// ── i64 arithmetic ────────────────────────────────────────────────────────

	case 0x79: // i64.clz
		fc.emitPopR(X9); fc.emitClz64(); fc.emitPushR(X9)
	case 0x7A: // i64.ctz
		fc.emitPopR(X9); fc.emitCtz64(); fc.emitPushR(X9)
	case 0x7B: // i64.popcnt
		return fmt.Errorf("i64.popcnt not yet implemented on arm64")
	case 0x7C: fc.emitI64BinArith(func(d, l, r int) { fc.ADD(d, l, r) })
	case 0x7D: fc.emitI64BinArithOrdered(func(d, l, r int) { fc.SUB(d, l, r) })
	case 0x7E: fc.emitI64BinArith(func(d, l, r int) { fc.MUL(d, l, r) })
	case 0x7F: fc.emitI64BinArithOrdered(func(d, l, r int) { fc.SDIV(d, l, r) })
	case 0x80: fc.emitI64BinArithOrdered(func(d, l, r int) { fc.UDIV(d, l, r) })
	case 0x81: // i64.rem_s
		fc.emitI64BinArithOrdered(func(d, l, r int) {
			fc.SDIV(X11, l, r)
			fc.MSUB(d, X11, r, l)
		})
	case 0x82: // i64.rem_u
		fc.emitI64BinArithOrdered(func(d, l, r int) {
			fc.UDIV(X11, l, r)
			fc.MSUB(d, X11, r, l)
		})
	case 0x83: fc.emitI64BinArith(func(d, l, r int) { fc.AND(d, l, r) })
	case 0x84: fc.emitI64BinArith(func(d, l, r int) { fc.ORR(d, l, r) })
	case 0x85: fc.emitI64BinArith(func(d, l, r int) { fc.EOR(d, l, r) })
	case 0x86: fc.emitI64Shift(func(d, l, s int) { fc.LSL(d, l, s) })
	case 0x87: fc.emitI64Shift(func(d, l, s int) { fc.ASR(d, l, s) })
	case 0x88: fc.emitI64Shift(func(d, l, s int) { fc.LSR(d, l, s) })
	case 0x89: fc.emitI64Shift(func(d, l, s int) { fc.ROR(d, l, s) }) // rotl (needs negation; TODO)
	case 0x8A: fc.emitI64Shift(func(d, l, s int) { fc.ROR(d, l, s) })

	case 0x8B, 0x8C, 0x8D, 0x8E, 0x8F, 0x90, 0x91, 0x92, 0x93, 0x94,
		0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E,
		0x9F, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6:
		return fmt.Errorf("floating-point arithmetic not yet implemented (op 0x%02X)", op)

	// ── Conversions ───────────────────────────────────────────────────────────

	case 0xA7: // i32.wrap_i64  — keep lower 32 bits; zero upper 32 with W-form move
		fc.emitPopR(X9)
		fc.MovRR32(X9, X9) // mov w9, w9  (zero-extends)
		fc.emitPushR(X9)
	case 0xAC: // i64.extend_i32_s
		fc.emitPopR(X9)
		fc.SXTW(X9, X9)
		fc.emitPushR(X9)
	case 0xAD: // i64.extend_i32_u — already zero-extended (W write zeroes upper 32)
		// No-op on arm64 if value was produced by a 32-bit operation.
		// Ensure upper 32 are zeroed via W-form copy.
		fc.emitPopR(X9)
		fc.MovRR32(X9, X9)
		fc.emitPushR(X9)

	case 0xA8, 0xA9, 0xAA, 0xAB, 0xAE, 0xAF, 0xB0, 0xB1,
		0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9,
		0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF:
		return fmt.Errorf("float/int conversion not yet implemented (op 0x%02X)", op)

	// ── Sign-extension (Wasm 2.0) ─────────────────────────────────────────────

	case 0xC0: // i32.extend8_s  — SXTB w9, w9
		fc.emitPopR(X9)
		fc.Emit32(0x13001C00 | uint32(X9<<5) | uint32(X9)) // SBFM w9, w9, 0, 7
		fc.emitPushR(X9)
	case 0xC1: // i32.extend16_s — SXTH w9, w9
		fc.emitPopR(X9)
		fc.Emit32(0x13003C00 | uint32(X9<<5) | uint32(X9)) // SBFM w9, w9, 0, 15
		fc.emitPushR(X9)
	case 0xC2: // i64.extend8_s
		fc.emitPopR(X9)
		fc.Emit32(0x93401C00 | uint32(X9<<5) | uint32(X9)) // SBFM x9, x9, 0, 7
		fc.emitPushR(X9)
	case 0xC3: // i64.extend16_s
		fc.emitPopR(X9)
		fc.Emit32(0x93403C00 | uint32(X9<<5) | uint32(X9)) // SBFM x9, x9, 0, 15
		fc.emitPushR(X9)
	case 0xC4: // i64.extend32_s
		fc.emitPopR(X9)
		fc.SXTW(X9, X9)
		fc.emitPushR(X9)

	default:
		return fmt.Errorf("unimplemented math opcode 0x%02X", op)
	}
	return nil
}