// sections.go
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
	ELFOSABI_NONE       = 0 // System V / none
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

// ── Processor-specific flags (e_flags) ───────────────────────────────────────
//
// AMD64 and AArch64 define no mandatory e_flags; leave at zero.
// RISC-V requires e_flags to encode the float ABI and ISA extensions in every
// output binary. A zero e_flags field is technically malformed for EM_RISCV.

// EF_RISCV_* flags for EM_RISCV binaries.
const (
	EF_RISCV_RVC              uint32 = 0x0001 // compressed (C) extension present
	EF_RISCV_FLOAT_ABI_SOFT   uint32 = 0x0000 // software float ABI (no FPU)
	EF_RISCV_FLOAT_ABI_SINGLE uint32 = 0x0002 // single-precision float ABI
	EF_RISCV_FLOAT_ABI_DOUBLE uint32 = 0x0004 // double-precision float ABI (most common)
	EF_RISCV_FLOAT_ABI_QUAD   uint32 = 0x0006 // quad-precision float ABI
	EF_RISCV_FLOAT_ABI_MASK   uint32 = 0x0006
	EF_RISCV_RVE              uint32 = 0x0008 // E extension (embedded, 16 integer regs)
	EF_RISCV_TSO              uint32 = 0x0010 // TSO memory model
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
	SHN_LORESERVE = 0xFF00
	SHN_LOPROC    = 0xFF00
	SHN_HIPROC    = 0xFF1F
	SHN_LOOS      = 0xFF20
	SHN_HIOS      = 0xFF3F
	SHN_ABS       = 0xFFF1
	SHN_COMMON    = 0xFFF2
	SHN_XINDEX    = 0xFFFF
	SHN_HIRESERVE = 0xFFFF
)

// PN_XNUM is the e_phnum sentinel indicating the real program header count is
// stored in section header [0].sh_info.
const PN_XNUM = 0xFFFF

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
	PT_GNU_PROPERTY = 0x6474E553
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
	STT_NOTYPE    = 0
	STT_OBJECT    = 1
	STT_FUNC      = 2
	STT_SECTION   = 3
	STT_FILE      = 4
	STT_COMMON    = 5
	STT_TLS       = 6
	STT_GNU_IFUNC = 10
)

// ── Symbol visibility (st_other) ─────────────────────────────────────────────

const (
	STV_DEFAULT   = 0
	STV_INTERNAL  = 1
	STV_HIDDEN    = 2
	STV_PROTECTED = 3
)

// ── Symbol version index sentinels ───────────────────────────────────────────

const (
	VER_NDX_LOCAL  = 0 // symbol is local; not visible outside the object
	VER_NDX_GLOBAL = 1 // symbol is global with no explicit version
)

// VER_FLG_* are flag bits in Elf64_Vernaux.vna_flags.
const (
	VER_FLG_BASE = 0x1 // version definition of the file itself
	VER_FLG_WEAK = 0x2 // weak version reference
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

// DT_FLAGS bit values (d_val for DT_FLAGS entry).
const (
	DF_ORIGIN     = 0x01
	DF_SYMBOLIC   = 0x02
	DF_TEXTREL    = 0x04
	DF_BIND_NOW   = 0x08
	DF_STATIC_TLS = 0x10
)

// DT_FLAGS_1 bit values (d_val for DT_FLAGS_1 entry).
const (
	DF_1_NOW        uint32 = 0x00000001 // perform complete relocation at load
	DF_1_GLOBAL     uint32 = 0x00000002
	DF_1_GROUP      uint32 = 0x00000004
	DF_1_NODELETE   uint32 = 0x00000008 // object cannot be unloaded
	DF_1_LOADFLTR   uint32 = 0x00000010
	DF_1_INITFIRST  uint32 = 0x00000020
	DF_1_NOOPEN     uint32 = 0x00000040 // cannot be opened with dlopen
	DF_1_ORIGIN     uint32 = 0x00000080
	DF_1_DIRECT     uint32 = 0x00000100
	DF_1_INTERPOSE  uint32 = 0x00000400 // interposes symbol table
	DF_1_NODEFLIB   uint32 = 0x00000800
	DF_1_NODUMP     uint32 = 0x00001000
	DF_1_DISPRELDNE uint32 = 0x00008000
	DF_1_DISPRELPND uint32 = 0x00010000
	DF_1_NODIRECT   uint32 = 0x00020000
	DF_1_IGNMULDEF  uint32 = 0x00040000
	DF_1_NOKSYMS    uint32 = 0x00080000
	DF_1_NOHDR      uint32 = 0x00100000
	DF_1_EDITED     uint32 = 0x00200000
	DF_1_NORELOC    uint32 = 0x00400000
	DF_1_SYMINTPOSE uint32 = 0x00800000
	DF_1_GLOBAUDIT  uint32 = 0x01000000
	DF_1_SINGLETON  uint32 = 0x02000000
	DF_1_PIE        uint32 = 0x08000000 // position-independent executable
)

// ── User-facing types ────────────────────────────────────────────────────────

// Section describes a single ELF section to be emitted into the binary.
type Section struct {
	Name    string
	Type    uint32
	Flags   uint64
	Data    []byte
	Align   uint64
	Size    uint64
	Link    uint32
	Info    uint32
	EntSize uint64

	// PreassignedAddr and PreassignedFileOffset, when non-zero, instruct the
	// emitter to place this section at exactly these addresses rather than
	// computing new ones. Set by the ELF linker so that bytes patched at link
	// time (PLT stubs, relocated .text) are serialized at the addresses the
	// linker used when patching them.
	PreassignedAddr       uint64
	PreassignedFileOffset uint64
}

// Symbol describes a symbol-table entry to include in .symtab.
type Symbol struct {
	// Name is the symbol's string-table name.
	Name string

	// Section is the name of the section this symbol is defined in.
	// Use "" for undefined (SHN_UNDEF) symbols.
	// Use "*ABS*" for absolute symbols (SHN_ABS).
	Section string

	// Offset is the byte offset from the start of Section.
	Offset uint64

	// Size is the symbol size in bytes (may be 0).
	Size uint64

	// Binding and visibility.
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

	// Offset is the byte offset within Section at which to apply the fixup.
	Offset uint64

	// Symbol is the name of the symbol the relocation references.
	Symbol string

	// Type is the architecture-specific relocation type (R_X86_64_*, etc.).
	Type uint32

	// Addend is the explicit constant addend (r_addend in Elf64_Rela).
	Addend int64
}