package asm

// REX builds a REX prefix byte.
// w=64-bit operand size, r=ModRM reg field ext, x=SIB index ext, b=r/m or base ext.
func REX(w, r, x, b bool) byte {
	v := byte(0x40)
	if w { v |= 0x08 }
	if r { v |= 0x04 }
	if x { v |= 0x02 }
	if b { v |= 0x01 }
	return v
}

// REXW returns REX.W with REX.R set if reg≥8 and REX.B set if rm≥8.
// Pass -1 for an unused field.
func REXW(reg, rm int) byte {
	v := byte(0x48)
	if reg >= 8 { v |= 0x04 }
	if rm  >= 8 { v |= 0x01 }
	return v
}

// REXWB returns REX.W|REX.B for single-register operands (push/pop, add imm, etc.).
func REXWB(reg int) byte {
	v := byte(0x48)
	if reg >= 8 { v |= 0x01 }
	return v
}

// ModRM builds a ModRM byte.
func ModRM(mod, reg, rm byte) byte {
	return (mod << 6) | ((reg & 7) << 3) | (rm & 7)
}

// SIB builds a SIB byte.
func SIB(scale, index, base byte) byte {
	return (scale << 6) | ((index & 7) << 3) | (base & 7)
}

// SIBNoIndex returns a SIB byte for [base] with no index (index field = 4 sentinel).
func SIBNoIndex(base int) byte { return SIB(0, 4, byte(base&7)) }

// NeedsSIB reports whether base requires a SIB byte in an effective address.
// R12 shares the 3-bit encoding with RSP (both = 4), so it always needs SIB as base.
func NeedsSIB(base int) bool { return base&7 == 4 }

// Fits8 reports whether v fits in a signed 8-bit immediate.
func Fits8(v int64) bool { return v >= -128 && v <= 127 }

// ── Little-endian helpers ─────────────────────────────────────────────────────

func Put16LE(b []byte, v uint16) {
	b[0] = byte(v); b[1] = byte(v >> 8)
}

func Put32LE(b []byte, v uint32) {
	b[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
}

func Put64LE(b []byte, v uint64) {
	b[0] = byte(v);    b[1] = byte(v >> 8);  b[2] = byte(v >> 16); b[3] = byte(v >> 24)
	b[4] = byte(v>>32); b[5] = byte(v >> 40); b[6] = byte(v >> 48); b[7] = byte(v >> 56)
}

func Append32LE(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func Append64LE(dst []byte, v uint64) []byte {
	return append(dst,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}