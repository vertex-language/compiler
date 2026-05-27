// cpu/amd64/arith.go
package amd64

// ── Global variable access ────────────────────────────────────────────────────
//
// Globals live at [R15 − 8*(idx+1)] — the reserved region below the base.

func (fc *funcCompiler) emitGlobalGet(idx int) {
	disp := -int32(8 * (idx + 1))
	fc.Emit(0x49, 0x8B, 0x87) // mov rax, [r15 + disp32]
	fc.Emit32(uint32(disp))
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitGlobalSet(idx int) {
	disp := -int32(8 * (idx + 1))
	fc.emitPopR(RAX)
	fc.Emit(0x49, 0x89, 0x87) // mov [r15 + disp32], rax
	fc.Emit32(uint32(disp))
}

// ── Select ────────────────────────────────────────────────────────────────────
// Stack before: [..., val1, val2, cond]  cond = TOS
// Stack after:  [..., cond ? val1 : val2]

func (fc *funcCompiler) emitSelect() {
	fc.emitPopR(R11)                 // cond
	fc.emitPopR(RCX)                 // val2
	fc.emitPeekR(RAX)                // val1 (keep slot)
	fc.Emit(0x45, 0x85, 0xDB)        // test r11d, r11d
	fc.Emit(0x48, 0x0F, 0x44, 0xC1) // cmovz rax, rcx
	fc.emitStoreToStack(RAX, 0)
}

// ── i32 binary helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitI32BinArith(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}
func (fc *funcCompiler) emitI32BinArithOrdered(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}
func (fc *funcCompiler) emitI32Shift(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}

// ── i64 binary helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitI64BinArith(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}
func (fc *funcCompiler) emitI64BinArithOrdered(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}
func (fc *funcCompiler) emitI64Shift(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX); body(); fc.emitPushR(RAX)
}

// ── Comparison helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitCmp32Push(setccByte byte) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	fc.Emit(0x39, 0xC8)
	fc.Emit(0x0F, setccByte, 0xC0)
	fc.Emit(0x0F, 0xB6, 0xC0)
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitCmp64Push(setccByte byte) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	fc.Emit(0x48, 0x39, 0xC8)
	fc.Emit(0x0F, setccByte, 0xC0)
	fc.Emit(0x0F, 0xB6, 0xC0)
	fc.emitPushR(RAX)
}

// ── Count leading / trailing zeros ───────────────────────────────────────────

func (fc *funcCompiler) emitClz32() {
	fc.Emit(0x85, 0xC0)
	zf := fc.JzRel32()
	fc.Emit(0x0F, 0xBD, 0xC0)
	fc.Emit(0x83, 0xF0, 0x1F)
	ef := fc.JmpShort()
	fc.Patch32(zf, fc.Pos())
	fc.Emit(0xB8, 32, 0, 0, 0)
	fc.SetByte(ef, byte(fc.Pos()-ef-1))
}

func (fc *funcCompiler) emitCtz32() {
	fc.Emit(0x85, 0xC0)
	zf := fc.JzRel32()
	fc.Emit(0x0F, 0xBC, 0xC0)
	ef := fc.JmpShort()
	fc.Patch32(zf, fc.Pos())
	fc.Emit(0xB8, 32, 0, 0, 0)
	fc.SetByte(ef, byte(fc.Pos()-ef-1))
}

func (fc *funcCompiler) emitClz64() {
	fc.Emit(0x48, 0x85, 0xC0)
	zf := fc.JzRel32()
	fc.Emit(0x48, 0x0F, 0xBD, 0xC0)
	fc.Emit(0x48, 0x83, 0xF0, 0x3F)
	ef := fc.JmpShort()
	fc.Patch32(zf, fc.Pos())
	fc.Emit(0x48, 0xC7, 0xC0, 64, 0, 0, 0)
	fc.SetByte(ef, byte(fc.Pos()-ef-1))
}

func (fc *funcCompiler) emitCtz64() {
	fc.Emit(0x48, 0x85, 0xC0)
	zf := fc.JzRel32()
	fc.Emit(0x48, 0x0F, 0xBC, 0xC0)
	ef := fc.JmpShort()
	fc.Patch32(zf, fc.Pos())
	fc.Emit(0x48, 0xC7, 0xC0, 64, 0, 0, 0)
	fc.SetByte(ef, byte(fc.Pos()-ef-1))
}