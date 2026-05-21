package asm

// Assembler is a raw x86-64 machine-code buffer.
//
// It has no knowledge of WebAssembly, object files, or linker symbols.
// Callers use Pos() to record relocation sites and Patch32 / Patch8 /
// Patch32Abs / SetByte to back-fill forward references and raw fields.
type Assembler struct {
	buf []byte
}

// Bytes returns the assembled byte slice.
func (a *Assembler) Bytes() []byte { return a.buf }

// Pos returns the current write offset.
func (a *Assembler) Pos() int { return len(a.buf) }

// Reset clears the buffer without releasing the backing allocation.
func (a *Assembler) Reset() { a.buf = a.buf[:0] }

// ── Raw emission ──────────────────────────────────────────────────────────────

func (a *Assembler) Emit(b ...byte)  { a.buf = append(a.buf, b...) }
func (a *Assembler) Emit32(v uint32) { a.buf = Append32LE(a.buf, v) }
func (a *Assembler) Emit64(v uint64) { a.buf = Append64LE(a.buf, v) }

// ZeroRel32 appends four zero bytes (a rel32 placeholder) and returns their
// offset. The caller records a relocation or calls Patch32 to fill them in.
func (a *Assembler) ZeroRel32() int {
	off := len(a.buf)
	a.buf = append(a.buf, 0, 0, 0, 0)
	return off
}

// ── Patching ──────────────────────────────────────────────────────────────────

// Patch32 back-patches the rel32 field at off so it resolves to target.
// Computes: rel32 = target − (off + 4).
func (a *Assembler) Patch32(off, target int) {
	Put32LE(a.buf[off:], uint32(int32(target-(off+4))))
}

// Patch8 back-patches the rel8 field at off so it resolves to target.
func (a *Assembler) Patch8(off, target int) {
	a.buf[off] = byte(int8(target - (off + 1)))
}

// Patch32Abs writes v directly at off without any relative adjustment.
// Used for jump-table entries and other absolute 32-bit fields.
func (a *Assembler) Patch32Abs(off int, v uint32) { Put32LE(a.buf[off:], v) }

// SetByte sets a single byte at off. Used for short-jump back-patching.
func (a *Assembler) SetByte(off int, v byte) { a.buf[off] = v }

// ── Effective-address encoding ────────────────────────────────────────────────

// EncodeMemOp emits ModRM (and optional SIB + displacement) for [base + disp]
// with reg as the ModRM reg field.
func (a *Assembler) EncodeMemOp(reg, base int, disp int64) {
	switch {
	case disp == 0 && base&7 != 5: // mod=00; RBP/R13 always need an explicit disp
		if NeedsSIB(base) {
			a.Emit(ModRM(0b00, byte(reg&7), 4), SIBNoIndex(base))
		} else {
			a.Emit(ModRM(0b00, byte(reg&7), byte(base&7)))
		}
	case Fits8(disp): // mod=01, disp8
		if NeedsSIB(base) {
			a.Emit(ModRM(0b01, byte(reg&7), 4), SIBNoIndex(base), byte(int8(disp)))
		} else {
			a.Emit(ModRM(0b01, byte(reg&7), byte(base&7)), byte(int8(disp)))
		}
	default: // mod=10, disp32
		if NeedsSIB(base) {
			a.Emit(ModRM(0b10, byte(reg&7), 4), SIBNoIndex(base))
		} else {
			a.Emit(ModRM(0b10, byte(reg&7), byte(base&7)))
		}
		a.Emit32(uint32(int32(disp)))
	}
}

// ── Stack ─────────────────────────────────────────────────────────────────────

// Push emits: push reg64
func (a *Assembler) Push(reg int) {
	if reg >= 8 { a.Emit(0x41) }
	a.Emit(0x50 | byte(reg&7))
}

// Pop emits: pop reg64
func (a *Assembler) Pop(reg int) {
	if reg >= 8 { a.Emit(0x41) }
	a.Emit(0x58 | byte(reg&7))
}

// PushImm8 emits: push sign-extended imm8
func (a *Assembler) PushImm8(v int8) { a.Emit(0x6A, byte(v)) }

// PushImm32 emits: push sign-extended imm32
func (a *Assembler) PushImm32(v uint32) {
	a.Emit(0x68)
	a.Emit32(v)
}

// ── Move ──────────────────────────────────────────────────────────────────────

