package macho

// ──────────────────────────────────────────────────────────────────────────────
// ARM64 relocation types  (<mach-o/arm64/reloc.h> ARM64_RELOC_*)
// ──────────────────────────────────────────────────────────────────────────────

// RelocTypeARM64 is a Mach-O ARM64 relocation type.
type RelocTypeARM64 uint8

const (
	// ARM64_RELOC_UNSIGNED — absolute 64-bit pointer; no addend.
	ARM64_RELOC_UNSIGNED RelocTypeARM64 = 0
	// ARM64_RELOC_SUBTRACTOR — must be immediately followed by an UNSIGNED;
	// computes (symbol_A - symbol_B).
	ARM64_RELOC_SUBTRACTOR RelocTypeARM64 = 1
	// ARM64_RELOC_BRANCH26 — 26-bit PC-relative displacement in BL/B instructions.
	ARM64_RELOC_BRANCH26 RelocTypeARM64 = 2
	// ARM64_RELOC_PAGE21 — high 21 bits of a PC-relative ADRP instruction.
	ARM64_RELOC_PAGE21 RelocTypeARM64 = 3
	// ARM64_RELOC_PAGEOFF12 — low 12-bit page offset for ADD / load / store.
	ARM64_RELOC_PAGEOFF12 RelocTypeARM64 = 4
	// ARM64_RELOC_GOT_LOAD_PAGE21 — ADRP addressing a GOT slot.
	ARM64_RELOC_GOT_LOAD_PAGE21 RelocTypeARM64 = 5
	// ARM64_RELOC_GOT_LOAD_PAGEOFF12 — 12-bit offset into a GOT slot (LDR).
	ARM64_RELOC_GOT_LOAD_PAGEOFF12 RelocTypeARM64 = 6
	// ARM64_RELOC_POINTER_TO_GOT — 32-bit PC-relative pointer into a GOT slot.
	ARM64_RELOC_POINTER_TO_GOT RelocTypeARM64 = 7
	// ARM64_RELOC_TLVP_LOAD_PAGE21 — ADRP to a thread-local variable pointer.
	ARM64_RELOC_TLVP_LOAD_PAGE21 RelocTypeARM64 = 8
	// ARM64_RELOC_TLVP_LOAD_PAGEOFF12 — 12-bit offset for a TLV LDR.
	ARM64_RELOC_TLVP_LOAD_PAGEOFF12 RelocTypeARM64 = 9
	// ARM64_RELOC_ADDEND — a preceding explicit addend for the next reloc entry.
	ARM64_RELOC_ADDEND RelocTypeARM64 = 10
	// ARM64_RELOC_AUTHENTICATED_POINTER — PAC-authenticated pointer (arm64e).
	ARM64_RELOC_AUTHENTICATED_POINTER RelocTypeARM64 = 11
)

// ──────────────────────────────────────────────────────────────────────────────
// AMD64 relocation types  (<mach-o/x86_64/reloc.h> X86_64_RELOC_*)
// ──────────────────────────────────────────────────────────────────────────────

// RelocTypeAMD64 is a Mach-O x86-64 relocation type.
type RelocTypeAMD64 uint8

const (
	// X86_64_RELOC_UNSIGNED — absolute 64-bit pointer.
	X86_64_RELOC_UNSIGNED RelocTypeAMD64 = 0
	// X86_64_RELOC_SIGNED — 32-bit PC-relative signed displacement.
	X86_64_RELOC_SIGNED RelocTypeAMD64 = 1
	// X86_64_RELOC_BRANCH — 32-bit PC-relative call/jmp displacement.
	X86_64_RELOC_BRANCH RelocTypeAMD64 = 2
	// X86_64_RELOC_GOT_LOAD — 32-bit PC-relative MOV through GOT.
	X86_64_RELOC_GOT_LOAD RelocTypeAMD64 = 3
	// X86_64_RELOC_GOT — 32-bit PC-relative reference to a GOT slot.
	X86_64_RELOC_GOT RelocTypeAMD64 = 4
	// X86_64_RELOC_SUBTRACTOR — must be followed by UNSIGNED.
	X86_64_RELOC_SUBTRACTOR RelocTypeAMD64 = 5
	// X86_64_RELOC_SIGNED_1 — like SIGNED but with -1 implicit addend.
	X86_64_RELOC_SIGNED_1 RelocTypeAMD64 = 6
	// X86_64_RELOC_SIGNED_2 — like SIGNED but with -2 implicit addend.
	X86_64_RELOC_SIGNED_2 RelocTypeAMD64 = 7
	// X86_64_RELOC_SIGNED_4 — like SIGNED but with -4 implicit addend.
	X86_64_RELOC_SIGNED_4 RelocTypeAMD64 = 8
	// X86_64_RELOC_TLV — 32-bit PC-relative reference to a thread-local variable.
	X86_64_RELOC_TLV RelocTypeAMD64 = 9
)