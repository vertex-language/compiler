package elf

// R_X86_64_* relocation type constants for AMD64 (EM_X86_64).
//
// Computation key:
//   A  = the explicit addend (r_addend)
//   B  = base address at which the shared object is loaded
//   G  = offset into the GOT where the symbol's address resides
//   GOT = address of the Global Offset Table (.got)
//   L  = PLT address for the symbol
//   P  = address of the storage unit being relocated (r_offset)
//   S  = value of the symbol referenced by the relocation
//   Z  = size of the symbol
//
// Source: System V AMD64 ABI, §4.4 "Relocation"
// https://refspecs.linuxbase.org/elf/x86_64-abi-0.99.pdf
const (
	// Static relocations.
	R_X86_64_NONE      = 0  // none
	R_X86_64_64        = 1  // S + A          (64-bit absolute)
	R_X86_64_PC32      = 2  // S + A - P      (32-bit PC-relative)
	R_X86_64_GOT32     = 3  // G + A          (32-bit GOT offset)
	R_X86_64_PLT32     = 4  // L + A - P      (32-bit PLT-relative)
	R_X86_64_COPY      = 5  // copy at runtime
	R_X86_64_GLOB_DAT  = 6  // S              (GOT entry ← symbol address)
	R_X86_64_JUMP_SLOT = 7  // S              (PLT GOT slot)
	R_X86_64_RELATIVE  = 8  // B + A          (relative address)
	R_X86_64_GOTPCREL  = 9  // G + GOT + A - P
	R_X86_64_32        = 10 // S + A          (32-bit zero-extend)
	R_X86_64_32S       = 11 // S + A          (32-bit sign-extend)
	R_X86_64_16        = 12 // S + A          (16-bit)
	R_X86_64_PC16      = 13 // S + A - P      (16-bit PC-relative)
	R_X86_64_8         = 14 // S + A          (8-bit)
	R_X86_64_PC8       = 15 // S + A - P      (8-bit PC-relative)

	// Thread-local storage relocations.
	R_X86_64_DTPMOD64 = 16 // ID of module containing symbol
	R_X86_64_DTPOFF64 = 17 // offset in TLS block
	R_X86_64_TPOFF64  = 18 // offset in initial TLS block
	R_X86_64_TLSGD    = 19 // PC-relative offset to GD GOT entry
	R_X86_64_TLSLD    = 20 // PC-relative offset to LD GOT entry
	R_X86_64_DTPOFF32 = 21 // offset in TLS block (32-bit)
	R_X86_64_GOTTPOFF = 22 // PC-relative offset to IE GOT entry
	R_X86_64_TPOFF32  = 23 // offset in initial TLS block (32-bit)

	// Additional static relocations.
	R_X86_64_PC64       = 24 // S + A - P          (64-bit PC-relative)
	R_X86_64_GOTOFF64   = 25 // S + A - GOT
	R_X86_64_GOTPC32    = 26 // GOT + A - P        (32-bit)
	R_X86_64_GOT64      = 27 // G + A              (64-bit GOT offset)
	R_X86_64_GOTPCREL64 = 28 // G + GOT + A - P    (64-bit)
	R_X86_64_GOTPC64    = 29 // GOT + A - P        (64-bit)
	R_X86_64_GOTPLT64   = 30 // G + A              (like GOT64 but PLT also valid)
	R_X86_64_PLTOFF64   = 31 // L + A - GOT
	R_X86_64_SIZE32     = 32 // Z + A              (symbol size, 32-bit)
	R_X86_64_SIZE64     = 33 // Z + A              (symbol size, 64-bit)

	// TLS descriptor relocations (TLSDESC ABI).
	R_X86_64_GOTPC32_TLSDESC = 34 // GOT offset to TLS descriptor
	R_X86_64_TLSDESC_CALL    = 35 // relax-able call through TLS descriptor
	R_X86_64_TLSDESC         = 36 // TLS descriptor

	// Indirect function (IFUNC) relocations.
	R_X86_64_IRELATIVE  = 37 // B + A → ifunc resolver result
	R_X86_64_RELATIVE64 = 38 // B + A (64-bit, used in ILP32)

	// Optimised GOTPCREL forms (relaxable by the linker).
	R_X86_64_GOTPCRELX     = 41 // like GOTPCREL but relaxable
	R_X86_64_REX_GOTPCRELX = 42 // like GOTPCREL with REX prefix, relaxable
)