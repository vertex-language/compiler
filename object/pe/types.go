// Package pe constructs COFF PE32+ relocatable object files (.obj).
//
// The cpu backend writes machine code, data, symbols, and relocations into an
// Object; Emit() serialises it to bytes that linker/pe.ParseObject can consume
// directly — no intermediate format, no translation layer.
//
//	obj := pe.NewObject(pe.ArchAMD64)
//
//	text := obj.Text()
//	symOff := text.Len()
//	text.Write(machineCode)
//
//	obj.AddSymbol(pe.Symbol{
//	    Name: "main", Section: ".text",
//	    Offset: uint32(symOff), Global: true, IsFunction: true,
//	})
//
//	obj.AddReloc(pe.Reloc{
//	    Section: ".text", Offset: uint32(callSite),
//	    Symbol:  "runtime.malloc", Type: pe.RelAMD64Rel32,
//	})
//
//	data, err := obj.Emit() // valid COFF .obj — pass to linker/pe.ParseObject
package pe

// Arch is the COFF Machine field value.
type Arch uint16

const (
	ArchAMD64 Arch = 0x8664
	ArchARM64 Arch = 0xAA64
)

// ── Section characteristics (IMAGE_SCN_*) ─────────────────────────────────────

const (
	ScnCntCode          = uint32(0x00000020) // IMAGE_SCN_CNT_CODE
	ScnCntInitialized   = uint32(0x00000040) // IMAGE_SCN_CNT_INITIALIZED_DATA
	ScnCntUninitialized = uint32(0x00000080) // IMAGE_SCN_CNT_UNINITIALIZED_DATA
	ScnLnkInfo          = uint32(0x00000200) // IMAGE_SCN_LNK_INFO
	ScnLnkRemove        = uint32(0x00000800) // IMAGE_SCN_LNK_REMOVE
	ScnLnkComdat        = uint32(0x00001000) // IMAGE_SCN_LNK_COMDAT
	ScnMemDiscardable   = uint32(0x02000000) // IMAGE_SCN_MEM_DISCARDABLE
	ScnMemExecute       = uint32(0x20000000) // IMAGE_SCN_MEM_EXECUTE
	ScnMemRead          = uint32(0x40000000) // IMAGE_SCN_MEM_READ
	ScnMemWrite         = uint32(0x80000000) // IMAGE_SCN_MEM_WRITE
)

// Section alignment constants (encoded in bits 20–23 of Characteristics).
const (
	ScnAlign1    = uint32(0x00100000) // IMAGE_SCN_ALIGN_1BYTES
	ScnAlign2    = uint32(0x00200000) // IMAGE_SCN_ALIGN_2BYTES
	ScnAlign4    = uint32(0x00300000) // IMAGE_SCN_ALIGN_4BYTES
	ScnAlign8    = uint32(0x00400000) // IMAGE_SCN_ALIGN_8BYTES
	ScnAlign16   = uint32(0x00500000) // IMAGE_SCN_ALIGN_16BYTES
	ScnAlign32   = uint32(0x00600000) // IMAGE_SCN_ALIGN_32BYTES
	ScnAlign64   = uint32(0x00700000) // IMAGE_SCN_ALIGN_64BYTES
	ScnAlign128  = uint32(0x00800000) // IMAGE_SCN_ALIGN_128BYTES
	ScnAlign256  = uint32(0x00900000) // IMAGE_SCN_ALIGN_256BYTES
	ScnAlign512  = uint32(0x00A00000) // IMAGE_SCN_ALIGN_512BYTES
	ScnAlign1024 = uint32(0x00B00000) // IMAGE_SCN_ALIGN_1024BYTES
	ScnAlign2048 = uint32(0x00C00000) // IMAGE_SCN_ALIGN_2048BYTES
	ScnAlign4096 = uint32(0x00D00000) // IMAGE_SCN_ALIGN_4096BYTES
)

// ── AMD64 relocation types (IMAGE_REL_AMD64_*) ────────────────────────────────

const (
	RelAMD64Absolute = uint16(0x0000) // no relocation
	RelAMD64Addr64   = uint16(0x0001) // 64-bit VA
	RelAMD64Addr32   = uint16(0x0002) // 32-bit VA
	RelAMD64Addr32NB = uint16(0x0003) // 32-bit RVA (no image base)
	RelAMD64Rel32    = uint16(0x0004) // 32-bit PC-relative, displacement to target minus 4
	RelAMD64Rel32_1  = uint16(0x0005) // as Rel32 but displacement minus 5
	RelAMD64Rel32_2  = uint16(0x0006) // as Rel32 but displacement minus 6
	RelAMD64Rel32_3  = uint16(0x0007) // as Rel32 but displacement minus 7
	RelAMD64Rel32_4  = uint16(0x0008) // as Rel32 but displacement minus 8
	RelAMD64Rel32_5  = uint16(0x0009) // as Rel32 but displacement minus 9
	RelAMD64Section  = uint16(0x000A) // 16-bit section index of defining section
	RelAMD64SecRel   = uint16(0x000B) // 32-bit offset from start of defining section
	RelAMD64SecRel7  = uint16(0x000C) // 7-bit offset from start of defining section
	RelAMD64Token    = uint16(0x000D) // CLR metadata token
)

// ── ARM64 relocation types (IMAGE_REL_ARM64_*) ────────────────────────────────

const (
	RelARM64Absolute      = uint16(0x0000)
	RelARM64Addr32        = uint16(0x0001)
	RelARM64Addr32NB      = uint16(0x0002)
	RelARM64Branch26      = uint16(0x0003)
	RelARM64PagebaseRel21 = uint16(0x0004) // ADRP page-relative
	RelARM64Rel21         = uint16(0x0005)
	RelARM64Pageoffset12A = uint16(0x0006) // ADD imm12 page offset
	RelARM64Pageoffset12L = uint16(0x0007) // LDR/STR imm12 page offset
	RelARM64Secrel        = uint16(0x0008) // 32-bit section-relative offset
	RelARM64Section       = uint16(0x000D) // 16-bit section index
	RelARM64Addr64        = uint16(0x000E)
	RelARM64Branch19      = uint16(0x000F)
	RelARM64Branch14      = uint16(0x0010)
	RelARM64Rel32         = uint16(0x0011)
)

// ── Symbol storage classes ────────────────────────────────────────────────────

const (
	SymClassExternal     = uint8(2)   // IMAGE_SYM_CLASS_EXTERNAL
	SymClassStatic       = uint8(3)   // IMAGE_SYM_CLASS_STATIC  (section/local)
	SymClassWeakExternal = uint8(105) // IMAGE_SYM_CLASS_WEAK_EXTERNAL
)

// ── Symbol type constants ─────────────────────────────────────────────────────

const (
	SymTypeNull     = uint16(0x0000)
	SymTypeFunction = uint16(0x0020) // IMAGE_SYM_DTYPE_FUNCTION in bits 4–7
)

// ── Weak-external search characteristics ─────────────────────────────────────

const (
	WeakSearchNolibrary = uint32(1) // IMAGE_WEAK_EXTERN_SEARCH_NOLIBRARY
	WeakSearchLibrary   = uint32(2) // IMAGE_WEAK_EXTERN_SEARCH_LIBRARY
	WeakSearchAlias     = uint32(3) // IMAGE_WEAK_EXTERN_SEARCH_ALIAS
)

// ── Wire sizes (mirrors linker/pe constants) ──────────────────────────────────

const (
	coffHdrSize  = 20
	secHdrSize   = 40
	symRecSize   = 18
	relocRecSize = 10
)