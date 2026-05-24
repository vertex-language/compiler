package raw

// Section is a contiguous blob of code or data to be placed in the binary.
// Sections are emitted in the order they are added, with zero-byte alignment
// padding inserted between them as needed.
//
// Name is optional. When set it can be referenced by Symbol.Section and
// Reloc.Section to target this section by name. If Name is empty the section
// can still be targeted by leaving those fields empty, which resolves to the
// first section.
type Section struct {
	Name  string
	Data  []byte
	Align uint32 // 0 and 1 both mean byte-aligned (no padding)
}

// Symbol defines a named address within a section.
// The resolved absolute address is:
//
//	origin + section_file_offset + Offset
//
// Symbols are used as targets for Reloc entries. They carry no meaning in
// the output binary itself — there is no symbol table in a flat binary.
type Symbol struct {
	Name    string
	Section string // section Name to anchor to; empty means the first section
	Offset  uint32 // byte offset from the start of the section
}