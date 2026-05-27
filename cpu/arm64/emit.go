// cpu/arm64/emit.go
package arm64

import "github.com/vertex-language/compiler/cpu/arm64/asm"

// ── Wasm operand-stack push / pop ─────────────────────────────────────────────
//
// Always use these helpers (never raw STR/LDR) so fc.depth stays accurate.

// emitPushR emits: str Xreg, [sp, #-8]!  (pre-decrement then store)
func (fc *funcCompiler) emitPushR(reg int) {
	fc.STR64PreIndex(reg, SP, -8)
	fc.depth++
}

// emitPopR emits: ldr Xreg, [sp], #8  (load then post-increment)
func (fc *funcCompiler) emitPopR(reg int) {
	fc.LDR64PostIndex(reg, SP, 8)
	fc.depth--
}

// emitPeekR emits: ldr Xreg, [sp]  — reads TOS without consuming it.
func (fc *funcCompiler) emitPeekR(reg int) {
	fc.LDR64(reg, SP, 0)
}

// emitStoreToStack overwrites a stack slot in place without changing depth.
// slotFromTop=0 overwrites TOS.
func (fc *funcCompiler) emitStoreToStack(src, slotFromTop int) {
	fc.STR64(src, SP, uint32(slotFromTop*8))
}

// emitDropN discards n slots (each 8 bytes) from the wasm operand stack.
func (fc *funcCompiler) emitDropN(n int) {
	if n == 0 {
		return
	}
	bytes := uint32(n * 8)
	if bytes <= 4095 {
		fc.ADDSI(SP, SP, bytes)
	} else {
		fc.MOVZ(X9, uint16(bytes), 0)
		fc.ADD(SP, SP, X9)
	}
	fc.depth -= n
}

// ── Local variable access ─────────────────────────────────────────────────────
// Locals live at [X29 − 8*(idx+1)] — negative offsets from the frame pointer.

func (fc *funcCompiler) emitLoadLocal64(dst, idx int) {
	disp := int32(-8 * (idx + 1))
	if disp >= -256 {
		fc.LDR64Unscaled(dst, FP, disp)
	} else {
		// Large frame: compute address into scratch then load.
		fc.SUBSI(X9, FP, uint32(-disp))
		fc.LDR64(dst, X9, 0)
	}
}

func (fc *funcCompiler) emitStoreLocal64(idx, src int) {
	disp := int32(-8 * (idx + 1))
	if disp >= -256 {
		fc.STR64Unscaled(src, FP, disp)
	} else {
		fc.SUBSI(X9, FP, uint32(-disp))
		fc.STR64(src, X9, 0)
	}
}

// ── Pointer translation ───────────────────────────────────────────────────────

// emitAddMemBaseTo emits: add reg, reg, x28  (wasm offset → native address)
// Used for syscall pointer arguments — no NULL guard.
func (fc *funcCompiler) emitAddMemBaseTo(reg int) {
	fc.ADD(reg, reg, asm.MemBase)
}

// emitSafePointerTranslate emits: cbz reg, +8; add reg, reg, x28
// NULL (offset 0) passes through as NULL — used for library import pointers.
func (fc *funcCompiler) emitSafePointerTranslate(reg int) {
	skipPatch := fc.CBZ(reg)
	fc.ADD(reg, reg, asm.MemBase)
	fc.PatchCondImm19(skipPatch, fc.Pos())
}