// MovRR emits: mov dst, src  (64-bit reg→reg)
func (a *Assembler) MovRR(dst, src int) {
	a.Emit(REXW(dst, src), 0x8B, ModRM(0b11, byte(dst&7), byte(src&7)))
}

// MovRR32 emits: mov dst32, src32  (zero-extends to 64 bits)
func (a *Assembler) MovRR32(dst, src int) {
	var rex byte
	if dst >= 8 { rex |= 0x44 } // REX.R
	if src >= 8 { rex |= 0x41 } // REX.B
	if rex != 0 { a.Emit(rex) }
	a.Emit(0x8B, ModRM(0b11, byte(dst&7), byte(src&7)))
}

// MovRI32 emits: mov dst32, imm32  (zero-extends to 64 bits, shorter encoding)
func (a *Assembler) MovRI32(dst int, v uint32) {
	if dst >= 8 { a.Emit(0x41) }
	a.Emit(0xB8 | byte(dst&7))
	a.Emit32(v)
}

// MovRI64 emits: mov dst, imm64
func (a *Assembler) MovRI64(dst int, v uint64) {
	a.Emit(REXWB(dst), 0xB8|byte(dst&7))
	a.Emit64(v)
}

// MovRI64Neg1 emits: mov dst, -1  (sign-extended imm32, REX.W + C7 /0)
func (a *Assembler) MovRI64Neg1(dst int) {
	a.Emit(REXWB(dst), 0xC7, ModRM(0b11, 0, byte(dst&7)))
	a.Emit32(0xFFFFFFFF)
}

// XorRR32 emits: xor dst32, dst32  (zeroes reg, zero-extends to 64 bits)
func (a *Assembler) XorRR32(reg int) {
	if reg >= 8 { a.Emit(0x45) }
	a.Emit(0x31, byte(0xC0|(uint8(reg&7)<<3)|uint8(reg&7)))
}

// Dec32 emits: dec reg32  (zero-extends to 64 bits)
func (a *Assembler) Dec32(reg int) {
	if reg >= 8 { a.Emit(0x41) } // REX.B
	a.Emit(0xFF, byte(0xC8|(reg&7)))
}

// ── LEA ───────────────────────────────────────────────────────────────────────

// LeaRIPRel32 emits: lea dst, [rip + ???]  (RIP-relative, 64-bit)
// Returns the offset of the 4-byte displacement field for relocation recording.
func (a *Assembler) LeaRIPRel32(dst int) int {
	a.Emit(REXW(dst, -1), 0x8D, byte(0x05|((dst&7)<<3)))
	return a.ZeroRel32()
}

// LeaScale emits: lea dst, [base + index*scale]  (64-bit, no displacement)
// scale must be 1, 2, 4, or 8.
// Handles the RBP/R13 base edge case (base&7==5) by using mod=01 with disp8=0.
func (a *Assembler) LeaScale(dst, base, index, scale int) {
	ss := byte(0)
	switch scale {
	case 2: ss = 1
	case 4: ss = 2
	case 8: ss = 3
	}
	rex := byte(0x48) // REX.W
	if dst >= 8   { rex |= 0x04 } // REX.R
	if index >= 8 { rex |= 0x02 } // REX.X
	if base >= 8  { rex |= 0x01 } // REX.B
	
	// FIXED: Explicitly cast integers to bytes for bitwise operations
	sib := (ss << 6) | (byte(index & 7) << 3) | byte(base & 7)
	
	if base&7 == 5 {
		// RBP/R13 as base in a mod=00 SIB encodes as "no base + disp32".
		// Use mod=01 with an explicit disp8=0 instead.
		a.Emit(rex, 0x8D, byte(0x44|((dst&7)<<3)), sib, 0x00)
	} else {
		a.Emit(rex, 0x8D, byte(0x04|((dst&7)<<3)), sib)
	}
}

// ── Bit manipulation ──────────────────────────────────────────────────────────

// BSR32 emits: bsr dst32, src32  (bit scan reverse; result undefined if src==0)
func (a *Assembler) BSR32(dst, src int) {
	var rex byte
	if dst >= 8 { rex |= 0x44 }
	if src >= 8 { rex |= 0x41 }
	if rex != 0 { a.Emit(rex) }
	a.Emit(0x0F, 0xBD, ModRM(0b11, byte(dst&7), byte(src&7)))
}

