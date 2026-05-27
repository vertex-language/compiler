package pe

// ─── Raw input types ─────────────────────────────────────────────────────────

// ObjectFile is a parsed COFF relocatable object file (.obj).
type ObjectFile struct {
	Path    string
	Machine uint16

	Sections []*RawSection
	Symbols  []*RawSymbol // index 0 is always the null symbol (for 1-based section refs)

	// Parsed from the .drectve section.
	Exports     []DrectveExport
	DefaultLibs []string
	EntryHint   string
}

// RawSection is one section from a COFF object file.
type RawSection struct {
	Name  string
	Chars uint32
	Data  []byte   // nil for IMAGE_SCN_CNT_UNINITIALIZED_DATA (BSS)
	VirtualSize uint32 // from section header (physical address field in .obj)

	Relocs []*RawReloc

	// 0-based position in the section header table of this object file.
	Index int

	// COMDAT fields. Valid when IMAGE_SCN_LNK_COMDAT is set in Chars.
	IsComdat    bool
	ComdatSel   uint8 // IMAGE_COMDAT_SELECT_* from aux section-def record
	ComdatAssoc int   // 0-based index of the associate section; -1 if none
}

// RawReloc is one COFF relocation record.
type RawReloc struct {
	Offset   uint32 // byte offset within section Data
	SymIndex uint32 // 0-based index into the containing ObjectFile.Symbols
	Type     uint16
}

// RawSymbol is one entry in the COFF symbol table.
type RawSymbol struct {
	Name          string
	Value         uint32
	SectionNumber int16 // 0=undefined, −1=absolute, −2=debug, >0=1-based section
	Type          uint16
	StorageClass  uint8

	// For IMAGE_SYM_CLASS_WEAK_EXTERNAL symbols (from aux record):
	WeakDefaultIdx int    // symbol-table index of default resolution symbol
	WeakChars      uint32 // IMAGE_WEAK_EXTERN_SEARCH_* characteristics

	// For section symbols (StorageClass==IMAGE_SYM_CLASS_STATIC) with COMDAT:
	ComdatSectionIdx int    // 0-based section index this aux record describes
	ComdatSel        uint8  // IMAGE_COMDAT_SELECT_*
	ComdatLen        uint32 // length of the COMDAT section data
	ComdatAssocNum   uint16 // 1-based section number of COMDAT associate section
	HasSectionAux    bool
}

// DrectveExport is a parsed -export: directive from the .drectve section.
type DrectveExport struct {
	InternalName string // symbol name inside the image
	ExportName   string // name visible to importers (may equal InternalName)
	IsData       bool   // ,data suffix: symbol is a data variable
}

// ─── Archive types ────────────────────────────────────────────────────────────

// Archive is a parsed COFF/GNU ar archive (.lib or .a).
type Archive struct {
	Path    string
	Members []*ArchiveMember
	symIdx  map[string]int // symbol name → member index (from linker member)
}

// MemberForSymbol returns the member that defines sym, or nil.
func (a *Archive) MemberForSymbol(sym string) *ArchiveMember {
	if i, ok := a.symIdx[sym]; ok && i < len(a.Members) {
		return a.Members[i]
	}
	// Fallback: linear scan.
	for _, m := range a.Members {
		if m.imp != nil {
			if m.imp.SymName == sym || "__imp_"+m.imp.SymName == sym {
				return m
			}
			continue
		}
		o, err := m.Object()
		if err != nil {
			continue
		}
		for _, sym2 := range o.Symbols {
			if sym2.Name == sym &&
				sym2.StorageClass == 2 && // IMAGE_SYM_CLASS_EXTERNAL
				sym2.SectionNumber > 0 {
				return m
			}
		}
	}
	return nil
}

// ArchiveMember is one member in an archive.
type ArchiveMember struct {
	Name string
	data []byte       // raw bytes (lazily parsed)
	obj  *ObjectFile  // non-nil once parsed
	imp  *ShortImport // non-nil if this is a short import stub
}

// Object parses and returns the ObjectFile for this member.
// Returns an error if the member is a short import stub.
func (m *ArchiveMember) Object() (*ObjectFile, error) {
	if m.imp != nil {
		return nil, nil // short import stubs have no ObjectFile
	}
	if m.obj != nil {
		return m.obj, nil
	}
	var err error
	m.obj, err = ParseObject(m.data)
	if err != nil {
		return nil, err
	}
	return m.obj, nil
}

// Import returns the ShortImport for this member, or nil if it is a regular object.
func (m *ArchiveMember) Import() *ShortImport { return m.imp }

// ShortImport is a parsed Windows short-format import stub
// (Sig1=0x0000, Sig2=0xFFFF — the modern .lib stub format).
type ShortImport struct {
	Machine    uint16
	DLL        string
	SymName    string // exported function/data name; empty = ordinal-only
	Ordinal    uint16
	ImportType uint16 // IMPORT_CODE=0, IMPORT_DATA=1, IMPORT_CONST=2
	NameType   uint16 // IMPORT_ORDINAL=0, IMPORT_NAME=1, …
}

// ─── Layout / merge types ─────────────────────────────────────────────────────

// Layout is the result of MergeSections: a list of output sections,
// each with their contributing input pieces.
type Layout struct {
	Sections []*MergedSection
}

// SectionByName returns the first merged section with the given name, or nil.
func (l *Layout) SectionByName(name string) *MergedSection {
	for _, s := range l.Sections {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// MergedSection is one output section, assembled from one or more input
// section contributions (Pieces). VAddr and FileOffset are filled by
// AssignLayout.
type MergedSection struct {
	Name  string
	Chars uint32
	// Pieces are the ordered input-section contributions.
	Pieces []Piece
	// Data holds the concatenated (and alignment-padded) section bytes.
	// Nil for purely BSS sections (IMAGE_SCN_CNT_UNINITIALIZED_DATA).
	Data []byte
	// VirtualSize is the in-memory byte count (>= len(Data)).
	VirtualSize uint32
	// VAddr is the virtual address assigned by AssignLayout.
	VAddr uint32
}

// Piece is one input section's contribution to a MergedSection.
type Piece struct {
	Obj    *ObjectFile
	Sec    *RawSection
	Offset uint32 // byte offset within the merged section
}

// ─── Import collection types ──────────────────────────────────────────────────

// CollectedImport groups all symbols imported from one DLL.
type CollectedImport struct {
	DLL     string
	Symbols []CollectedImportSym
}

// CollectedImportSym is one symbol imported from a DLL.
type CollectedImportSym struct {
	Name       string // export name; empty = ordinal-only
	Ordinal    uint16
	Hint       uint16
	ImportType uint16 // IMPORT_CODE / IMPORT_DATA
	// IATSlotRVA is filled by resolveImportSymbols after VA assignment.
	IATSlotRVA uint32
}