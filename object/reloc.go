package object

// RelocSection identifies which output section a relocation site is in.
type RelocSection int

const (
	RelocSecText   RelocSection = iota // site is in .text
	RelocSecROData                     // site is in .rodata
	RelocSecData                       // site is in .data
)

// RelocKind describes how a relocation site should be patched.
type RelocKind int

const (
	// RelocRel32: 4-byte PC-relative signed offset.
	// Formula: S + A − (P+4), where A is the ELF r_addend.
	// Covers R_X86_64_PC32 and R_X86_64_PLT32.
	// A is almost always −4, but glibc occasionally emits −5 (sym−1
	// references used in internal trampolines).
	RelocRel32 RelocKind = iota

	// RelocAbs64: 8-byte absolute virtual address.
	// Formula: S.  Covers R_X86_64_64.
	RelocAbs64

	// RelocAbs32S: 4-byte absolute VA, sign-extended.
	// Formula: S.  Covers R_X86_64_32S.
	RelocAbs32S

	// RelocGOTPCRel32: 4-byte PC-relative offset to a GOT slot holding S.
	// Formula: G − (P+4) where G = GOT slot VA.
	// Covers R_X86_64_GOTPCREL, R_X86_64_GOTPCRELX, R_X86_64_REX_GOTPCRELX.
	RelocGOTPCRel32

	// RelocTLSGOTPCRel32: 4-byte PC-relative offset to a TLS IE GOT slot.
	// The slot holds tpoff(sym) = sym_tls_offset − alignUp(totalTLSSize, 16).
	// Formula: G_tls − (P+4).  Covers R_X86_64_GOTTPOFF.
	RelocTLSGOTPCRel32

	// RelocTPOFF32: 4-byte TP-relative signed offset (local exec TLS).
	// Written directly into the instruction; no GOT needed.
	// Formula: tpoff(sym).  Covers R_X86_64_TPOFF32.
	RelocTPOFF32
)

// Reloc is one relocation entry: a patch site in a section that the linker
// must resolve once symbol addresses are known.
type Reloc struct {
	Section RelocSection // which output section the patch site is in
	Offset  int          // byte offset of the field within that section
	Symbol  string       // target symbol name
	Kind    RelocKind    // how to compute and write the patch value
	// Addend is the ELF r_addend value carried by RELA relocations.
	// For RelocRel32 the patcher computes S + Addend − P; all other kinds
	// that use a fixed formula ignore this field (it is always 0 for them).
	Addend int64
}