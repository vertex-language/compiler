// Package elf constructs ELF64 ET_REL relocatable object files (.o).
//
// The cpu backend writes machine code, data, symbols, and relocations into an
// Object; Emit() serialises it to bytes that linker/elf.ParseObject can consume
// directly — no intermediate format, no translation layer.
//
//	obj := elf.NewObject(elf.ArchAMD64)
//
//	text := obj.Text()
//	symOff := text.Len()
//	text.Write(machineCode)
//
//	obj.AddSymbol(elf.Symbol{
//	    Name: "main", Section: ".text",
//	    Offset: uint64(symOff), Global: true, IsFunction: true,
//	})
//
//	obj.AddReloc(elf.Reloc{
//	    Section: ".text", Offset: uint64(callSite),
//	    Symbol:  "runtime.malloc", Type: elf.RAMD64PLT32, Addend: -4,
//	})
//
//	data, err := obj.Emit() // valid ELF64 ET_REL — pass to linker/elf.ParseObject
package elf

// Arch is the ELF e_machine value for the target architecture.
type Arch uint16

const (
	ArchAMD64 Arch = 0x3E // EM_X86_64
	ArchARM64 Arch = 0xB7 // EM_AARCH64
	ArchRISCV Arch = 0xF3 // EM_RISCV
)

// ── Section flags (SHF_*) ──────────────────────────────────────────────────────

const (
	SHFWrite     = uint64(0x001) // SHF_WRITE
	SHFAlloc     = uint64(0x002) // SHF_ALLOC
	SHFExecinstr = uint64(0x004) // SHF_EXECINSTR
	SHFMerge     = uint64(0x010) // SHF_MERGE
	SHFStrings   = uint64(0x020) // SHF_STRINGS
	SHFInfoLink  = uint64(0x040) // SHF_INFO_LINK — sh_info holds a section index
	SHFLinkOrder = uint64(0x080) // SHF_LINK_ORDER
	SHFTLS       = uint64(0x400) // SHF_TLS
)

// ── Section types (SHT_*) ─────────────────────────────────────────────────────

const (
	SHTProgbits = uint32(1) // SHT_PROGBITS — section has file bytes
	SHTSymtab   = uint32(2) // SHT_SYMTAB   — symbol table (full)
	SHTStrtab   = uint32(3) // SHT_STRTAB   — string table
	SHTRela     = uint32(4) // SHT_RELA     — explicit-addend relocations
	SHTNobits   = uint32(8) // SHT_NOBITS   — BSS / zero-fill; no file bytes
)

// ── AMD64 relocation types (R_X86_64_*) ───────────────────────────────────────

const (
	RAMD64None    = uint32(0)  // R_X86_64_NONE    no relocation
	RAMD64_64     = uint32(1)  // R_X86_64_64      S + A
	RAMD64PC32    = uint32(2)  // R_X86_64_PC32    S + A − P
	RAMD64GOT32   = uint32(3)  // R_X86_64_GOT32   G + A
	RAMD64PLT32   = uint32(4)  // R_X86_64_PLT32   L + A − P
	RAMD64Copy    = uint32(5)  // R_X86_64_COPY
	RAMD64GlobDat = uint32(6)  // R_X86_64_GLOB_DAT
	RAMD64JmpSlot = uint32(7)  // R_X86_64_JUMP_SLOT
	RAMD64Relative= uint32(8)  // R_X86_64_RELATIVE
	RAMD64GOTPCRel= uint32(9)  // R_X86_64_GOTPCREL  G + GOT + A − P
	RAMD64_32     = uint32(10) // R_X86_64_32       S + A  (zero-extend to 64)
	RAMD64_32S    = uint32(11) // R_X86_64_32S      S + A  (sign-extend to 64)
	RAMD64_16     = uint32(12) // R_X86_64_16
	RAMD64PC16    = uint32(13) // R_X86_64_PC16
	RAMD64_8      = uint32(14) // R_X86_64_8
	RAMD64PC8     = uint32(15) // R_X86_64_PC8
	RAMD64PC64    = uint32(24) // R_X86_64_PC64     S + A − P (64-bit PC-rel)
	RAMD64GOTPCRelX = uint32(41) // R_X86_64_GOTPCRELX  relaxable GOTPCREL
	RAMD64RexGOTPCRelX = uint32(42) // R_X86_64_REX_GOTPCRELX
)

// ── ARM64 relocation types (R_AARCH64_*) ──────────────────────────────────────

