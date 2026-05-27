// cpu/arm64/arith.go
package arm64

// ── Global variable access ────────────────────────────────────────────────────
// Globals live at [MemBase(X28) − 8*(idx+1)] — negative offsets from MemBase.

func (fc *funcCompiler) emitGlobalGet(idx int) {
	disp := int32(-8 * (idx + 1))
	if disp >= -256 {
		fc.LDR64Unscaled(X9, MemBase, disp)
	} else {
		fc.SUBSI(X9, MemBase, uint32(-disp))
		fc.LDR64(X9, X9, 0)
	}
	fc.emitPushR(X9)
}

func (fc *funcCompiler) emitGlobalSet(idx int) {
	disp := int32(-8 * (idx + 1))
	fc.emitPopR(X9)
	if disp >= -256 {
		fc.STR64Unscaled(X9, MemBase, disp)
	} else {
		fc.SUBSI(X10, MemBase, uint32(-disp))
		fc.STR64(X9, X10, 0)
	}
}

// ── Select ────────────────────────────────────────────────────────────────────
// Stack before: [..., val1, val2, cond]  cond = TOS
// Stack after:  [..., val1]  if cond≠0, else [..., val2]

func (fc *funcCompiler) emitSelect() {
	fc.emitPopR(X11) // cond
	fc.emitPopR(X10) // val2
	fc.emitPeekR(X9) // val1 (keep slot)
	// csel x9, x9, x10, ne  (if cond ≠ 0 choose val1, else val2)
	fc.CSEL(X9, X9, X10, CondNE)
	fc.emitStoreToStack(X9, 0)
}

// ── i32 binary helpers ────────────────────────────────────────────────────────
// The wasm operand stack stores i32 values zero-extended to 64 bits.
// All i32 binary ops pop two 64-bit slots and push one.

func (fc *funcCompiler) emitI32BinArith(emit func(dst, lhs, rhs int)) {
	fc.emitPopR(X10) // rhs (TOS)
	fc.emitPopR(X9)  // lhs
	emit(X9, X9, X10)
	// Zero-extend 32-bit result: writing W register zeros upper 32 bits,
	// but our helpers emit X-register forms; use AND with 0xFFFFFFFF mask
	// (cheaper: just let upper bits exist; wasm semantics only use lower 32).
	// We adopt the same approach as the amd64 backend: leave upper bits dirty
	// for intermediate values; comparisons use 32-bit forms explicitly.
	fc.emitPushR(X9)
}

func (fc *funcCompiler) emitI32BinArithOrdered(emit func(dst, lhs, rhs int)) {
	fc.emitI32BinArith(emit)
}

func (fc *funcCompiler) emitI32Shift(emit func(dst, lhs, shift int)) {
	fc.emitPopR(X10) // shift amount (rhs)
	fc.emitPopR(X9)  // value (lhs)
	emit(X9, X9, X10)
	fc.emitPushR(X9)
}

// ── i64 binary helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitI64BinArith(emit func(dst, lhs, rhs int)) {
	fc.emitPopR(X10)
	fc.emitPopR(X9)
	emit(X9, X9, X10)
	fc.emitPushR(X9)
}

func (fc *funcCompiler) emitI64BinArithOrdered(emit func(dst, lhs, rhs int)) {
	fc.emitI64BinArith(emit)
}

func (fc *funcCompiler) emitI64Shift(emit func(dst, lhs, shift int)) {
	fc.emitPopR(X10)
	fc.emitPopR(X9)
	emit(X9, X9, X10)
	fc.emitPushR(X9)
}

// ── Comparison helpers ────────────────────────────────────────────────────────

// emitCmp32Push pops two i32 values, compares them, and pushes 1 or 0.
func (fc *funcCompiler) emitCmp32Push(cond uint32) {
	fc.emitPopR(X10) // rhs
	fc.emitPopR(X9)  // lhs
	fc.CMP32(X9, X10)
	fc.CSET32(X9, cond)
	fc.emitPushR(X9)
}

// emitCmp64Push pops two i64 values, compares them, and pushes 1 or 0.
func (fc *funcCompiler) emitCmp64Push(cond uint32) {
	fc.emitPopR(X10)
	fc.emitPopR(X9)
	fc.CMP(X9, X10)
	fc.CSET(X9, cond)
	fc.emitPushR(X9)
}

// ── Count leading / trailing zeros ───────────────────────────────────────────

func (fc *funcCompiler) emitClz32() {
	// clz w9, w9 handles the zero case correctly on arm64 (returns 32).
	fc.CLZ32(X9, X9)
}

func (fc *funcCompiler) emitCtz32() {
	// ctz = clz(rbit(x)); returns 32 for input 0.
	fc.RBIT32(X9, X9)
	fc.CLZ32(X9, X9)
}

func (fc *funcCompiler) emitClz64() {
	fc.CLZ(X9, X9)
}

func (fc *funcCompiler) emitCtz64() {
	fc.RBIT(X9, X9)
	fc.CLZ(X9, X9)
}