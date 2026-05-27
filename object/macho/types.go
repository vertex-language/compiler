// object/macho/types.go

// Package macho constructs Mach-O MH_OBJECT relocatable object files.
//
// The cpu backend writes machine code, data, symbols, and relocations into an
// Object; Emit() serialises it to bytes that linker/macho.ParseObject can
// consume directly — no intermediate format, no translation layer.
//
//	obj := macho.NewObject(macho.ArchAMD64)
//
//	text := obj.Text()
//	symOff := text.Len()
//	text.Write(machineCode)
//
//	obj.AddSymbol(macho.Symbol{
//	    Name: "main", SegmentName: "__TEXT", SectionName: "__text",
//	    Offset: uint64(symOff), Global: true,
//	})
//
//	obj.AddReloc(macho.Reloc{
//	    SectionName: "__text", Offset: uint32(callSite),
//	    Extern: true, Symbol: "_runtime_malloc",
//	    PCRel: true, Length: 2, Type: macho.RelocAMD64Branch,
//	})
//
//	data, err := obj.Emit() // valid MH_OBJECT — pass to linker/macho.ParseObject
package macho

// ── Architecture ──────────────────────────────────────────────────────────────

// Arch is the Mach-O cputype for the target architecture.
type Arch uint32

const (
	ArchAMD64 Arch = 0x01000007 // CPU_TYPE_X86_64
	ArchARM64 Arch = 0x0100000C // CPU_TYPE_ARM64
)

// cpuSubtype returns the canonical cpusubtype for the architecture.
func (a Arch) cpuSubtype() uint32 {
	switch a {
	case ArchAMD64:
		return 3 // CPU_SUBTYPE_X86_64_ALL
	default:
		return 0 // CPU_SUBTYPE_ARM64_ALL
	}
}

// ── Mach-O header constants ───────────────────────────────────────────────────

const (
	mhMagic64   uint32 = 0xFEEDFACF
	mhObject    uint32 = 0x1
	lcSegment64 uint32 = 0x19
	lcSymtab    uint32 = 0x02
)

// ── Mach-O header flags ───────────────────────────────────────────────────────

const (
	// MHSubsectionsViaSym enables dead-stripping at the symbol level.
	// Clang sets this on every MH_OBJECT it produces; we do the same.
	MHSubsectionsViaSym uint32 = 0x2000
)

// ── Section types (low byte of section flags) ─────────────────────────────────

const (
	STypeRegular         = uint32(0x00)
	STypeZerofill        = uint32(0x01)
	STypeCStringLiterals = uint32(0x02)
	SType4ByteLiterals   = uint32(0x03)
	SType8ByteLiterals   = uint32(0x04)
	STypeLiteralPointers = uint32(0x05)
	SType16ByteLiterals  = uint32(0x0e)
)

// ── Section attributes (high bytes of section flags) ──────────────────────────

const (
	SAttrPureInstructions = uint32(0x80000000)
	SAttrSomeInstructions = uint32(0x00000400)
	SAttrDebug            = uint32(0x02000000)
)

// ── nlist_64 type byte composition ───────────────────────────────────────────

const (
	nExt  = uint8(0x01) // N_EXT  — external (global) symbol
	nType = uint8(0x0e) // N_TYPE — type-field mask

	nUndf = uint8(0x00) // N_UNDF — undefined
	nAbs  = uint8(0x02) // N_ABS  — absolute
	nSect = uint8(0x0e) // N_SECT — defined in a section
)

// ── nlist_64 desc bits ────────────────────────────────────────────────────────

const (
	nWeakRef = uint16(0x0040) // N_WEAK_REF — weak reference (undefined)
	nWeakDef = uint16(0x0080) // N_WEAK_DEF — weak definition (defined)
)

// ── AMD64 relocation types (X86_64_RELOC_*) ───────────────────────────────────

const (
	RelocAMD64Unsigned   = uint8(0)
	RelocAMD64Signed     = uint8(1)
	RelocAMD64Branch     = uint8(2)
	RelocAMD64GotLoad    = uint8(3)
	RelocAMD64Got        = uint8(4)
	RelocAMD64Subtractor = uint8(5)
	RelocAMD64Signed1    = uint8(6)
	RelocAMD64Signed2    = uint8(7)
	RelocAMD64Signed4    = uint8(8)
	RelocAMD64TLV        = uint8(9)
)

// ── ARM64 relocation types (ARM64_RELOC_*) ────────────────────────────────────

const (
	RelocARM64Unsigned          = uint8(0)
	RelocARM64Subtractor        = uint8(1)
	RelocARM64Branch26          = uint8(2)
	RelocARM64Page21            = uint8(3)
	RelocARM64Pageoff12         = uint8(4)
	RelocARM64GotLoadPage21     = uint8(5)
	RelocARM64GotLoadPageoff12  = uint8(6)
	RelocARM64PointerToGot      = uint8(7)
	RelocARM64TlvpLoadPage21    = uint8(8)
	RelocARM64TlvpLoadPageoff12 = uint8(9)
	RelocARM64Addend            = uint8(10)
)

// ── Wire sizes ────────────────────────────────────────────────────────────────

const (
	machHeader64Size = 32 // mach_header_64
	segCmd64Size     = 72 // segment_command_64
	section64Size    = 80 // section_64
	symtabCmdSize    = 24 // symtab_command
	nlist64Size      = 16 // nlist_64
	relocEntrySize   = 8  // relocation_info
)