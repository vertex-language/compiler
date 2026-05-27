package pe

// COFF relocation type indicators for AMD64 (x86-64) object files.
// These are used in [COFFReloc].Type when building .obj files.
// In final PE32+ images all relocations are already applied by the linker;
// only IMAGE_REL_BASED_* values (in base-relocation blocks) remain at that stage.
//
// Reference: Microsoft PE/COFF Specification §COFF Relocations – x64.
const (
	// IMAGE_REL_AMD64_ABSOLUTE is a no-op used for padding.
	IMAGE_REL_AMD64_ABSOLUTE = uint16(0x0000)

	// IMAGE_REL_AMD64_ADDR64 patches an 8-byte field with the 64-bit VA of the target.
	// Produces an IMAGE_REL_BASED_DIR64 base-reloc entry.
	IMAGE_REL_AMD64_ADDR64 = uint16(0x0001)

	// IMAGE_REL_AMD64_ADDR32 patches a 4-byte field with the 32-bit VA of the target
	// (target VA must fit in 32 bits).
	// Produces an IMAGE_REL_BASED_HIGHLOW base-reloc entry.
	IMAGE_REL_AMD64_ADDR32 = uint16(0x0002)

	// IMAGE_REL_AMD64_ADDR32NB patches a 4-byte field with the 32-bit RVA of the target.
	// Does not produce a base-reloc entry (image-relative, not affected by ASLR slide).
	IMAGE_REL_AMD64_ADDR32NB = uint16(0x0003)

	// IMAGE_REL_AMD64_REL32 patches a 4-byte field with the signed 32-bit PC-relative
	// offset from the byte immediately after the patch site to the target symbol.
	// Used for near CALL and JMP instructions.
	IMAGE_REL_AMD64_REL32 = uint16(0x0004)

	// IMAGE_REL_AMD64_REL32_1 through IMAGE_REL_AMD64_REL32_5 are REL32 variants
	// with 1–5 additional bytes of instruction payload following the displacement field.
	IMAGE_REL_AMD64_REL32_1 = uint16(0x0005)
	IMAGE_REL_AMD64_REL32_2 = uint16(0x0006)
	IMAGE_REL_AMD64_REL32_3 = uint16(0x0007)
	IMAGE_REL_AMD64_REL32_4 = uint16(0x0008)
	IMAGE_REL_AMD64_REL32_5 = uint16(0x0009)

	// IMAGE_REL_AMD64_SECTION patches a 2-byte field with the 1-based section index
	// containing the target symbol. Used in debug information.
	IMAGE_REL_AMD64_SECTION = uint16(0x000A)

	// IMAGE_REL_AMD64_SECREL patches a 4-byte field with the 32-bit offset of the
	// target from the beginning of its section.
	IMAGE_REL_AMD64_SECREL = uint16(0x000B)

	// IMAGE_REL_AMD64_SECREL7 patches the low 7 bits of a byte with the 7-bit
	// section-relative offset of the target.
	IMAGE_REL_AMD64_SECREL7 = uint16(0x000C)

	// IMAGE_REL_AMD64_TOKEN patches a 4-byte field with a 32-bit CLR metadata token.
	IMAGE_REL_AMD64_TOKEN = uint16(0x000D)

	// IMAGE_REL_AMD64_SREL32 is a span-dependent signed 32-bit relocation;
	// must be followed by IMAGE_REL_AMD64_PAIR.
	IMAGE_REL_AMD64_SREL32 = uint16(0x000E)

	// IMAGE_REL_AMD64_PAIR must immediately follow an SREL32 record.
	IMAGE_REL_AMD64_PAIR = uint16(0x000F)

	// IMAGE_REL_AMD64_SSPAN32 is a span-dependent signed 32-bit relocation applied
	// to an instruction displacement.
	IMAGE_REL_AMD64_SSPAN32 = uint16(0x0010)
)