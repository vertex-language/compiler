package object

// SymbolKind indicates whether a symbol is defined in this object or must be
// supplied by the linker.
type SymbolKind int

const (
	SymDefined   SymbolKind = iota // locally compiled function or data
	SymUndefined                   // import; linker must provide the address
	SymTLS                         // STT_TLS thread-local variable
)

// SymSection identifies which output section a SymDefined symbol lives in.
type SymSection int

const (
	SymSecText   SymSection = iota // .text
	SymSecROData                   // .rodata
	SymSecData                     // .data (initialized RW)
	SymSecBSS                      // .bss (zero-init RW)
	SymSecTData                    // .tdata (initialized TLS)
	SymSecTBSS                     // .tbss (zero-init TLS)
)

// Symbol is one entry in an object's symbol table.
type Symbol struct {
	Name    string
	Kind    SymbolKind
	Section SymSection // meaningful for SymDefined; zero for others
	// Offset is the byte position of the symbol within its section.
	// For SymTLS in SymSecTBSS: offset within this object's .tbss contribution
	// (the linker adjusts to a global TLS-template offset after all ingestion).
	Offset int
}