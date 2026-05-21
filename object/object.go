// Package object defines the internal format that the compiler produces and
// the linker consumes.
package object

// WasmObj is the output of the compiler and the input to the linker.
// It carries compiled machine code together with all data sections and enough
// metadata for the linker to resolve external references and produce a native
// binary.
type WasmObj struct {
	// .text — raw machine code for all compiled functions.
	Code []byte

	// .rodata — read-only data (string literals, jump tables, …).
	ROData []byte

	// .data — initialized read-write data.
	Data []byte

	// BSS is the size in bytes of the zero-initialized read-write (.bss) region.
	BSS uint64

	// TLSData — initialized thread-local data (.tdata).
	TLSData []byte

	// TLSBSSSize is the size in bytes of the zero-initialized thread-local
	// (.tbss) region.
	TLSBSSSize uint64

	// Symbols is the combined symbol table for all sections above.
	Symbols []Symbol

	// Relocs is the relocation table for .text, .rodata, and .data.
	// The Reloc.Section field identifies which section each site is in.
	Relocs []Reloc
}