// cpu/arm64/asm/asm.go
package asm

// Assembler is a raw AArch64 (A64) machine-code buffer.
//
// Every instruction is a fixed-width 32-bit little-endian word.  There are no
// REX prefixes, no ModRM/SIB bytes, and no variable-length encodings.
//
// Callers use Pos() to record relocation sites (always a 4-byte-aligned
// instruction word or an embedded immediate field) and Patch32 / Patch26 /
// Patch19 to back-fill forward references.
type Assembler struct {
	buf []byte
}

// Bytes returns the assembled byte slice.
func (a *Assembler) Bytes() []byte { return a.buf }

// Pos returns the current byte offset (always a multiple of 4 for well-formed
// A64 code).
func (a *Assembler) Pos() int { return len(a.buf) }

// Reset clears the buffer without releasing the backing allocation.
func (a *Assembler) Reset() { a.buf = a.buf[:0] }

// ── Raw emission ──────────────────────────────────────────────────────────────

// Emit32 appends one A64 instruction word (little-endian).
func (a *Assembler) Emit32(insn uint32) {
	a.buf = Append32LE(a.buf, insn)
}

// ZeroInsn appends a zero instruction word and returns its byte offset.
// Used as a forward-reference placeholder (the caller patches it later).
func (a *Assembler) ZeroInsn() int {
	off := len(a.buf)
	a.buf = append(a.buf, 0, 0, 0, 0)
	return off
}

// ── Patching ──────────────────────────────────────────────────────────────────

// PatchBranchImm26 back-patches a B/BL instruction at byte offset off so that
// it branches to target (also a byte offset).
// Computes: imm26 = (target − off) / 4, preserving the top 6 opcode bits.
func (a *Assembler) PatchBranchImm26(off, target int) {
	delta := int32((target - off) / 4)
	insn := uint32(a.buf[off]) | uint32(a.buf[off+1])<<8 |
		uint32(a.buf[off+2])<<16 | uint32(a.buf[off+3])<<24
	insn = (insn &^ 0x03FFFFFF) | Imm26Field(delta)
	Put32LE(a.buf[off:], insn)
}

// PatchCondImm19 back-patches a B.cond/CBZ/CBNZ instruction at byte offset off
// so that it branches to target.
func (a *Assembler) PatchCondImm19(off, target int) {
	delta := int32((target - off) / 4)
	insn := uint32(a.buf[off]) | uint32(a.buf[off+1])<<8 |
		uint32(a.buf[off+2])<<16 | uint32(a.buf[off+3])<<24
	insn = (insn &^ (0x7FFFF << 5)) | Imm19Field(delta)
	Put32LE(a.buf[off:], insn)
}

// PatchADR back-patches an ADR/ADRP instruction at off to point to target.
// For ADR: encodes (target − off) as a 21-bit signed PC-relative byte offset.
func (a *Assembler) PatchADR(off, target int) {
	delta := int32(target - off)
	// ADR imm encoding: immlo in [30:29], immhi in [23:5].
	immlo := uint32(delta) & 0x3
	immhi := uint32(delta>>2) & 0x7FFFF
	insn := uint32(a.buf[off]) | uint32(a.buf[off+1])<<8 |
		uint32(a.buf[off+2])<<16 | uint32(a.buf[off+3])<<24
	insn = (insn &^ ((0x7FFFF << 5) | (0x3 << 29))) |
		(immhi << 5) | (immlo << 29)
	Put32LE(a.buf[off:], insn)
}

// ── Stack ─────────────────────────────────────────────────────────────────────

// STP emits: stp Xt1, Xt2, [sp, #imm7*8]!  (pre-index; allocates stack space)
// imm7 must be negative and a multiple of 8 for 64-bit pairs; range [-512, 504].
func (a *Assembler) STP(rt1, rt2 int, imm7 int32) {
	// A64: sf=1, op2=10, V=0, L=0, imm7, Rt2, Rn=SP, Rt1
	// Pre-index: opc = 0b10, 0b101_0100_0 prefix
	// sf:opc2 for 64-bit pair pre-index = 0xA9800000
	v := uint32(0xA9800000) |
		uint32(imm7&0x7F)<<15 |
		uint32(rt2&0x1F)<<10 |
		uint32(SP)<<5 |
		uint32(rt1&0x1F)
	a.Emit32(v)
}

// LDP emits: ldp Xt1, Xt2, [sp], #imm7*8  (post-index; frees stack space)
func (a *Assembler) LDP(rt1, rt2 int, imm7 int32) {
	// Post-index 64-bit pair load: 0xA8C00000
	v := uint32(0xA8C00000) |
		uint32(imm7&0x7F)<<15 |
		uint32(rt2&0x1F)<<10 |
		uint32(SP)<<5 |
		uint32(rt1&0x1F)
	a.Emit32(v)
}

