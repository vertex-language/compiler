// parse.go
// ELF64 structure field offsets, sizes, and bit-manipulation helpers.
// All offsets are relative to the start of the structure, not the file.
// Sources: ELF-64 Object File Format v1.5, System V AMD64 ABI §4.
package elf

// ── ELF Machine types ─────────────────────────────────────────────────────────

const (
	EM_X86_64  uint16 = 0x3E // 62
	EM_AARCH64 uint16 = 0xB7 // 183
	EM_RISCV   uint16 = 0xF3 // 243
)

// ── Elf64_Ehdr (64 bytes) ────────────────────────────────────────────────────

const (
	ehoff_ident     = 0  // [16]uint8
	ehoff_type      = 16 // uint16  e_type
	ehoff_machine   = 18 // uint16  e_machine
	ehoff_version   = 20 // uint32  e_version
	ehoff_entry     = 24 // uint64  e_entry
	ehoff_phoff     = 32 // uint64  e_phoff
	ehoff_shoff     = 40 // uint64  e_shoff
	ehoff_flags     = 48 // uint32  e_flags
	ehoff_ehsize    = 52 // uint16  e_ehsize
	ehoff_phentsize = 54 // uint16  e_phentsize
	ehoff_phnum     = 56 // uint16  e_phnum
	ehoff_shentsize = 58 // uint16  e_shentsize
	ehoff_shnum     = 60 // uint16  e_shnum
	ehoff_shstrndx  = 62 // uint16  e_shstrndx
	ehdrSize        = 64
)

// e_ident byte indices (already defined in bin/elf — repeated here for
// standalone use in the parser without importing that package).
const (
	eiClass = 4 // EI_CLASS
	eiData  = 5 // EI_DATA
)

const (
	elfClass64  = 2 // ELFCLASS64
	elfData2LSB = 1 // ELFDATA2LSB (little-endian)
)

// ── Elf64_Shdr (64 bytes) ─────────────────────────────────────────────────────

const (
	shoff_name      = 0  // uint32  sh_name
	shoff_type      = 4  // uint32  sh_type
	shoff_flags     = 8  // uint64  sh_flags
	shoff_addr      = 16 // uint64  sh_addr
	shoff_offset    = 24 // uint64  sh_offset
	shoff_size      = 32 // uint64  sh_size
	shoff_link      = 40 // uint32  sh_link
	shoff_info      = 44 // uint32  sh_info
	shoff_addralign = 48 // uint64  sh_addralign
	shoff_entsize   = 56 // uint64  sh_entsize
	shdrSize        = 64
)

// ── Elf64_Sym (24 bytes) ──────────────────────────────────────────────────────
//
// Note: unlike Elf32_Sym, the 64-bit layout moves st_info/st_other/st_shndx
// before st_value to reduce padding.
//
//   offset  size  field
//   0       4     st_name
//   4       1     st_info
//   5       1     st_other
//   6       2     st_shndx
//   8       8     st_value
//   16      8     st_size

const (
	symoff_name  = 0  // uint32
	symoff_info  = 4  // uint8
	symoff_other = 5  // uint8
	symoff_shndx = 6  // uint16
	symoff_value = 8  // uint64
	symoff_size  = 16 // uint64
	symEntSize   = 24
)

// ── Elf64_Rela (24 bytes) ─────────────────────────────────────────────────────

const (
	relaoff_offset = 0  // uint64  r_offset
	relaoff_info   = 8  // uint64  r_info  (sym<<32 | type)
	relaoff_addend = 16 // int64   r_addend
	relaEntSize    = 24
)

// ── Elf64_Dyn (16 bytes) ──────────────────────────────────────────────────────

const (
	dynoff_tag = 0 // int64   d_tag
	dynoff_val = 8 // uint64  d_val / d_ptr
	dynEntSize = 16
)

// ── r_info encoding ───────────────────────────────────────────────────────────

func relaSymIdx(info uint64) uint32 { return uint32(info >> 32) }
func relaType(info uint64) uint32   { return uint32(info) }

// ── st_info encoding ──────────────────────────────────────────────────────────

func stBind(info uint8) uint8 { return info >> 4 }
func stType(info uint8) uint8 { return info & 0xf }

// Symbol binding constants (duplicated from bin/elf for parser independence).
const (
	stbLocal  = 0
	stbGlobal = 1
	stbWeak   = 2
)

// Symbol type constants.
const (
	sttNotype = 0
	sttObject = 1
	sttFunc   = 2
	sttSection= 3
	sttFile   = 4
	sttCommon = 5
	sttTLS    = 6
)

// Special section indices.
const (
	shnUndef     = 0
	shnAbs       = 0xFFF1
	shnCommon    = 0xFFF2
	shnXindex    = 0xFFFF
	shnLoreserve = 0xFF00
)

// Section types (subset needed for parsing).
const (
	shtNull      = 0
	shtProgbits  = 1
	shtSymtab    = 2
	shtStrtab    = 3
	shtRela      = 4
	shtHash      = 5
	shtDynamic   = 6
	shtNobits    = 8
	shtDynsym    = 11
	shtGnuHash   = 0x6FFFFFF6
	shtGnuVerneed= 0x6FFFFFFE
	shtGnuVersym = 0x6FFFFFFF
)

// ELF file types.
const (
	etRel  = 1
	etExec = 2
	etDyn  = 3
)

// Dynamic tags (subset for shared-lib parsing).
const (
	dtNull    = 0
	dtNeeded  = 1
	dtStrtab  = 5
	dtSymtab  = 6
	dtStrsz   = 10
	dtSoname  = 14
	dtRpath   = 15
	dtRunpath = 29
)