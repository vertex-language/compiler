// Package elf constructs and serializes ELF64 binaries.
package elf

// ── ELF identification (e_ident) ─────────────────────────────────────────────

// Magic bytes (e_ident[EI_MAG0..EI_MAG3]).
const (
	ELFMAG0 = 0x7F
	ELFMAG1 = 'E'
	ELFMAG2 = 'L'
	ELFMAG3 = 'F'
)

// e_ident byte indices.
const (
	EI_MAG0       = 0
	EI_MAG1       = 1
	EI_MAG2       = 2
	EI_MAG3       = 3
	EI_CLASS      = 4
	EI_DATA       = 5
	EI_VERSION    = 6
	EI_OSABI      = 7
	EI_ABIVERSION = 8
	EI_PAD        = 9
	EI_NIDENT     = 16
)

// EI_CLASS values.
const (
	ELFCLASSNONE = 0
	ELFCLASS32   = 1
	ELFCLASS64   = 2
)

// EI_DATA values.
const (
	ELFDATANONE = 0
	ELFDATA2LSB = 1 // little-endian
	ELFDATA2MSB = 2 // big-endian
)

// EI_VERSION / e_version values.
const (
	EV_NONE    = 0
	EV_CURRENT = 1
)

// EI_OSABI values.
const (
	ELFOSABI_NONE       = 0  // System V / none
	ELFOSABI_SYSV       = 0
	ELFOSABI_NETBSD     = 2
	ELFOSABI_LINUX      = 3
	ELFOSABI_FREEBSD    = 9
	ELFOSABI_OPENBSD    = 12
	ELFOSABI_STANDALONE = 255
)

// ── Object file type (e_type) ────────────────────────────────────────────────

const (
	ET_NONE = 0
	ET_REL  = 1
	ET_EXEC = 2
	ET_DYN  = 3
	ET_CORE = 4
)

// ── Machine architecture (e_machine) ─────────────────────────────────────────

const (
	EM_NONE    = 0x00
	EM_386     = 0x03
	EM_PPC64   = 0x15
	EM_ARM     = 0x28
	EM_X86_64  = 0x3E
	EM_AARCH64 = 0xB7
	EM_RISCV   = 0xF3
)

// ── Section header types (sh_type) ───────────────────────────────────────────

const (
	SHT_NULL          = 0
	SHT_PROGBITS      = 1
	SHT_SYMTAB        = 2
	SHT_STRTAB        = 3
	SHT_RELA          = 4
	SHT_HASH          = 5
	SHT_DYNAMIC       = 6
	SHT_NOTE          = 7
	SHT_NOBITS        = 8
	SHT_REL           = 9
	SHT_SHLIB         = 10
	SHT_DYNSYM        = 11
	SHT_INIT_ARRAY    = 14
	SHT_FINI_ARRAY    = 15
	SHT_PREINIT_ARRAY = 16
	SHT_GROUP         = 17
	SHT_SYMTAB_SHNDX  = 18
	SHT_GNU_HASH      = 0x6FFFFFF6
	SHT_GNU_VERNEED   = 0x6FFFFFFE
	SHT_GNU_VERSYM    = 0x6FFFFFFF
)

// ── Section header flags (sh_flags) ──────────────────────────────────────────

const (
	SHF_WRITE            uint64 = 0x001
	SHF_ALLOC            uint64 = 0x002
	SHF_EXECINSTR        uint64 = 0x004
	SHF_MERGE            uint64 = 0x010
	SHF_STRINGS          uint64 = 0x020
	SHF_INFO_LINK        uint64 = 0x040
	SHF_LINK_ORDER       uint64 = 0x080
	SHF_OS_NONCONFORMING uint64 = 0x100
	SHF_GROUP            uint64 = 0x200
	SHF_TLS              uint64 = 0x400
	SHF_COMPRESSED       uint64 = 0x800
)

// ── Special section indices ───────────────────────────────────────────────────

const (
	SHN_UNDEF     = 0
	SHN_ABS       = 0xFFF1
	SHN_COMMON    = 0xFFF2
	SHN_XINDEX    = 0xFFFF
	SHN_LORESERVE = 0xFF00
	SHN_HIRESERVE = 0xFFFF
	SHN_LOPROC    = 0xFF00
	SHN_HIPROC    = 0xFF1F
	SHN_LOOS      = 0xFF20
	SHN_HIOS      = 0xFF3F
)

// ── Program header types (p_type) ────────────────────────────────────────────

const (
	PT_NULL         = 0
	PT_LOAD         = 1
	PT_DYNAMIC      = 2
	PT_INTERP       = 3
	PT_NOTE         = 4
	PT_SHLIB        = 5
	PT_PHDR         = 6
	PT_TLS          = 7
	PT_GNU_EH_FRAME = 0x6474E550
	PT_GNU_STACK    = 0x6474E551
	PT_GNU_RELRO    = 0x6474E552
)

// ── Program header flags (p_flags) ───────────────────────────────────────────

const (
	PF_X = 0x1 // executable
	PF_W = 0x2 // writable
	PF_R = 0x4 // readable
)

// ── Symbol binding (high nibble of st_info) ───────────────────────────────────

