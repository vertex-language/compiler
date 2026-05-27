// cpu/arm64/asm/encode.go
package asm

// ── Bit-field helpers ─────────────────────────────────────────────────────────
//
// Every A64 instruction is a 32-bit little-endian word.  All encoding is done
// by OR-ing named bit fields into a base opcode word.

// SF returns the sf bit (bit 31) for 64-bit register-size selection.
// sf=1 → 64-bit (X registers); sf=0 → 32-bit (W registers, zero-extends).
func SF(is64 bool) uint32 {
	if is64 {
		return 1 << 31
	}
	return 0
}

// Rd encodes a destination register number into bits [4:0].
func Rd(reg int) uint32 { return uint32(reg & 0x1F) }

// Rn encodes a first source register into bits [9:5].
func Rn(reg int) uint32 { return uint32(reg&0x1F) << 5 }

// Rm encodes a second source register into bits [20:16].
func Rm(reg int) uint32 { return uint32(reg&0x1F) << 16 }

// Ra encodes an accumulator register into bits [14:10].
func Ra(reg int) uint32 { return uint32(reg&0x1F) << 10 }

// Imm12Field encodes a 12-bit unsigned immediate into bits [21:10].
func Imm12Field(imm uint32) uint32 { return (imm & 0xFFF) << 10 }

// Imm16Field encodes a 16-bit immediate into bits [20:5] (MOVZ/MOVK/MOVN).
func Imm16Field(imm uint16) uint32 { return uint32(imm) << 5 }

// HW encodes the hw (half-word shift) field into bits [22:21] (MOVZ/MOVK/MOVN).
// hw selects which 16-bit chunk: 0→bits[15:0], 1→bits[31:16],
// 2→bits[47:32], 3→bits[63:48].
func HW(hw uint32) uint32 { return (hw & 0x3) << 21 }

// Shift2 encodes a 2-bit shift type into bits [23:22] for shifted-register ops.
// 0=LSL, 1=LSR, 2=ASR, 3=ROR.
func Shift2(sh uint32) uint32 { return (sh & 0x3) << 22 }

// Imm6Field encodes a 6-bit shift amount into bits [15:10].
func Imm6Field(imm uint32) uint32 { return (imm & 0x3F) << 10 }

// Cond encodes a 4-bit condition code into bits [3:0] (B.cond).
// 0=EQ,1=NE,2=CS,3=CC,4=MI,5=PL,6=VS,7=VC,8=HI,9=LS,10=GE,11=LT,12=GT,13=LE.
func Cond(c uint32) uint32 { return c & 0xF }

// Imm19Field encodes a 19-bit PC-relative offset into bits [23:5] (B.cond, CBZ).
func Imm19Field(imm int32) uint32 { return uint32(imm&0x7FFFF) << 5 }

// Imm26Field encodes a 26-bit PC-relative offset into bits [25:0] (B, BL).
func Imm26Field(imm int32) uint32 { return uint32(imm & 0x3FFFFFF) }

// ── Little-endian 32-bit helpers ──────────────────────────────────────────────

// Put32LE writes a 32-bit little-endian word at b[0..3].
func Put32LE(b []byte, v uint32) {
	b[0] = byte(v); b[1] = byte(v >> 8)
	b[2] = byte(v >> 16); b[3] = byte(v >> 24)
}

// Append32LE appends a 32-bit little-endian word to dst and returns the result.
func Append32LE(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}