// ShlCL32 emits: shl reg32, cl  (shift left by CL; zero-extends to 64 bits)
// The caller must ensure RCX holds the shift count.
func (a *Assembler) ShlCL32(reg int) {
	if reg >= 8 { a.Emit(0x41) } // REX.B
	a.Emit(0xD3, byte(0xE0|(reg&7)))
}

// ── Arithmetic ────────────────────────────────────────────────────────────────

// AddRR emits: add dst, src  (64-bit)
func (a *Assembler) AddRR(dst, src int) {
	a.Emit(REXW(src, dst), 0x01, ModRM(0b11, byte(src&7), byte(dst&7)))
}

// AddRI emits: add reg, imm  (sign-extended imm8 or imm32, 64-bit)
func (a *Assembler) AddRI(reg int, v int64) {
	if Fits8(v) {
		a.Emit(REXWB(reg), 0x83, ModRM(0b11, 0, byte(reg&7)), byte(int8(v)))
	} else {
		a.Emit(REXWB(reg), 0x81, ModRM(0b11, 0, byte(reg&7)))
		a.Emit32(uint32(int32(v)))
	}
}

// SubRR emits: sub dst, src  (64-bit)
func (a *Assembler) SubRR(dst, src int) {
	a.Emit(REXW(src, dst), 0x29, ModRM(0b11, byte(src&7), byte(dst&7)))
}

// SubRI emits: sub reg, imm  (sign-extended imm8 or imm32, 64-bit)
func (a *Assembler) SubRI(reg int, v int64) {
	if Fits8(v) {
		a.Emit(REXWB(reg), 0x83, ModRM(0b11, 5, byte(reg&7)), byte(int8(v)))
	} else {
		a.Emit(REXWB(reg), 0x81, ModRM(0b11, 5, byte(reg&7)))
		a.Emit32(uint32(int32(v)))
	}
}

// AndRI8 emits: and reg, imm8  (sign-extended, 64-bit; useful for alignment masks)
func (a *Assembler) AndRI8(reg int, v int8) {
	a.Emit(REXWB(reg), 0x83, ModRM(0b11, 4, byte(reg&7)), byte(v))
}

// ── Compare / Test ────────────────────────────────────────────────────────────

// CmpRR emits: cmp dst, src  (dst − src flags, 64-bit)
func (a *Assembler) CmpRR(dst, src int) {
	a.Emit(REXW(src, dst), 0x39, ModRM(0b11, byte(src&7), byte(dst&7)))
}

// CmpRI emits: cmp reg, imm  (sign-extended imm8 or imm32, 64-bit)
func (a *Assembler) CmpRI(reg int, v int64) {
	if Fits8(v) {
		a.Emit(REXWB(reg), 0x83, ModRM(0b11, 7, byte(reg&7)), byte(int8(v)))
	} else {
		a.Emit(REXWB(reg), 0x81, ModRM(0b11, 7, byte(reg&7)))
		a.Emit32(uint32(int32(v)))
	}
}

// TestRR64 emits: test reg, reg  (64-bit; sets ZF if zero)
func (a *Assembler) TestRR64(reg int) {
	a.Emit(REXW(reg, reg), 0x85, ModRM(0b11, byte(reg&7), byte(reg&7)))
}

// TestRR32 emits: test reg32, reg32
func (a *Assembler) TestRR32(reg int) {
	if reg >= 8 { a.Emit(0x45) }
	a.Emit(0x85, byte(0xC0|(uint8(reg&7)<<3)|uint8(reg&7)))
}

// ── Jumps — all return the patch offset of the rel32 / rel8 field ─────────────

// JmpRel32 emits: jmp rel32
func (a *Assembler) JmpRel32() int { a.Emit(0xE9); return a.ZeroRel32() }

// JmpShort emits: jmp rel8 — returns patch offset of the rel8 byte.
func (a *Assembler) JmpShort() int {
	a.Emit(0xEB, 0x00)
	return len(a.buf) - 1
}

// JmpRel32Back emits a jmp rel32 that resolves to an already-known target.
func (a *Assembler) JmpRel32Back(target int) {
	a.Emit(0xE9)
	off := a.ZeroRel32()
	a.Patch32(off, target)
}

// JzRel32 emits: jz rel32  (jump if zero / equal)
func (a *Assembler) JzRel32() int { a.Emit(0x0F, 0x84); return a.ZeroRel32() }

