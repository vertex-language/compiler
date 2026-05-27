package pe

// COFF relocation type indicators for ARM64 (AArch64) object files.
// Used in [COFFReloc].Type when building .obj files targeting ARM64.
//
// Reference: Microsoft PE/COFF Specification §COFF Relocations – ARM64.
const (
	// IMAGE_REL_ARM64_ABSOLUTE is a no-op used for padding.
	IMAGE_REL_ARM64_ABSOLUTE = uint16(0x0000)

	// IMAGE_REL_ARM64_ADDR32 patches a 4-byte field with the 32-bit VA of the target.
	IMAGE_REL_ARM64_ADDR32 = uint16(0x0001)

	// IMAGE_REL_ARM64_ADDR32NB patches a 4-byte field with the 32-bit RVA of the target.
	IMAGE_REL_ARM64_ADDR32NB = uint16(0x0002)

	// IMAGE_REL_ARM64_BRANCH26 patches the 26-bit displacement of a B or BL instruction.
	IMAGE_REL_ARM64_BRANCH26 = uint16(0x0003)

	// IMAGE_REL_ARM64_PAGEBASE_REL21 patches the 21-bit immediate of an ADRP instruction
	// with the page-relative offset: Page(target) − Page(ADRP_VA).
	IMAGE_REL_ARM64_PAGEBASE_REL21 = uint16(0x0004)

	// IMAGE_REL_ARM64_REL21 patches the 21-bit immediate of an ADR instruction
	// with the PC-relative offset to the target.
	IMAGE_REL_ARM64_REL21 = uint16(0x0005)

	// IMAGE_REL_ARM64_PAGEOFFSET_12A patches the 12-bit immediate of an ADD instruction
	// with the page offset of the target (for ADD after ADRP).
	IMAGE_REL_ARM64_PAGEOFFSET_12A = uint16(0x0006)

	// IMAGE_REL_ARM64_PAGEOFFSET_12L patches the 12-bit immediate of an LDR/STR instruction
	// with the scaled page offset of the target (for LDR/STR after ADRP).
	IMAGE_REL_ARM64_PAGEOFFSET_12L = uint16(0x0007)

	// IMAGE_REL_ARM64_SECREL patches a 4-byte field with the 32-bit section-relative
	// offset of the target.
	IMAGE_REL_ARM64_SECREL = uint16(0x0008)

	// IMAGE_REL_ARM64_SECREL_LOW12A patches the 12-bit immediate of an ADD instruction
	// with the low 12 bits of the section-relative offset (bits [11:0]).
	IMAGE_REL_ARM64_SECREL_LOW12A = uint16(0x0009)

	// IMAGE_REL_ARM64_SECREL_HIGH12A patches the 12-bit immediate of an ADD instruction
	// with bits [23:12] of the section-relative offset.
	IMAGE_REL_ARM64_SECREL_HIGH12A = uint16(0x000A)

	// IMAGE_REL_ARM64_SECREL_LOW12L patches the 12-bit immediate of an LDR/STR
	// with the low 12 bits of the section-relative offset (scaled by access size).
	IMAGE_REL_ARM64_SECREL_LOW12L = uint16(0x000B)

	// IMAGE_REL_ARM64_TOKEN patches a 4-byte field with a 32-bit CLR metadata token.
	IMAGE_REL_ARM64_TOKEN = uint16(0x000C)

	// IMAGE_REL_ARM64_SECTION patches a 2-byte field with the 1-based section index
	// containing the target symbol. Used in debug information.
	IMAGE_REL_ARM64_SECTION = uint16(0x000D)

	// IMAGE_REL_ARM64_ADDR64 patches an 8-byte field with the 64-bit VA of the target.
	IMAGE_REL_ARM64_ADDR64 = uint16(0x000E)

	// IMAGE_REL_ARM64_BRANCH19 patches the 19-bit displacement of a conditional branch
	// instruction (CBZ, CBNZ, B.cond).
	IMAGE_REL_ARM64_BRANCH19 = uint16(0x000F)

	// IMAGE_REL_ARM64_BRANCH14 patches the 14-bit displacement of a TBZ/TBNZ instruction.
	IMAGE_REL_ARM64_BRANCH14 = uint16(0x0010)

	// IMAGE_REL_ARM64_REL32 patches a 4-byte field with the signed 32-bit PC-relative
	// offset to the target symbol.
	IMAGE_REL_ARM64_REL32 = uint16(0x0011)
)