// ── Move ──────────────────────────────────────────────────────────────────────

// MOVZ emits: movz Xd, #imm16, lsl #(hw*16)  (zero other bits)
func (a *Assembler) MOVZ(dst int, imm uint16, hw uint32) {
	a.Emit32(SF(true) | 0x52800000 | HW(hw) | Imm16Field(imm) | Rd(dst))
}

// MOVK emits: movk Xd, #imm16, lsl #(hw*16)  (keep other bits)
func (a *Assembler) MOVK(dst int, imm uint16, hw uint32) {
	a.Emit32(SF(true) | 0x72800000 | HW(hw) | Imm16Field(imm) | Rd(dst))
}

// MOVZ32 emits: movz Wd, #imm16  (32-bit; hw must be 0 or 1)
func (a *Assembler) MOVZ32(dst int, imm uint16, hw uint32) {
	a.Emit32(0x52800000 | HW(hw) | Imm16Field(imm) | Rd(dst))
}

// MovRR emits: mov Xd, Xn  (64-bit register copy; encoded as ORR Xd, XZR, Xn)
func (a *Assembler) MovRR(dst, src int) {
	// ORR (shifted register), sf=1, shift=LSL, imm6=0, Rn=XZR
	a.Emit32(0xAA000000 | Rm(src) | Rn(XZR) | Rd(dst))
}

// MovRR32 emits: mov Wd, Wn  (32-bit; zero-extends to 64 bits)
func (a *Assembler) MovRR32(dst, src int) {
	a.Emit32(0x2A000000 | Rm(src) | Rn(XZR) | Rd(dst))
}

// MovSP emits: mov Xd, sp  or  mov sp, Xn  (encoded as ADD Xd, Xn/SP, #0)
func (a *Assembler) MovSP(dst, src int) {
	a.Emit32(SF(true) | 0x11000000 | Rn(src) | Rd(dst))
}

// ── Arithmetic ────────────────────────────────────────────────────────────────