// JnzRel32 emits: jnz rel32  (jump if not zero / not equal)
func (a *Assembler) JnzRel32() int { a.Emit(0x0F, 0x85); return a.ZeroRel32() }

// JeRel32 emits: je rel32  (alias for jz)
func (a *Assembler) JeRel32() int { return a.JzRel32() }

// JneRel32 emits: jne rel32  (alias for jnz)
func (a *Assembler) JneRel32() int { return a.JnzRel32() }

// JneRel32Back emits a jne rel32 that resolves to an already-known target.
// Used for backward CAS-retry loops.
func (a *Assembler) JneRel32Back(target int) {
	a.Emit(0x0F, 0x85)
	off := a.ZeroRel32()
	a.Patch32(off, target)
}

// JaRel32 emits: ja rel32  (jump if unsigned above)
func (a *Assembler) JaRel32() int { a.Emit(0x0F, 0x87); return a.ZeroRel32() }

// JaeRel32 emits: jae rel32  (jump if unsigned above or equal / carry clear)
func (a *Assembler) JaeRel32() int { a.Emit(0x0F, 0x83); return a.ZeroRel32() }

// JbeRel32 emits: jbe rel32  (jump if unsigned below or equal)
func (a *Assembler) JbeRel32() int { a.Emit(0x0F, 0x86); return a.ZeroRel32() }

// JbRel32 emits: jb rel32  (jump if unsigned below / carry set)
func (a *Assembler) JbRel32() int { a.Emit(0x0F, 0x82); return a.ZeroRel32() }

// JneRel8 emits: jne rel8 — returns patch offset of the rel8 byte.
func (a *Assembler) JneRel8() int { a.Emit(0x75, 0x00); return len(a.buf) - 1 }

// JeRel8 emits: je rel8 — returns patch offset of the rel8 byte.
func (a *Assembler) JeRel8() int { a.Emit(0x74, 0x00); return len(a.buf) - 1 }

// ── Calls ─────────────────────────────────────────────────────────────────────

// CallRel32 emits: call rel32 — returns the offset of the rel32 field.
func (a *Assembler) CallRel32() int { a.Emit(0xE8); return a.ZeroRel32() }

// CallReg emits: call reg  (indirect call through a register)
func (a *Assembler) CallReg(reg int) {
	if reg >= 8 { a.Emit(0x41) }
	a.Emit(0xFF, byte(0xD0|(reg&7)))
}

// ── Return / Trap ─────────────────────────────────────────────────────────────

func (a *Assembler) Ret() { a.Emit(0xC3) }
func (a *Assembler) UD2() { a.Emit(0x0F, 0x0B) }

// ── Memory loads ──────────────────────────────────────────────────────────────

// LoadMem64 emits: mov dst, [base + disp]  (64-bit)
func (a *Assembler) LoadMem64(dst, base int, disp int64) {
	a.Emit(REXW(dst, base), 0x8B)
	a.EncodeMemOp(dst, base, disp)
}

// LoadMem32ZX emits: mov dst32, [base + disp]  (zero-extends to 64 bits)
func (a *Assembler) LoadMem32ZX(dst, base int, disp int64) {
	a.Emit(REX(false, dst >= 8, false, base >= 8), 0x8B)
	a.EncodeMemOp(dst, base, disp)
}

// ── Memory stores ─────────────────────────────────────────────────────────────

// StoreMem64 emits: mov [base + disp], src  (64-bit)
func (a *Assembler) StoreMem64(base int, disp int64, src int) {
	a.Emit(REXW(src, base), 0x89)
	a.EncodeMemOp(src, base, disp)
}

// StoreMem32R emits: mov [base + disp], src32  (32-bit register store)
func (a *Assembler) StoreMem32R(base int, disp int64, src int) {
	var rex byte
	if src >= 8  { rex |= 0x44 } // REX.R extends ModRM.reg to src
	if base >= 8 { rex |= 0x41 } // REX.B extends ModRM.rm / base
	if rex != 0  { a.Emit(rex) }
	a.Emit(0x89) // MOV r/m32, r32 — no REX.W, so 32-bit
	a.EncodeMemOp(src, base, disp)
}

