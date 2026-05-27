// cpu/arm64/memory.go
package arm64

// emitMemAddr pops the wasm linear-memory address from the operand stack
// and computes the native address into X9: X9 = X28(MemBase) + wasm_offset.
func (fc *funcCompiler) emitMemAddr() {
	fc.emitPopR(X9)
	fc.ADD(X9, MemBase, X9) // native_addr = MemBase + wasm_offset
}

// emitAddOffset adds a static wasm memory offset to a register.
// Handles offsets up to 24 bits with at most two ADD instructions.
// Larger offsets use MOVZ+ADD.
func (fc *funcCompiler) emitAddOffset(reg int, off uint32) {
	if off == 0 {
		return
	}
	if off <= 4095 {
		fc.ADDSI(reg, reg, off)
		return
	}
	hi := (off >> 12) & 0xFFF
	lo := off & 0xFFF
	upper := off >> 24
	if upper != 0 {
		// Offset ≥ 16 MB: materialise in X11 and add.
		fc.MOVZ32(X11, uint16(off), 0)
		if off > 0xFFFF {
			fc.MOVK(X11, uint16(off>>16), 1)
		}
		if off > 0xFFFFFFFF {
			fc.MOVK(X11, uint16(off>>32), 2)
		}
		fc.ADD(reg, reg, X11)
		return
	}
	if hi > 0 {
		fc.ADDSIShifted(reg, reg, hi)
	}
	if lo > 0 {
		fc.ADDSI(reg, reg, lo)
	}
}

// ── Loads ─────────────────────────────────────────────────────────────────────

func (fc *funcCompiler) emitMemLoad32zx(off uint32) {
	fc.emitMemAddr()
	fc.emitAddOffset(X9, off)
	fc.LDR32(X10, X9, 0)
	fc.emitPushR(X10)
}

func (fc *funcCompiler) emitMemLoad64(off uint32) {
	fc.emitMemAddr()
	fc.emitAddOffset(X9, off)
	fc.LDR64(X10, X9, 0)
	fc.emitPushR(X10)
}

// emitMemLoadSX handles i32 narrow loads (result is an i32, sign- or zero-extended).
func (fc *funcCompiler) emitMemLoadSX(off uint32, byteWidth int, signed bool) {
	fc.emitMemAddr()
	fc.emitAddOffset(X9, off)
	switch {
	case byteWidth == 1 && signed:
		fc.LDRSB32(X10, X9, 0)
	case byteWidth == 1:
		fc.LDRB(X10, X9, 0)
	case byteWidth == 2 && signed:
		fc.LDRSH32(X10, X9, 0)
	default:
		fc.LDRH(X10, X9, 0)
	}
	fc.emitPushR(X10)
}

// emitMemLoadSX64 handles i64 narrow loads (result sign- or zero-extended to 64 bits).
func (fc *funcCompiler) emitMemLoadSX64(off uint32, byteWidth int, signed bool) {
	fc.emitMemAddr()
	fc.emitAddOffset(X9, off)
	switch {
	case byteWidth == 1 && signed:
		fc.LDRSB(X10, X9, 0)
	case byteWidth == 1:
		fc.LDRB(X10, X9, 0)
	case byteWidth == 2 && signed:
		fc.LDRSH(X10, X9, 0)
	case byteWidth == 2:
		fc.LDRH(X10, X9, 0)
	case byteWidth == 4 && signed:
		fc.LDRSW(X10, X9, 0)
	default:
		fc.LDR32(X10, X9, 0)
	}
	fc.emitPushR(X10)
}

// ── Stores ────────────────────────────────────────────────────────────────────

func (fc *funcCompiler) emitMemStore(off uint32, byteWidth int) {
	fc.emitPopR(X10) // value (TOS)
	fc.emitPopR(X9)  // address
	fc.ADD(X9, MemBase, X9)
	fc.emitAddOffset(X9, off)
	switch byteWidth {
	case 1:
		fc.STRB(X10, X9, 0)
	case 2:
		fc.STRH(X10, X9, 0)
	case 4:
		fc.STR32(X10, X9, 0)
	case 8:
		fc.STR64(X10, X9, 0)
	}
}

// emitMemFill implements memory.fill.
// Stack before: [..., d, val, n]  n = TOS
func (fc *funcCompiler) emitMemFill() {
	fc.emitPopR(X11) // n (byte count)
	fc.emitPopR(X10) // val (byte value)
	fc.emitPopR(X9)  // d (wasm destination offset)
	fc.ADD(X9, MemBase, X9)

	// Simple byte loop: while n > 0 { *d++ = val; n-- }
	// CBZ x11, .done
	donePatch := fc.CBZ(X11)
	loopTop := fc.Pos()
	fc.STRB(X10, X9, 0)
	fc.ADDSI(X9, X9, 1)
	fc.SUBSI(X11, X11, 1)
	fc.CBNZ(X11)
	// patch the CBNZ back to loopTop
	cbnzOff := fc.Pos() - 4
	fc.PatchCondImm19(cbnzOff, loopTop)
	fc.PatchCondImm19(donePatch, fc.Pos())
}