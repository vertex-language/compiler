package x86_64

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
// Stack before: [..., val1, val2, cond]  (cond = TOS)
// Stack after:  [..., cond ? val1 : val2]

func (fc *funcCompiler) emitSelect() {
	fc.emitPopR(R11)                   // cond → R11
	fc.emitPopR(RCX)                   // val2 → RCX
	fc.emitPeekR(RAX)                  // val1 → RAX (keep slot on stack)
	fc.Emit(0x45, 0x85, 0xDB)          // test r11d, r11d
	fc.Emit(0x48, 0x0F, 0x44, 0xC1)   // cmovz rax, rcx  (pick val2 if cond==0)
	fc.emitStoreToStack(RAX, 0)        // overwrite TOS slot
}

// ── i32 binary helpers ────────────────────────────────────────────────────────
// Stack: [..., a, b]  b=TOS

func (fc *funcCompiler) emitI32BinArith(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	body()
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitI32BinArithOrdered(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX) // b→RCX (right), a→RAX (left)
	body()
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitI32Shift(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX) // count→CL, value→EAX
	body()
	fc.emitPushR(RAX)
}

// ── i64 binary helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitI64BinArith(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	body()
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitI64BinArithOrdered(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	body()
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitI64Shift(body func()) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	body()
	fc.emitPushR(RAX)
}

// ── Comparison helpers ────────────────────────────────────────────────────────

func (fc *funcCompiler) emitCmp32Push(setccByte byte) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	fc.Emit(0x39, 0xC8)             // cmp eax, ecx
	fc.Emit(0x0F, setccByte, 0xC0)  // setcc al
	fc.Emit(0x0F, 0xB6, 0xC0)       // movzx eax, al
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitCmp64Push(setccByte byte) {
	fc.emitPopR(RCX); fc.emitPopR(RAX)
	fc.Emit(0x48, 0x39, 0xC8)       // cmp rax, rcx
	fc.Emit(0x0F, setccByte, 0xC0)
	fc.Emit(0x0F, 0xB6, 0xC0)
	fc.emitPushR(RAX)
}

// ── Count leading / trailing zeros ───────────────────────────────────────────
// All operate on RAX in-place. BSR/BSF are undefined for input=0, so we
// special-case it with a short branch.

func (fc *funcCompiler) emitClz32() {
	fc.Emit(0x85, 0xC0)        // test eax, eax
	zeroFwd := fc.JzRel32()
	fc.Emit(0x0F, 0xBD, 0xC0) // bsr eax, eax
	fc.Emit(0x83, 0xF0, 0x1F) // xor eax, 31  → clz = 31 - bsr
	endFwd := fc.JmpShort()
	fc.Patch32(zeroFwd, fc.Pos())
	fc.Emit(0xB8, 32, 0, 0, 0) // mov eax, 32  (clz of 0)
	fc.SetByte(endFwd, byte(fc.Pos()-endFwd-1))
}

func (fc *funcCompiler) emitCtz32() {
	fc.Emit(0x85, 0xC0)
	zeroFwd := fc.JzRel32()
	fc.Emit(0x0F, 0xBC, 0xC0) // bsf eax, eax
	endFwd := fc.JmpShort()
	fc.Patch32(zeroFwd, fc.Pos())
	fc.Emit(0xB8, 32, 0, 0, 0)
	fc.SetByte(endFwd, byte(fc.Pos()-endFwd-1))
}

func (fc *funcCompiler) emitClz64() {
	fc.Emit(0x48, 0x85, 0xC0)        // test rax, rax
	zeroFwd := fc.JzRel32()
	fc.Emit(0x48, 0x0F, 0xBD, 0xC0) // bsr rax, rax
	fc.Emit(0x48, 0x83, 0xF0, 0x3F) // xor rax, 63
	endFwd := fc.JmpShort()
	fc.Patch32(zeroFwd, fc.Pos())
	fc.Emit(0x48, 0xC7, 0xC0, 64, 0, 0, 0) // mov rax, 64
	fc.SetByte(endFwd, byte(fc.Pos()-endFwd-1))
}

func (fc *funcCompiler) emitCtz64() {
	fc.Emit(0x48, 0x85, 0xC0)
	zeroFwd := fc.JzRel32()
	fc.Emit(0x48, 0x0F, 0xBC, 0xC0) // bsf rax, rax
	endFwd := fc.JmpShort()
	fc.Patch32(zeroFwd, fc.Pos())
	fc.Emit(0x48, 0xC7, 0xC0, 64, 0, 0, 0)
	fc.SetByte(endFwd, byte(fc.Pos()-endFwd-1))
}