// StoreMem32Imm emits: mov dword [base + disp], imm32  (32-bit immediate store)
func (a *Assembler) StoreMem32Imm(base int, disp int64, imm uint32) {
	// No REX.W — this must be a 32-bit store.
	if base >= 8 { a.Emit(0x41) } // REX.B only
	a.Emit(0xC7)
	a.EncodeMemOp(0, base, disp)
	a.Emit32(imm)
}

// StoreMem64Imm emits: mov qword [base + disp], imm32  (sign-extended to 64 bits)
// Use this to store non-zero 64-bit values expressible as a signed 32-bit immediate.
func (a *Assembler) StoreMem64Imm(base int, disp int64, imm int32) {
	a.Emit(REXWB(base), 0xC7)
	a.EncodeMemOp(0, base, disp)
	a.Emit32(uint32(imm))
}

// StoreMem64Zero emits: mov qword [base + disp], 0
func (a *Assembler) StoreMem64Zero(base int, disp int64) {
	a.Emit(REXWB(base), 0xC7)
	a.EncodeMemOp(0, base, disp)
	a.Emit32(0)
}

// ── RSP-relative helpers (wasm operand stack) ─────────────────────────────────

// PeekRSP emits: mov dst, [rsp]  — loads TOS without consuming it.
func (a *Assembler) PeekRSP(dst int) {
	a.Emit(REX(true, dst >= 8, false, false), 0x8B)
	a.Emit(ModRM(0b00, byte(dst&7), 4), SIB(0, 4, 4))
}

// StoreRSPSlot emits: mov [rsp + slotFromTop*8], src
func (a *Assembler) StoreRSPSlot(src, slotFromTop int) {
	disp := int32(slotFromTop * 8)
	a.Emit(REX(true, src >= 8, false, false), 0x89)
	if disp == 0 {
		a.Emit(ModRM(0b00, byte(src&7), 4), SIB(0, 4, 4))
	} else if Fits8(int64(disp)) {
		a.Emit(ModRM(0b01, byte(src&7), 4), SIB(0, 4, 4), byte(int8(disp)))
	} else {
		a.Emit(ModRM(0b10, byte(src&7), 4), SIB(0, 4, 4))
		a.Emit32(uint32(disp))
	}
}

// AddRSP emits: add rsp, n  (n > 0, caller ensures multiple of 8)
func (a *Assembler) AddRSP(n int) {
	if n == 0 { return }
	if Fits8(int64(n)) {
		a.Emit(0x48, 0x83, 0xC4, byte(n))
	} else {
		a.Emit(0x48, 0x81, 0xC4)
		a.Emit32(uint32(n))
	}
}

// ── RBP-relative locals ───────────────────────────────────────────────────────

// LoadLocal64 emits: mov dst, [rbp − 8*(idx+1)]
func (a *Assembler) LoadLocal64(dst, idx int) {
	disp := -int32(8 * (idx + 1))
	a.Emit(REX(true, dst >= 8, false, false), 0x8B)
	if Fits8(int64(disp)) {
		a.Emit(ModRM(0b01, byte(dst&7), 5), byte(int8(disp)))
	} else {
		a.Emit(ModRM(0b10, byte(dst&7), 5))
		a.Emit32(uint32(disp))
	}
}

// StoreLocal64 emits: mov [rbp − 8*(idx+1)], src
func (a *Assembler) StoreLocal64(idx, src int) {
	disp := -int32(8 * (idx + 1))
	a.Emit(REX(true, src >= 8, false, false), 0x89)
	if Fits8(int64(disp)) {
		a.Emit(ModRM(0b01, byte(src&7), 5), byte(int8(disp)))
	} else {
		a.Emit(ModRM(0b10, byte(src&7), 5))
		a.Emit32(uint32(disp))
	}
}

// LoadRBPDisp emits: mov dst, [rbp + disp]
func (a *Assembler) LoadRBPDisp(dst int, disp int32) {
	a.Emit(REX(true, dst >= 8, false, false), 0x8B)
	if Fits8(int64(disp)) {
		a.Emit(ModRM(0b01, byte(dst&7), 5), byte(int8(disp)))
	} else {
		a.Emit(ModRM(0b10, byte(dst&7), 5))
		a.Emit32(uint32(disp))
	}
}

// ── Atomics ───────────────────────────────────────────────────────────────────

// LockIncMem64 emits: lock inc qword [base + disp]
func (a *Assembler) LockIncMem64(base int, disp int64) {
	a.Emit(0xF0, REXWB(base), 0xFF)
	a.EncodeMemOp(0, base, disp)
}

