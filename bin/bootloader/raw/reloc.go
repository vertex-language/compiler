package raw

// RelocType identifies how a relocation slot is patched at emit time.
// All values are resolved statically — there is no runtime linker or loader
// involved. The patch is written in little-endian byte order.
type RelocType uint8

const (
	// R_ABS8 writes an 8-bit absolute address into the slot.
	// The value stored is: sym_addr + addend.
	// Typically used for zero-page addressing on architectures that support it.
	R_ABS8 RelocType = iota

	// R_ABS16 writes a 16-bit absolute address into the slot.
	// The value stored is: sym_addr + addend.
	// Common in x86 real mode where all addresses fit in 16 bits.
	R_ABS16

	// R_ABS32 writes a 32-bit absolute address into the slot.
	// The value stored is: sym_addr + addend.
	// Used in 32-bit protected mode and embedded targets.
	R_ABS32

	// R_REL8 writes an 8-bit PC-relative offset into the slot.
	// The value stored is: sym_addr - (patch_addr + 1) + addend.
	// Encodes short conditional and unconditional jumps (e.g. JMP SHORT, Jcc).
	R_REL8

	// R_REL16 writes a 16-bit PC-relative offset into the slot.
	// The value stored is: sym_addr - (patch_addr + 2) + addend.
	// Used for near jumps and calls in 16-bit real-mode code.
	R_REL16

	// R_REL32 writes a 32-bit PC-relative offset into the slot.
	// The value stored is: sym_addr - (patch_addr + 4) + addend.
	// Used for near calls and jumps in 32-bit protected-mode code.
	R_REL32

	// R_SEG16 writes the 16-bit real-mode segment value of a symbol into the slot.
	// The value stored is: (sym_addr + addend) >> 4.
	// Used to load segment registers (CS, DS, ES, …) for far calls, far jumps,
	// and explicit segment-register initialisation in real-mode code.
	R_SEG16
)

// relocWidth returns the number of bytes written by relocation type t.
func relocWidth(t RelocType) uint32 {
	switch t {
	case R_ABS8, R_REL8:
		return 1
	case R_ABS16, R_REL16, R_SEG16:
		return 2
	case R_ABS32, R_REL32:
		return 4
	default:
		return 0
	}
}

// Reloc describes a single relocation to be applied during Emit.
//
// The relocation site is the byte range [Offset, Offset+width) within the
// named Section. The patch value is computed from the resolved address of
// Symbol according to Type and Addend.
type Reloc struct {
	// Section is the name of the section containing the relocation site.
	// Empty means the first section.
	Section string

	// Offset is the byte offset of the relocation site from the start of
	// the section.
	Offset uint32

	// Symbol is the name of the target symbol, which must have been
	// registered with Builder.AddSymbol before Emit is called.
	Symbol string

	// Type controls how the patch value is computed and how wide the write is.
	Type RelocType

	// Addend is a signed value added to the computed address before it is
	// written. Equivalent to the addend field in ELF RELA entries.
	Addend int32
}