const (
	RARM64None            = uint32(0)   // R_AARCH64_NONE
	RARM64Abs64           = uint32(257) // R_AARCH64_ABS64           S + A
	RARM64Abs32           = uint32(258) // R_AARCH64_ABS32           S + A
	RARM64Prel32          = uint32(261) // R_AARCH64_PREL32          S + A − P
	RARM64AdrPrelPgHi21   = uint32(275) // R_AARCH64_ADR_PREL_PG_HI21 ADRP page delta
	RARM64AddAbsLo12Nc    = uint32(277) // R_AARCH64_ADD_ABS_LO12_NC ADD imm12
	RARM64Ldst8AbsLo12Nc  = uint32(278) // R_AARCH64_LDST8_ABS_LO12_NC
	RARM64Ldst16AbsLo12Nc = uint32(284) // R_AARCH64_LDST16_ABS_LO12_NC
	RARM64Ldst32AbsLo12Nc = uint32(285) // R_AARCH64_LDST32_ABS_LO12_NC
	RARM64Ldst64AbsLo12Nc = uint32(286) // R_AARCH64_LDST64_ABS_LO12_NC
	RARM64Jump26          = uint32(282) // R_AARCH64_JUMP26          B target
	RARM64Call26          = uint32(283) // R_AARCH64_CALL26          BL target
)

// ── RISC-V relocation types (R_RISCV_*) ───────────────────────────────────────

const (
	RRISCVNone       = uint32(0)  // R_RISCV_NONE
	RRISCV32         = uint32(1)  // R_RISCV_32        S + A
	RRISCV64         = uint32(2)  // R_RISCV_64        S + A
	RRISCVJal        = uint32(17) // R_RISCV_JAL       S + A − P → J-type
	RRISCVCall       = uint32(18) // R_RISCV_CALL      AUIPC+JALR pair
	RRISCVCallPlt    = uint32(19) // R_RISCV_CALL_PLT  AUIPC+JALR via PLT
	RRISCVPcrelHi20  = uint32(23) // R_RISCV_PCREL_HI20  %pcrel_hi → AUIPC
	RRISCVPcrelLo12I = uint32(24) // R_RISCV_PCREL_LO12_I %pcrel_lo → I-type
	RRISCVPcrelLo12S = uint32(25) // R_RISCV_PCREL_LO12_S %pcrel_lo → S-type
	RRISCVHi20       = uint32(26) // R_RISCV_HI20      %hi(S+A) → LUI
	RRISCVLo12I      = uint32(27) // R_RISCV_LO12_I    %lo(S+A) → I-type
	RRISCVLo12S      = uint32(28) // R_RISCV_LO12_S    %lo(S+A) → S-type
)

// ── Symbol binding (st_info high nibble) ──────────────────────────────────────

const (
	STBLocal  = uint8(0) // STB_LOCAL  — not visible outside translation unit
	STBGlobal = uint8(1) // STB_GLOBAL — globally visible
	STBWeak   = uint8(2) // STB_WEAK   — globally visible, overridable
)

// ── Symbol type (st_info low nibble) ──────────────────────────────────────────

const (
	STTNotype  = uint8(0) // STT_NOTYPE
	STTObject  = uint8(1) // STT_OBJECT   — data variable
	STTFunc    = uint8(2) // STT_FUNC     — function or executable code
	STTSection = uint8(3) // STT_SECTION  — associated with a section
	STTFile    = uint8(4) // STT_FILE     — source file name
	STTCommon  = uint8(5) // STT_COMMON   — uninitialized common block
	STTTLS     = uint8(6) // STT_TLS      — thread-local storage
)

// ── Symbol visibility (st_other low 2 bits) ───────────────────────────────────

const (
	STVDefault   = uint8(0) // STV_DEFAULT   — default visibility rules
	STVInternal  = uint8(1) // STV_INTERNAL  — processor-specific hidden
	STVHidden    = uint8(2) // STV_HIDDEN    — not visible outside DSO
	STVProtected = uint8(3) // STV_PROTECTED — visible but not preemptible
)

// ── Special section indices ───────────────────────────────────────────────────

const (
	SHNUndef  = uint16(0x0000) // SHN_UNDEF   — undefined / not yet defined
	SHNAbs    = uint16(0xFFF1) // SHN_ABS     — absolute symbol value
	SHNCommon = uint16(0xFFF2) // SHN_COMMON  — common block (C tentative def)
)

// ── Wire sizes ────────────────────────────────────────────────────────────────

const (
	elfHdrSize = 64 // sizeof(Elf64_Ehdr)
	shdrSize   = 64 // sizeof(Elf64_Shdr)
	symSize    = 24 // sizeof(Elf64_Sym)
	relaSize   = 24 // sizeof(Elf64_Rela)
)