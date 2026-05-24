package pe

// COFF relocation types for AMD64 (x86-64) PE images.
//
// These constants are used in [Reloc].Type when building images directly
// without the linker. When a [PEImage] comes from linker/pe all relocations
// are already applied; these constants are not needed on that path.
//
// Reference: Microsoft PE/COFF Specification §4 "COFF Relocations".
const (
	// IMAGE_REL_AMD64_ABSOLUTE is a no-op relocation used for padding.
	IMAGE_REL_AMD64_ABSOLUTE = uint16(0x0000)

	// IMAGE_REL_AMD64_ADDR64 patches an 8-byte field with the 64-bit
	// virtual address of the target symbol.
	// Generates an IMAGE_REL_BASED_DIR64 base-relocation entry.
	IMAGE_REL_AMD64_ADDR64 = uint16(0x0001)

	// IMAGE_REL_AMD64_ADDR32 patches a 4-byte field with the 32-bit
	// virtual address of the target symbol (must fit in 32 bits).
	// Generates an IMAGE_REL_BASED_HIGHLOW base-relocation entry.
	IMAGE_REL_AMD64_ADDR32 = uint16(0x0002)

	// IMAGE_REL_AMD64_ADDR32NB patches a 4-byte field with the 32-bit
	// image-relative address (RVA) of the target symbol.
	// Does not generate a base-relocation entry.
	IMAGE_REL_AMD64_ADDR32NB = uint16(0x0003)

	// IMAGE_REL_AMD64_REL32 patches a 4-byte field with the signed
	// 32-bit offset from the byte immediately following the patch site
	// to the target symbol. Used for near CALL and JMP instructions.
	// Does not generate a base-relocation entry.
	IMAGE_REL_AMD64_REL32 = uint16(0x0004)

	// IMAGE_REL_AMD64_REL32_1 through IMAGE_REL_AMD64_REL32_5 are
	// variants of REL32 with 1–5 bytes of additional instruction bytes
	// following the 4-byte displacement field (used with MOV r/m, imm).
	IMAGE_REL_AMD64_REL32_1 = uint16(0x0005)
	IMAGE_REL_AMD64_REL32_2 = uint16(0x0006)
	IMAGE_REL_AMD64_REL32_3 = uint16(0x0007)
	IMAGE_REL_AMD64_REL32_4 = uint16(0x0008)
	IMAGE_REL_AMD64_REL32_5 = uint16(0x0009)

	// IMAGE_REL_AMD64_SECTION patches a 2-byte field with the
	// 1-based index of the section that contains the target symbol.
	IMAGE_REL_AMD64_SECTION = uint16(0x000A)

	// IMAGE_REL_AMD64_SECREL patches a 4-byte field with the
	// 32-bit offset of the target from the beginning of its section.
	IMAGE_REL_AMD64_SECREL = uint16(0x000B)

	// IMAGE_REL_AMD64_SECREL7 patches the low 7 bits of a byte field
	// with the 7-bit section-relative offset of the target.
	IMAGE_REL_AMD64_SECREL7 = uint16(0x000C)

	// IMAGE_REL_AMD64_TOKEN patches a 4-byte field with a 32-bit
	// CLR metadata token for the target.
	IMAGE_REL_AMD64_TOKEN = uint16(0x000D)

	// IMAGE_REL_AMD64_SREL32 is a span-dependent signed 32-bit relative
	// relocation; must be followed by an IMAGE_REL_AMD64_PAIR record.
	IMAGE_REL_AMD64_SREL32 = uint16(0x000E)

	// IMAGE_REL_AMD64_PAIR must immediately follow an SREL32 record
	// and encodes the pair end offset.
	IMAGE_REL_AMD64_PAIR = uint16(0x000F)

	// IMAGE_REL_AMD64_SSPAN32 is a span-dependent signed 32-bit
	// relocation applied to the instruction displacement.
	IMAGE_REL_AMD64_SSPAN32 = uint16(0x0010)
)