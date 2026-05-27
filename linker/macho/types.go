package macho

// ──────────────────────────────────────────────────────────────────────────────
// Architecture constants (cputype)
// ──────────────────────────────────────────────────────────────────────────────

const (
	ArchAMD64 uint32 = 0x01000007 // CPU_TYPE_X86_64
	ArchARM64 uint32 = 0x0100000C // CPU_TYPE_ARM64
)

// ──────────────────────────────────────────────────────────────────────────────
// Output type
// ──────────────────────────────────────────────────────────────────────────────

// OutputType selects the Mach-O file type produced by the linker.
type OutputType int

const (
	OutputExec   OutputType = iota // MH_EXECUTE  (0x2)
	OutputDylib                    // MH_DYLIB    (0x6)
	OutputBundle                   // MH_BUNDLE   (0x8)
)

// ──────────────────────────────────────────────────────────────────────────────
// Raw section
// ──────────────────────────────────────────────────────────────────────────────

// RawSection is one section_64 entry from an MH_OBJECT input file.
type RawSection struct {
	SegName  string // e.g. "__TEXT"
	SectName string // e.g. "__text"
	Addr     uint64 // in-object VA (usually 0 for MH_OBJECT)
	Data     []byte // nil for S_ZEROFILL-family sections
	Size     uint64 // authoritative size; len(Data) for non-zerofill
	Align    uint32 // alignment as byte count (power of two); 1 if 0
	Flags    uint32 // section type (low byte) + attributes (high 3 bytes)
	Reserved1 uint32
	Reserved2 uint32
	Index    int    // 1-based section number within the source object
}

// IsZerofill returns true when the section carries no file bytes.
func (s *RawSection) IsZerofill() bool {
	t := s.Flags & 0xff
	return t == 0x01 || // S_ZEROFILL
		t == 0x0c || // S_GB_ZEROFILL
		t == 0x12 // S_THREAD_LOCAL_ZEROFILL
}

// ──────────────────────────────────────────────────────────────────────────────
// Raw symbol
// ──────────────────────────────────────────────────────────────────────────────

// RawSymbol is one nlist_64 entry decoded from an MH_OBJECT file.
type RawSymbol struct {
	Name  string
	Type  uint8  // raw n_type byte
	Sect  uint8  // 1-based section number (0 = no section)
	Desc  uint16 // n_desc
	Value uint64 // n_value

	// Decoded convenience fields.
	IsGlobal  bool // N_EXT set
	IsPrivExt bool // N_PEXT set
	IsAbs     bool // N_TYPE == N_ABS
	IsUndef   bool // N_TYPE == N_UNDF and Value == 0
	IsCommon  bool // N_TYPE == N_UNDF and Value > 0 (tentative definition)
	IsWeak    bool // N_WEAK_DEF or N_WEAK_REF in n_desc
	IsDebug   bool // N_STAB bits set (debug symbol, skipped by linker)

	// Section decoded (valid when !IsAbs && !IsUndef && !IsCommon).
	SectionName string // e.g. "__text"
	SegmentName string // e.g. "__TEXT"
}

// ──────────────────────────────────────────────────────────────────────────────
// Raw relocation
// ──────────────────────────────────────────────────────────────────────────────

// RawReloc is one relocation_info record from an MH_OBJECT file.
type RawReloc struct {
	SectionIdx int    // 0-based index into ObjectFile.Sections (the patched section)
	Offset     uint32 // r_address: byte offset within that section
	SymIdx     uint32 // symbol-table index (when Extern == true)
	SectNum    uint32 // 1-based section number (when Extern == false)
	PCRel      bool
	Length     uint8 // 0=1B, 1=2B, 2=4B, 3=8B
	Extern     bool
	Type       uint8
}

// ──────────────────────────────────────────────────────────────────────────────
// ObjectFile
// ──────────────────────────────────────────────────────────────────────────────

// ObjectFile is a parsed MH_OBJECT Mach-O file.
type ObjectFile struct {
	Path     string
	Arch     uint32       // cputype
	MHFlags  uint32       // mach_header flags
	Sections []*RawSection // 1-based; index 0 is always nil
	Symbols  []*RawSymbol  // flat nlist_64 list (includes locals and debug)
	Relocs   []*RawReloc
}

// ──────────────────────────────────────────────────────────────────────────────
// Archive
// ──────────────────────────────────────────────────────────────────────────────

// Archive is a parsed static library (.a file).
type Archive struct {
	Path     string
	Members  []*ArchiveMember
	symIndex map[string]*ArchiveMember // pre-built from ar symbol table
}

// MemberForSymbol returns the archive member that provides the given symbol,
// or nil if not found.  Consults the pre-built symbol index first, then falls
// back to exhaustive scanning.
func (a *Archive) MemberForSymbol(sym string) *ArchiveMember {
	if m, ok := a.symIndex[sym]; ok {
		return m
	}
	for _, m := range a.Members {
		obj, err := m.Object()
		if err != nil {
			continue
		}
		for _, s := range obj.Symbols {
			if s.Name == sym && s.IsGlobal && !s.IsUndef && !s.IsDebug {
				if a.symIndex == nil {
					a.symIndex = make(map[string]*ArchiveMember)
				}
				a.symIndex[sym] = m
				return m
			}
		}
	}
	return nil
}

// ArchiveMember is one .o file inside an Archive.
type ArchiveMember struct {
	Name string // member filename
	data []byte // raw bytes of the .o file
	obj  *ObjectFile
}

// Object parses (and caches) the member's ObjectFile.
func (m *ArchiveMember) Object() (*ObjectFile, error) {
	if m.obj != nil {
		return m.obj, nil
	}
	obj, err := ParseObject(m.data)
	if err != nil {
		return nil, err
	}
	obj.Path = m.Name
	m.obj = obj
	return obj, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// DylibFile
// ──────────────────────────────────────────────────────────────────────────────

// DylibFile is a parsed MH_DYLIB (or MH_BUNDLE used as dylib) file.
type DylibFile struct {
	Path    string
	Soname  string            // LC_ID_DYLIB install name (or path basename)
	Needed  []string          // LC_LOAD_DYLIB dependencies
	Rpaths  []string          // LC_RPATH entries
	symbols map[string]*DylibSymbol
}

// Symbol looks up an exported symbol by name.
func (d *DylibFile) Symbol(name string) (*DylibSymbol, bool) {
	s, ok := d.symbols[name]
	return s, ok
}

// DylibSymbol is a symbol exported by a DylibFile.
type DylibSymbol struct {
	Name       string
	VMOffset   uint64 // offset from dylib image base
	ExportFlags uint64
	IsWeak     bool
}