// ADD emits: add Xd, Xn, Xm  (64-bit shifted register, LSL #0)
func (a *Assembler) ADD(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x0B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// ADD32 emits: add Wd, Wn, Wm  (32-bit)
func (a *Assembler) ADD32(dst, src1, src2 int) {
	a.Emit32(0x0B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// ADDSI emits: add Xd, Xn, #imm  (64-bit immediate, imm ∈ [0,4095])
func (a *Assembler) ADDSI(dst, src int, imm uint32) {
	a.Emit32(SF(true) | 0x11000000 | Imm12Field(imm) | Rn(src) | Rd(dst))
}

// ADDSI32 emits: add Wd, Wn, #imm  (32-bit immediate)
func (a *Assembler) ADDSI32(dst, src int, imm uint32) {
	a.Emit32(0x11000000 | Imm12Field(imm) | Rn(src) | Rd(dst))
}

// SUB emits: sub Xd, Xn, Xm  (64-bit shifted register, LSL #0)
func (a *Assembler) SUB(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x4B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// SUBSI emits: sub Xd, Xn, #imm  (64-bit immediate, imm ∈ [0,4095])
func (a *Assembler) SUBSI(dst, src int, imm uint32) {
	a.Emit32(SF(true) | 0x51000000 | Imm12Field(imm) | Rn(src) | Rd(dst))
}

// SUBSI32 emits: sub Wd, Wn, #imm  (32-bit)
func (a *Assembler) SUBSI32(dst, src int, imm uint32) {
	a.Emit32(0x51000000 | Imm12Field(imm) | Rn(src) | Rd(dst))
}

// SUBS emits: subs Xd, Xn, Xm  (sets flags; 64-bit)
func (a *Assembler) SUBS(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x6B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// ADDS emits: adds Xd, Xn, Xm  (sets flags; 64-bit)
func (a *Assembler) ADDS(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x2B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// NEG emits: neg Xd, Xm  (SUB Xd, XZR, Xm; 64-bit)
func (a *Assembler) NEG(dst, src int) {
	a.Emit32(SF(true) | 0x4B000000 | Rm(src) | Rn(XZR) | Rd(dst))
}

// MUL emits: mul Xd, Xn, Xm  (MADD Xd, Xn, Xm, XZR; 64-bit)
func (a *Assembler) MUL(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x1B000000 | Rm(src2) | Ra(XZR) | Rn(src1) | Rd(dst))
}

// SDIV emits: sdiv Xd, Xn, Xm  (signed 64-bit divide)
func (a *Assembler) SDIV(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x1AC00C00 | Rm(src2) | Rn(src1) | Rd(dst))
}

// UDIV emits: udiv Xd, Xn, Xm  (unsigned 64-bit divide)
func (a *Assembler) UDIV(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x1AC00800 | Rm(src2) | Rn(src1) | Rd(dst))
}

// MSUB emits: msub Xd, Xn, Xm, Xa  (Xd = Xa − Xn*Xm; used for remainder)
func (a *Assembler) MSUB(dst, src1, src2, acc int) {
	a.Emit32(SF(true) | 0x9B008000 | Rm(src2) | Ra(acc) | Rn(src1) | Rd(dst))
}

// ── Logical ───────────────────────────────────────────────────────────────────

// AND emits: and Xd, Xn, Xm  (64-bit shifted register)
func (a *Assembler) AND(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x0A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// ORR emits: orr Xd, Xn, Xm  (64-bit shifted register)
func (a *Assembler) ORR(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x2A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// EOR emits: eor Xd, Xn, Xm  (64-bit shifted register)
func (a *Assembler) EOR(dst, src1, src2 int) {
	a.Emit32(SF(true) | 0x4A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// LSL emits: lsl Xd, Xn, Xm  (variable shift; LSLV)
func (a *Assembler) LSL(dst, src, shift int) {
	a.Emit32(SF(true) | 0x1AC02000 | Rm(shift) | Rn(src) | Rd(dst))
}

// LSR emits: lsr Xd, Xn, Xm  (logical right shift variable; LSRV)
func (a *Assembler) LSR(dst, src, shift int) {
	a.Emit32(SF(true) | 0x1AC02400 | Rm(shift) | Rn(src) | Rd(dst))
}

// ASR emits: asr Xd, Xn, Xm  (arithmetic right shift variable; ASRV)
func (a *Assembler) ASR(dst, src, shift int) {
	a.Emit32(SF(true) | 0x1AC02800 | Rm(shift) | Rn(src) | Rd(dst))
}

// ROR emits: ror Xd, Xn, Xm  (rotate right variable; RORV)
func (a *Assembler) ROR(dst, src, shift int) {
	a.Emit32(SF(true) | 0x1AC02C00 | Rm(shift) | Rn(src) | Rd(dst))
}

// LSLI emits: lsl Xd, Xn, #shift  (immediate; encoded as UBFM)
func (a *Assembler) LSLI(dst, src int, shift uint32) {
	// UBFM 64-bit: 0xD3400000 (N=1 already encoded)
	immr := (64 - shift) & 0x3F
	imms := 63 - shift
	a.Emit32(0xD3400000 | uint32(immr)<<16 | uint32(imms)<<10 | Rn(src) | Rd(dst))
}

// LSRI emits: lsr Xd, Xn, #shift  (immediate; encoded as UBFM)
func (a *Assembler) LSRI(dst, src int, shift uint32) {
	a.Emit32(0xD3400000 | uint32(shift)<<16 | uint32(63)<<10 | Rn(src) | Rd(dst))
}

// ASRI emits: asr Xd, Xn, #shift  (immediate; encoded as SBFM)
func (a *Assembler) ASRI(dst, src int, shift uint32) {
	// SBFM 64-bit: 0x93400000 (N=1 already encoded)
	a.Emit32(0x93400000 | uint32(shift)<<16 | uint32(63)<<10 | Rn(src) | Rd(dst))
}

// CLZ emits: clz Xd, Xn  (count leading zeros, 64-bit)
func (a *Assembler) CLZ(dst, src int) {
	a.Emit32(SF(true) | 0x5AC01000 | Rn(src) | Rd(dst))
}

// RBIT emits: rbit Xd, Xn  (reverse bits; used to implement CTZ via CLZ(RBIT(x)))
func (a *Assembler) RBIT(dst, src int) {
	a.Emit32(SF(true) | 0x5AC00000 | Rn(src) | Rd(dst))
}

// CNT (VCNT) is not available in base A64 for GPRs; CTZ is done as CLZ(RBIT(x)).
// For popcount we use the NEON vcnt path — added later if needed.

// ── Compare / Test ────────────────────────────────────────────────────────────

// CMP emits: cmp Xn, Xm  (SUBS XZR, Xn, Xm; sets flags; 64-bit)
func (a *Assembler) CMP(src1, src2 int) {
	a.Emit32(SF(true) | 0x6B000000 | Rm(src2) | Rn(src1) | Rd(XZR))
}

// CMPI emits: cmp Xn, #imm  (SUBS XZR, Xn, #imm; 64-bit)
func (a *Assembler) CMPI(src int, imm uint32) {
	a.Emit32(SF(true) | 0x71000000 | Imm12Field(imm) | Rn(src) | Rd(XZR))
}

// TST emits: tst Xn, Xm  (ANDS XZR, Xn, Xm; sets flags)
func (a *Assembler) TST(src1, src2 int) {
	a.Emit32(SF(true) | 0xEA000000 | Rm(src2) | Rn(src1) | Rd(XZR))
}

// CBZ emits: cbz Xn, #target  (returns patch offset; target patched later)
func (a *Assembler) CBZ(reg int) int {
	off := a.Pos()
	a.Emit32(SF(true) | 0x34000000 | Rd(reg)) // Rd field holds the tested reg
	return off
}

// CBNZ emits: cbnz Xn, #target  (returns patch offset)
func (a *Assembler) CBNZ(reg int) int {
	off := a.Pos()
	a.Emit32(SF(true) | 0x35000000 | Rd(reg))
	return off
}

// ── Conditional select ────────────────────────────────────────────────────────

// CSEL emits: csel Xd, Xn, Xm, cond  (Xd = (cond) ? Xn : Xm)
// cond is the 4-bit condition code (use the Cond* constants below).
func (a *Assembler) CSEL(dst, src1, src2 int, cond uint32) {
	a.Emit32(SF(true) | 0x9A800000 | Rm(src2) | (cond<<12) | Rn(src1) | Rd(dst))
}

// Condition code constants for CSEL, B.cond, etc.
const (
	CondEQ = 0x0 // Equal / Zero
	CondNE = 0x1 // Not equal / Not zero
	CondCS = 0x2 // Carry set / Unsigned >=
	CondCC = 0x3 // Carry clear / Unsigned <
	CondMI = 0x4 // Minus / Negative
	CondPL = 0x5 // Plus / Positive or zero
	CondVS = 0x6 // Overflow
	CondVC = 0x7 // No overflow
	CondHI = 0x8 // Unsigned higher
	CondLS = 0x9 // Unsigned lower or same
	CondGE = 0xA // Signed >=
	CondLT = 0xB // Signed <
	CondGT = 0xC // Signed >
	CondLE = 0xD // Signed <=
	CondAL = 0xE // Always
)

// ── Memory loads ──────────────────────────────────────────────────────────────
//
// Unsigned-offset addressing: address = Xbase + uimm * access_size.
// The pimm helpers below take the *byte* offset and right-shift it by the
// access log2 to produce the encoded uimm field.

// LDR64 emits: ldr Xd, [Xn, #off]  (64-bit; off must be a multiple of 8, max 32760)
func (a *Assembler) LDR64(dst, base int, off uint32) {
	// opc=0b01, V=0, size=0b11: base opcode 0xF9400000
	a.Emit32(0xF9400000 | (off/8)<<10 | Rn(base) | Rd(dst))
}

// LDR32 emits: ldr Wd, [Xn, #off]  (32-bit zero-extend; off multiple of 4, max 16380)
func (a *Assembler) LDR32(dst, base int, off uint32) {
	a.Emit32(0xB9400000 | (off/4)<<10 | Rn(base) | Rd(dst))
}

// LDRH emits: ldrh Wd, [Xn, #off]  (16-bit zero-extend; off multiple of 2)
func (a *Assembler) LDRH(dst, base int, off uint32) {
	a.Emit32(0x79400000 | (off/2)<<10 | Rn(base) | Rd(dst))
}

// LDRB emits: ldrb Wd, [Xn, #off]  (8-bit zero-extend; any byte offset)
func (a *Assembler) LDRB(dst, base int, off uint32) {
	a.Emit32(0x39400000 | off<<10 | Rn(base) | Rd(dst))
}

// LDRSW emits: ldrsw Xd, [Xn, #off]  (32-bit sign-extend to 64; off multiple of 4)
func (a *Assembler) LDRSW(dst, base int, off uint32) {
	a.Emit32(0xB9800000 | (off/4)<<10 | Rn(base) | Rd(dst))
}

// LDRSH emits: ldrsh Xd, [Xn, #off]  (16-bit sign-extend to 64; off multiple of 2)
func (a *Assembler) LDRSH(dst, base int, off uint32) {
	a.Emit32(0x79800000 | (off/2)<<10 | Rn(base) | Rd(dst))
}

// LDRSB emits: ldrsb Xd, [Xn, #off]  (8-bit sign-extend to 64)
func (a *Assembler) LDRSB(dst, base int, off uint32) {
	a.Emit32(0x39800000 | off<<10 | Rn(base) | Rd(dst))
}

// LDR64Reg emits: ldr Xd, [Xbase, Xoff, lsl #3]  (register offset, scaled)
// Used for wasm memory access when offset is in a register.
func (a *Assembler) LDR64Reg(dst, base, off int) {
	// opc=0b01 size=0b11 option=0b011 S=1 (lsl #3)
	a.Emit32(0xF8607800 | Rm(off) | Rn(base) | Rd(dst))
}

// LDR64Unscaled emits: ldur Xd, [Xn, #simm9]  (unscaled signed offset, any alignment)
// simm9 range: [-256, 255].
func (a *Assembler) LDR64Unscaled(dst, base int, simm9 int32) {
	a.Emit32(0xF8400000 | uint32(simm9&0x1FF)<<12 | Rn(base) | Rd(dst))
}

// ── Memory stores ─────────────────────────────────────────────────────────────

// STR64 emits: str Xd, [Xn, #off]  (64-bit; off multiple of 8)
func (a *Assembler) STR64(src, base int, off uint32) {
	a.Emit32(0xF9000000 | (off/8)<<10 | Rn(base) | Rd(src))
}

// STR32 emits: str Wd, [Xn, #off]  (32-bit; off multiple of 4)
func (a *Assembler) STR32(src, base int, off uint32) {
	a.Emit32(0xB9000000 | (off/4)<<10 | Rn(base) | Rd(src))
}

// STRH emits: strh Wd, [Xn, #off]  (16-bit; off multiple of 2)
func (a *Assembler) STRH(src, base int, off uint32) {
	a.Emit32(0x79000000 | (off/2)<<10 | Rn(base) | Rd(src))
}

// STRB emits: strb Wd, [Xn, #off]  (8-bit; any byte offset)
func (a *Assembler) STRB(src, base int, off uint32) {
	a.Emit32(0x39000000 | off<<10 | Rn(base) | Rd(src))
}

// STR64Unscaled emits: stur Xd, [Xn, #simm9]  (unscaled, any offset)
func (a *Assembler) STR64Unscaled(src, base int, simm9 int32) {
	a.Emit32(0xF8000000 | uint32(simm9&0x1FF)<<12 | Rn(base) | Rd(src))
}

// ── Branches ──────────────────────────────────────────────────────────────────

// B emits: b #target  (unconditional; returns patch offset)
func (a *Assembler) B() int {
	off := a.Pos()
	a.Emit32(0x14000000) // imm26 = 0 placeholder
	return off
}

// BBack emits a B instruction resolving immediately to an already-known target.
func (a *Assembler) BBack(target int) {
	off := a.Pos()
	a.Emit32(0x14000000)
	a.PatchBranchImm26(off, target)
}

// BL emits: bl #target  (call; saves return address in LR/X30; returns patch offset)
func (a *Assembler) BL() int {
	off := a.Pos()
	a.Emit32(0x94000000)
	return off
}

// BR emits: br Xn  (indirect unconditional branch through register)
func (a *Assembler) BR(reg int) {
	a.Emit32(0xD61F0000 | Rn(reg))
}

// BLR emits: blr Xn  (indirect call through register)
func (a *Assembler) BLR(reg int) {
	a.Emit32(0xD63F0000 | Rn(reg))
}

// RET emits: ret  (branches to X30/LR; standard function return)
func (a *Assembler) RET() {
	a.Emit32(0xD65F03C0) // ret x30
}

// BCond emits: b.cond #target  (returns patch offset)
func (a *Assembler) BCond(cond uint32) int {
	off := a.Pos()
	a.Emit32(0x54000000 | Cond(cond))
	return off
}

// BCondBack emits a B.cond resolving immediately to a known target.
func (a *Assembler) BCondBack(cond uint32, target int) {
	off := a.Pos()
	a.Emit32(0x54000000 | Cond(cond))
	a.PatchCondImm19(off, target)
}

// ── Calls / Return ────────────────────────────────────────────────────────────

// NOP emits a no-op instruction.
func (a *Assembler) NOP() { a.Emit32(0xD503201F) }

// BRK emits: brk #0  (software breakpoint / undefined trap)
func (a *Assembler) BRK() { a.Emit32(0xD4200000) }

// ── Atomics ───────────────────────────────────────────────────────────────────
//
// ARMv8.1 LSE (Large System Extensions) atomic instructions.
// These are available on all Apple Silicon, all Graviton2+, and all
// modern Linux arm64 servers.  For v8.0 compatibility we would need
// LDAXR/STLXR CAS loops — left for a future compat flag.

// LDADD emits: ldadd Xm, Xd, [Xn]  (atomic add; returns old value in Xd)
// This is the ARMv8.1-A LSE ldadd instruction (acquire+release form).
func (a *Assembler) LDADD(rs, rd, base int) {
	// A64: size=11, V=0, A=1, R=1, Rs, o3=0, Rt2=11111, Rn, Rd
	// LDADDAL: 0xF8E00000 | rs<<16 | base<<5 | rd
	a.Emit32(0xF8E00000 | Rm(rs) | Rn(base) | Rd(rd))
}

// STADD emits: stadd Xm, [Xn]  (atomic add, no return; STADDL semantics)
func (a *Assembler) STADD(rs, base int) {
	a.Emit32(0xF8200000 | Rm(rs) | Rn(base) | Rd(XZR))
}

// CASAL emits: casal Xm, Xd, [Xn]  (compare-and-swap, acquire+release)
// If [Xn] == Xd: [Xn] = Xm, else Xd = [Xn].
func (a *Assembler) CASAL(expected, desired, base int) {
	// 0xC8E0FC00 | rs=desired[20:16] | rn=base[9:5] | rt=expected[4:0]
	a.Emit32(0xC8E0FC00 | Rm(desired) | Rn(base) | Rd(expected))
}

// ── System ────────────────────────────────────────────────────────────────────

// SVC emits: svc #0  (Linux syscall; number in X8, args in X0–X5)
func (a *Assembler) SVC() { a.Emit32(0xD4000001) }

// DMB emits: dmb ish  (data memory barrier, inner shareable domain)
// Required before releasing a lock or after acquiring one.
func (a *Assembler) DMB() { a.Emit32(0xD5033BBF) }

// ── Frame helpers ─────────────────────────────────────────────────────────────

// Prologue emits a standard AAPCS64 function prologue:
//   stp  x29, x30, [sp, #-frameSize]!
//   mov  x29, sp
//   (stp  callee-saved pairs...)
//
// regs is the list of extra callee-saved registers to push beyond FP/LR.
// Must be an even-length list (pair them for STP); caller pads with XZR if odd.
// Returns the total frame size in bytes (always a multiple of 16).
func (a *Assembler) Prologue(regs []int) int {
	// Pairs: FP+LR first, then caller-supplied pairs.
	// Each pair costs 16 bytes.  Total must be 16-byte aligned.
	nPairs := 1 + len(regs)/2
	frameSize := nPairs * 16

	// stp x29, x30, [sp, #-frameSize]!   (pre-decrement)
	imm7 := int32(-frameSize / 8) // imm7 is in units of 8 bytes for 64-bit pairs
	a.STP(FP, LR, imm7)
	a.MovSP(FP, SP) // mov x29, sp

	for i := 0; i+1 < len(regs); i += 2 {
		off := uint32((i/2 + 1) * 16)
		a.STR64(regs[i], SP, off)
		a.STR64(regs[i+1], SP, off+8)
	}
	return frameSize
}

// Epilogue emits the matching AAPCS64 function epilogue and RET.
func (a *Assembler) Epilogue(regs []int, frameSize int) {
	for i := 0; i+1 < len(regs); i += 2 {
		off := uint32((i/2 + 1) * 16)
		a.LDR64(regs[i], SP, off)
		a.LDR64(regs[i+1], SP, off+8)
	}
	// ldp x29, x30, [sp], #frameSize  (post-increment)
	imm7 := int32(frameSize / 8)
	a.LDP(FP, LR, imm7)
	a.RET()
}

// ExitGroup emits the arm64 Linux exit_group(code) syscall (SYS_exit_group = 94).
func (a *Assembler) ExitGroup(code uint32) {
	// movz x0, #code
	a.MOVZ32(X0, uint16(code), 0)
	// movz x8, #94
	a.MOVZ32(X8, 94, 0)
	a.SVC()
}

// MmapAnon emits: anonymous mmap(NULL, length, PROT_READ|PROT_WRITE,
// MAP_PRIVATE|MAP_ANONYMOUS, -1, 0)  — Linux arm64 syscall (SYS_mmap = 222).
// Caller must load length into X1 before calling this helper.
// Returns mapping start in X0.
func (a *Assembler) MmapAnon() {
	// x0 = NULL (addr)
	a.MOVZ32(X0, 0, 0)          // mov w0, #0
	// x1 = length — set by caller
	// x2 = PROT_READ|PROT_WRITE = 3
	a.MOVZ32(X2, 3, 0)
	// x3 = MAP_PRIVATE|MAP_ANONYMOUS = 0x22
	a.MOVZ32(X3, 0x22, 0)
	// x4 = -1 (fd); use movn w4, #0 → w4 = ~0 = -1
	a.Emit32(0x12800004) // movn w4, #0
	// x5 = 0 (offset)
	a.MOVZ32(X5, 0, 0)
	// x8 = 222 (SYS_mmap)
	a.MOVZ32(X8, 222, 0)
	a.SVC()
}

// CheckMmapError emits: cmn x0, #4096; b.hi .fail
// Returns the patch offset of the b.hi for the failure path.
// (On error mmap returns a value in [-4096, -1], i.e. x0 > ~4096u.)
func (a *Assembler) CheckMmapError() int {
	// cmn x0, #4096  (= adds xzr, x0, #4096; sets flags)
	a.Emit32(SF(true) | 0x31000000 | Imm12Field(4096) | Rn(X0) | Rd(XZR))
	return a.BCond(CondHI) // b.hi fail
}

// ── additions to cpu/arm64/asm/asm.go ────────────────────────────────────────

// STR64PreIndex emits: str Xt, [Xn, #simm9]!  (pre-decrement; Xn updated before store)
// Used for operand-stack push.  simm9 range: [-256, 255].
func (a *Assembler) STR64PreIndex(src, base int, simm9 int32) {
	// size=11 V=0 opc=00 pre-index(11): base 0xF8000C00
	a.Emit32(0xF8000C00 | uint32(simm9&0x1FF)<<12 | Rn(base) | Rd(src))
}

// LDR64PostIndex emits: ldr Xt, [Xn], #simm9  (load then post-increment Xn)
// Used for operand-stack pop.
func (a *Assembler) LDR64PostIndex(dst, base int, simm9 int32) {
	// size=11 V=0 opc=01 post-index(01): base 0xF8400400
	a.Emit32(0xF8400400 | uint32(simm9&0x1FF)<<12 | Rn(base) | Rd(dst))
}

// ADRP emits: adrp Xd, #0  (placeholder; linker patches with ADRPHi21 reloc)
// Returns the byte offset of the instruction for relocation recording.
func (a *Assembler) ADRP(dst int) int {
	off := a.Pos()
	a.Emit32(0x90000000 | Rd(dst))
	return off
}

// ADDSIShifted emits: add Xd, Xn, #imm12, lsl #12  (bit 22 = shift=1)
// Covers the upper 12 bits of a 24-bit offset in two-ADD sequences.
func (a *Assembler) ADDSIShifted(dst, src int, imm uint32) {
	a.Emit32(SF(true) | 0x11400000 | Imm12Field(imm) | Rn(src) | Rd(dst))
}

// ADDExtReg emits: add Xd, Xn, Wm, uxtw  (zero-extend 32-bit Wm then add to Xn)
// Used to compute native_addr = MemBase + zero_extend(wasm_i32_offset).
func (a *Assembler) ADDExtReg(dst, base, ext32 int) {
	// ADD extended register; option=UXTW(010) at bits[15:13]
	a.Emit32(0x8B200000 |
		uint32(ext32&0x1F)<<16 |
		(0x2 << 13) | // UXTW
		uint32(base&0x1F)<<5 |
		uint32(dst&0x1F))
}

// LDRSB32 emits: ldrsb Wd, [Xn, #off]  (8-bit sign-extend to 32; upper 32 zeroed)
func (a *Assembler) LDRSB32(dst, base int, off uint32) {
	a.Emit32(0x39C00000 | off<<10 | Rn(base) | Rd(dst))
}

// LDRSH32 emits: ldrsh Wd, [Xn, #off]  (16-bit sign-extend to 32; upper 32 zeroed)
func (a *Assembler) LDRSH32(dst, base int, off uint32) {
	a.Emit32(0x79C00000 | (off/2)<<10 | Rn(base) | Rd(dst))
}

// CSET emits: cset Xd, cond  (Xd = cond ? 1 : 0; 64-bit)
// Encoded as: csinc Xd, xzr, xzr, NOT(cond).
func (a *Assembler) CSET(dst int, cond uint32) {
	a.Emit32(0x9A800400 | Rm(XZR) | ((cond^1)<<12) | Rn(XZR) | Rd(dst))
}

// CSET32 emits: cset Wd, cond  (32-bit; zero-extends to 64)
func (a *Assembler) CSET32(dst int, cond uint32) {
	a.Emit32(0x1A800400 | Rm(XZR) | ((cond^1)<<12) | Rn(XZR) | Rd(dst))
}

// CMP32 emits: cmp Wn, Wm  (32-bit; sets flags based on Wn−Wm)
func (a *Assembler) CMP32(src1, src2 int) {
	a.Emit32(0x6B000000 | Rm(src2) | Rn(src1) | Rd(XZR))
}

// CMPI32 emits: cmp Wn, #imm  (32-bit immediate comparison)
func (a *Assembler) CMPI32(src int, imm uint32) {
	a.Emit32(0x71000000 | Imm12Field(imm) | Rn(src) | Rd(XZR))
}

// MUL32 emits: mul Wd, Wn, Wm  (32-bit)
func (a *Assembler) MUL32(dst, src1, src2 int) {
	a.Emit32(0x1B000000 | Rm(src2) | Ra(XZR) | Rn(src1) | Rd(dst))
}

// SDIV32 emits: sdiv Wd, Wn, Wm  (signed 32-bit divide)
func (a *Assembler) SDIV32(dst, src1, src2 int) {
	a.Emit32(0x1AC00C00 | Rm(src2) | Rn(src1) | Rd(dst))
}

// UDIV32 emits: udiv Wd, Wn, Wm  (unsigned 32-bit divide)
func (a *Assembler) UDIV32(dst, src1, src2 int) {
	a.Emit32(0x1AC00800 | Rm(src2) | Rn(src1) | Rd(dst))
}

// MSUB32 emits: msub Wd, Wn, Wm, Wa  (Wd = Wa − Wn×Wm; used for i32 remainder)
func (a *Assembler) MSUB32(dst, src1, src2, acc int) {
	a.Emit32(0x1B008000 | Rm(src2) | Ra(acc) | Rn(src1) | Rd(dst))
}

// CLZ32 emits: clz Wd, Wn  (count leading zeros, 32-bit)
func (a *Assembler) CLZ32(dst, src int) {
	a.Emit32(0x5AC01000 | Rn(src) | Rd(dst))
}

// RBIT32 emits: rbit Wd, Wn  (reverse bits; used for CTZ via CLZ(RBIT(x)))
func (a *Assembler) RBIT32(dst, src int) {
	a.Emit32(0x5AC00000 | Rn(src) | Rd(dst))
}

// LSL32 emits: lsl Wd, Wn, Wm  (variable shift left, 32-bit)
func (a *Assembler) LSL32(dst, src, shift int) {
	a.Emit32(0x1AC02000 | Rm(shift) | Rn(src) | Rd(dst))
}

// LSR32 emits: lsr Wd, Wn, Wm  (logical right shift, 32-bit)
func (a *Assembler) LSR32(dst, src, shift int) {
	a.Emit32(0x1AC02400 | Rm(src) | Rn(src) | Rd(dst))
}

// ASR32 emits: asr Wd, Wn, Wm  (arithmetic right shift, 32-bit)
func (a *Assembler) ASR32(dst, src, shift int) {
	a.Emit32(0x1AC02800 | Rm(shift) | Rn(src) | Rd(dst))
}

// ROR32 emits: ror Wd, Wn, Wm  (rotate right variable, 32-bit)
func (a *Assembler) ROR32(dst, src, shift int) {
	a.Emit32(0x1AC02C00 | Rm(shift) | Rn(src) | Rd(dst))
}

// SUB32 emits: sub Wd, Wn, Wm  (32-bit)
func (a *Assembler) SUB32(dst, src1, src2 int) {
	a.Emit32(0x4B000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// AND32 emits: and Wd, Wn, Wm  (32-bit)
func (a *Assembler) AND32(dst, src1, src2 int) {
	a.Emit32(0x0A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// ORR32 emits: orr Wd, Wn, Wm  (32-bit)
func (a *Assembler) ORR32(dst, src1, src2 int) {
	a.Emit32(0x2A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// EOR32 emits: eor Wd, Wn, Wm  (32-bit)
func (a *Assembler) EOR32(dst, src1, src2 int) {
	a.Emit32(0x4A000000 | Rm(src2) | Rn(src1) | Rd(dst))
}

// LDADD32 emits: ldaddal Wm, Wd, [Xn]  (32-bit acquire+release atomic add)
// old = [Xn], [Xn] += Wm, Wd = old
func (a *Assembler) LDADD32(rs, rd, base int) {
	a.Emit32(0xB8E00000 | Rm(rs) | Rn(base) | Rd(rd))
}

// SXTW emits: sxtw Xd, Wn  (sign-extend 32 → 64; encoded as SBFM imms=31)
func (a *Assembler) SXTW(dst, src int) {
	a.Emit32(0x93407C00 | Rn(src) | Rd(dst))
}