// LockDecMem64 emits: lock dec qword [base + disp]
func (a *Assembler) LockDecMem64(base int, disp int64) {
	a.Emit(0xF0, REXWB(base), 0xFF)
	a.EncodeMemOp(1, base, disp)
}

// LockXaddRAX emits: lock xadd [base + disp], rax
func (a *Assembler) LockXaddRAX(base int, disp int64) {
	a.Emit(0xF0, REXW(RAX, base), 0x0F, 0xC1)
	a.EncodeMemOp(RAX, base, disp)
}

// LockCmpxchg emits: lock cmpxchg [base + disp], src
// Compares RAX with [base+disp]; if equal stores src, else loads [base+disp]→RAX.
func (a *Assembler) LockCmpxchg(base int, disp int64, src int) {
	a.Emit(0xF0, REXW(src, base), 0x0F, 0xB1)
	a.EncodeMemOp(src, base, disp)
}

// ── String operations ─────────────────────────────────────────────────────────

// RepStosb emits: rep stosb  — fills [rdi .. rdi+rcx) with AL.
func (a *Assembler) RepStosb() { a.Emit(0xF3, 0xAA) }

// RepMovsb emits: rep movsb  — copies RCX bytes from [RSI] to [RDI].
// Both RSI and RDI advance; DF must be 0 (standard calling convention).
func (a *Assembler) RepMovsb() { a.Emit(0xF3, 0xA4) }

// ── Standard frame helpers ────────────────────────────────────────────────────

// Prologue emits the standard function prologue: push rbp, mov rbp rsp,
// push callee-saved regs, optionally sub rsp for alignment.
// Returns the alignment padding emitted (pass unchanged to Epilogue).
func (a *Assembler) Prologue(regs []int) int {
	a.Push(RBP)
	a.Emit(0x48, 0x89, 0xE5) // mov rbp, rsp
	for _, r := range regs {
		a.Push(r)
	}
	// SysV requires RSP % 16 == 0 before a call.
	// On entry: ret-addr (1) + rbp (1) + regs = (2+len) qwords pushed total.
	// If (1+len(regs)) is even, RSP is already aligned; otherwise sub 8.
	total := 1 + len(regs)
	align := 0
	if total%2 == 0 {
		align = 8
	}
	if align > 0 {
		a.SubRI(RSP, int64(align))
	}
	return align
}

// Epilogue emits the matching function epilogue.
func (a *Assembler) Epilogue(regs []int, align int) {
	if align > 0 {
		a.AddRI(RSP, int64(align))
	}
	for i := len(regs) - 1; i >= 0; i-- {
		a.Pop(regs[i])
	}
	a.Pop(RBP)
	a.Ret()
}

// ── Linux syscall helpers ─────────────────────────────────────────────────────

// Syscall emits: syscall
func (a *Assembler) Syscall() { a.Emit(0x0F, 0x05) }

// ExitGroup emits an exit_group(code) syscall (SYS_exit_group = 231).
func (a *Assembler) ExitGroup(code uint32) {
	a.MovRI32(RDI, code)
	a.MovRI32(RAX, 231)
	a.Syscall()
}

// MmapFixed emits a 6-argument anonymous mmap syscall.
// Caller must set RDI = desired address before calling this.
// Flags: PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS|MAP_FIXED, fd=-1, off=0.
func (a *Assembler) MmapFixed(length uint32) {
	a.MovRI32(RSI, length)
	a.Emit(0xBA, 0x03, 0x00, 0x00, 0x00)       // mov edx, 3
	a.Emit(0x41, 0xBA, 0x32, 0x00, 0x00, 0x00) // mov r10d, 0x32
	a.MovRI64Neg1(R8)                            // mov r8, -1 (fd)
	a.XorRR32(R9)                                // xor r9d, r9d (offset)
	a.MovRI32(RAX, 9)                            // mov eax, 9 (SYS_mmap)
	a.Syscall()
}

// CheckMmapError emits: cmp rax, -4096; ja .fail
// Returns the patch offset of the ja rel32 for the failure path.
func (a *Assembler) CheckMmapError() int {
	a.Emit(0x48, 0x3D)
	a.Emit32(0xFFFFF000) // -4096 as uint32
	return a.JaRel32()
}