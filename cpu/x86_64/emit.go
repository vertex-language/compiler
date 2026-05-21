package x86_64

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// ── Wasm operand-stack push / pop ─────────────────────────────────────────────
//
// These wrap the raw asm Push/Pop with wasm stack-depth accounting.
// Use these (not fc.Push/fc.Pop directly) whenever moving a wasm value.

func (fc *funcCompiler) emitPushR(reg int) {
	fc.Push(reg)
	fc.depth++
}

func (fc *funcCompiler) emitPopR(reg int) {
	fc.Pop(reg)
	fc.depth--
}

// emitPeekR emits: mov dst, [rsp]  — reads TOS without consuming it.
func (fc *funcCompiler) emitPeekR(dst int) { fc.PeekRSP(dst) }

// emitStoreToStack overwrites a stack slot in place without changing depth.
// slotFromTop=0 overwrites TOS.
func (fc *funcCompiler) emitStoreToStack(src, slotFromTop int) {
	fc.StoreRSPSlot(src, slotFromTop)
}

// emitAddRSP pops n slots off the wasm operand stack (each 8 bytes).
func (fc *funcCompiler) emitAddRSP(n int) { fc.AddRSP(n) }

// ── Local variable access ─────────────────────────────────────────────────────

func (fc *funcCompiler) emitLoadLocal64(dst, idx int)  { fc.LoadLocal64(dst, idx) }
func (fc *funcCompiler) emitStoreLocal64(idx, src int) { fc.StoreLocal64(idx, src) }
func (fc *funcCompiler) emitMovR64FromRBPDisp(dst int, disp int32) {
	fc.LoadRBPDisp(dst, disp)
}

// ── Pointer translation ───────────────────────────────────────────────────────

// emitAddR15To emits: add reg, r15  — translates a wasm linear-memory offset
// to a native virtual address unconditionally (syscall / kernel pointer path).
func (fc *funcCompiler) emitAddR15To(reg int) {
	rexByte := byte(0x4C)
	if reg >= 8 { rexByte |= 0x01 }
	fc.Emit(rexByte, 0x01, asm.ModRM(0b11, 7, byte(reg&7)))
}

// emitSafePointerTranslate emits: test reg,reg; jz +3; add reg,r15
// Used for library imports where NULL (offset 0) must pass through as NULL.
func (fc *funcCompiler) emitSafePointerTranslate(reg int) {
	fc.Emit(asm.REX(true, reg >= 8, false, reg >= 8))
	fc.Emit(0x85, asm.ModRM(0b11, byte(reg&7), byte(reg&7)))
	fc.Emit(0x74, 0x03) // je +3 (skip the add)
	fc.emitAddR15To(reg)
}