const (
	STB_LOCAL  = 0
	STB_GLOBAL = 1
	STB_WEAK   = 2
)

// ── Symbol type (low nibble of st_info) ───────────────────────────────────────

const (
	STT_NOTYPE  = 0
	STT_OBJECT  = 1
	STT_FUNC    = 2
	STT_SECTION = 3
	STT_FILE    = 4
	STT_COMMON  = 5
	STT_TLS     = 6
	STT_GNU_IFUNC = 10
)

// ── Symbol visibility (st_other) ─────────────────────────────────────────────

const (
	STV_DEFAULT   = 0
	STV_INTERNAL  = 1
	STV_HIDDEN    = 2
	STV_PROTECTED = 3
)

// ── Dynamic tag constants (d_tag) ────────────────────────────────────────────

const (
	DT_NULL         = 0
	DT_NEEDED       = 1
	DT_PLTRELSZ     = 2
	DT_PLTGOT       = 3
	DT_HASH         = 4
	DT_STRTAB       = 5
	DT_SYMTAB       = 6
	DT_RELA         = 7
	DT_RELASZ       = 8
	DT_RELAENT      = 9
	DT_STRSZ        = 10
	DT_SYMENT       = 11
	DT_INIT         = 12
	DT_FINI         = 13
	DT_SONAME       = 14
	DT_RPATH        = 15
	DT_SYMBOLIC     = 16
	DT_REL          = 17
	DT_RELSZ        = 18
	DT_RELENT       = 19
	DT_PLTREL       = 20
	DT_DEBUG        = 21
	DT_TEXTREL      = 22
	DT_JMPREL       = 23
	DT_BIND_NOW     = 24
	DT_INIT_ARRAY   = 25
	DT_FINI_ARRAY   = 26
	DT_INIT_ARRAYSZ = 27
	DT_FINI_ARRAYSZ = 28
	DT_RUNPATH      = 29
	DT_FLAGS        = 30
	DT_GNU_HASH     = 0x6FFFFEF5
	DT_VERSYM       = 0x6FFFFFF0
	DT_RELACOUNT    = 0x6FFFFFF9
	DT_RELCOUNT     = 0x6FFFFFFA
	DT_FLAGS_1      = 0x6FFFFFFB
	DT_VERNEED      = 0x6FFFFFFE
	DT_VERNEEDNUM   = 0x6FFFFFFF
)

// DT_FLAGS bit values.
const (
	DF_ORIGIN     = 0x01
	DF_SYMBOLIC   = 0x02
	DF_TEXTREL    = 0x04
	DF_BIND_NOW   = 0x08
	DF_STATIC_TLS = 0x10
)

// ── User-facing types ────────────────────────────────────────────────────────

// Section describes a single ELF section to be emitted into the binary.
type Section struct {
	// Name is the section name string, e.g. ".text", ".data", ".rodata".
	Name string

	// Type is the SHT_* section type constant.
	Type uint32

	// Flags is the SHF_* bitmask: SHF_ALLOC | SHF_EXECINSTR, etc.
	Flags uint64

	// Data holds the raw section content. For SHT_NOBITS (.bss) this
	// should be nil or empty; set Size to the desired in-memory byte count.
	Data []byte

	// Align is the required address and file alignment. Must be a power
	// of two (≥ 1). Zero is treated as 1.
	Align uint64

	// Size overrides the section size for SHT_NOBITS sections. Ignored for
	// sections with Data content; their size is len(Data).
	Size uint64

	// Link and Info carry the sh_link / sh_info semantics defined by each
	// section type. Most user sections leave these zero.
	Link    uint32
	Info    uint32

	// EntSize is the entry size for table sections (SHT_SYMTAB, SHT_RELA,
	// SHT_DYNAMIC, etc.). Leave zero for variable-content sections.
	EntSize uint64
}

// Symbol describes a symbol-table entry to include in .symtab.
type Symbol struct {
	// Name is the symbol's string-table name.
	Name string

	// Section is the name of the section this symbol is defined in.
	// Use "" for undefined (SHN_UNDEF) or external symbols.
	// Use "*ABS*" for absolute symbols (SHN_ABS).
	Section string

	// Offset is the byte offset from the start of Section.
	Offset uint64

	// Size is the symbol size in bytes (may be 0).
	Size uint64

	// Binding / visibility.
	Global bool  // STB_GLOBAL when true, otherwise STB_LOCAL
	Weak   bool  // STB_WEAK (takes precedence over Global when both set)

	// Type is the STT_* constant (0 → STT_NOTYPE).
	Type uint8

	// Vis is the STV_* visibility constant (0 → STV_DEFAULT).
	Vis uint8
}

// Reloc describes a RELA relocation entry to be emitted into a .rela.<section>
// section. ELF64 uses RELA exclusively (explicit addend).
type Reloc struct {
	// Section is the name of the section being patched, e.g. ".text".
	Section string

	// Offset is the byte offset within Section at which to apply the fix-up.
	Offset uint64

	// Symbol is the name of the symbol the relocation references.
	Symbol string

	// Type is the architecture-specific relocation type (R_X86_64_*, etc.).
	Type uint32

	// Addend is the explicit constant addend (r_addend in Elf64_Rela).
	Addend int64
}