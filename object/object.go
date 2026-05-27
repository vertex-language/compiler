// Package object is the platform-neutral interface between the cpu backend
// and the format-specific object sub-packages (elf, pe, macho).
//
// The cpu backend works exclusively against Object and Section; it never
// imports object/elf, object/pe, or object/macho directly. The driver
// (cmd/compile or similar) calls New() with the target platform and hands
// the result to the backend.
//
//	obj := object.New(object.AMD64, object.Linux)
//
//	text := obj.Text()
//	text.Write(machineCode)
//
//	obj.AddSymbol(object.Symbol{Name: "main", Section: ".text", Offset: 0, Global: true})
//	obj.AddReloc(object.Reloc{Section: ".text", Offset: callSite, Symbol: "puts", Kind: object.RelocCall32})
//
//	bytes, err := obj.Emit()
package object

import "fmt"

// ── Target axes ───────────────────────────────────────────────────────────────

// Arch identifies the target CPU architecture.
type Arch uint8

const (
	AMD64 Arch = iota
	ARM64
)

func (a Arch) String() string {
	switch a {
	case AMD64:
		return "AMD64"
	case ARM64:
		return "ARM64"
	default:
		return fmt.Sprintf("Arch(%d)", uint8(a))
	}
}

// Platform identifies the target operating system / object-file ABI.
type Platform uint8

const (
	Linux   Platform = iota
	Darwin
	Windows
	FreeBSD
)

// CallingConv identifies the function-call ABI for a platform/arch pair.
type CallingConv uint8

const (
	SystemV      CallingConv = iota // SysV AMD64 ABI — Linux, macOS, *BSD
	MicrosoftX64                    // Windows x64 ABI
	WindowsARM64                    // Windows ARM64 ABI
)

// CallingConv returns the calling convention for this platform/arch pair.
// The cpu backend queries this to know which registers to use.
func (p Platform) CallingConv(a Arch) CallingConv {
	switch {
	case p == Windows && a == AMD64:
		return MicrosoftX64
	case p == Windows && a == ARM64:
		return WindowsARM64
	default:
		return SystemV // Linux, Darwin, FreeBSD on AMD64 and ARM64
	}
}

// ── Section interfaces ────────────────────────────────────────────────────────

// Section is the write surface the cpu backend holds for an individual section.
// Concrete implementations live inside the sub-packages and are never exposed.
type Section interface {
	Len() int
	Write([]byte) (int, error)
	WriteByte(byte) error
}

// BSSSection is a Section that supports reserving zero-initialised space.
// The cpu backend calls Grow instead of Write for BSS regions.
type BSSSection interface {
	Section
	// Grow extends the reservation to at least sz bytes.
	// It is idempotent: calling it with a smaller value than the current
	// reservation is a no-op.
	Grow(sz uint64)
}

// ── Platform-neutral symbol and reloc types ───────────────────────────────────

// Symbol is a platform-neutral symbol descriptor.
//
// Section uses canonical names: ".text", ".data", ".rodata", ".bss".
// Leave Section empty for an undefined external reference.
type Symbol struct {
	Name       string
	Section    string // canonical section name; "" = undefined external
	Offset     uint64
	Global     bool
	Weak       bool
	IsFunction bool
	Abs        bool   // absolute symbol; Section and Offset are ignored
	AbsValue   uint64 // value for Abs == true
}

// RelocKind is a semantic relocation type, independent of object format.
// Each Object implementation maps these to its native reloc constants.
type RelocKind uint8

const (
	// RelocCall32 is a 32-bit PC-relative branch/call to a symbol.
	//   ELF:    R_X86_64_PLT32 / R_AARCH64_CALL26
	//   PE:     RelAMD64Rel32  / RelARM64Branch26
	//   Mach-O: RelocAMD64Branch / RelocARM64Branch26
	RelocCall32 RelocKind = iota

	// RelocAbs64 is a 64-bit absolute address.
	//   ELF:    R_X86_64_64 / R_AARCH64_ABS64
	//   PE:     RelAMD64Addr64 / RelARM64Addr64
	//   Mach-O: RelocAMD64Unsigned / RelocARM64Unsigned
	RelocAbs64

	// RelocAbs32NB is a 32-bit image-relative (RVA) address. Windows only.
	//   PE: RelAMD64Addr32NB / RelARM64Addr32NB
	RelocAbs32NB

	// RelocPCRel32 is a generic 32-bit PC-relative data reference (not a call).
	//   ELF:    R_X86_64_PC32
	//   Mach-O: X86_64_RELOC_SIGNED
	RelocPCRel32

	// RelocGOTLoad is a GOT-indirect load.
	//   ELF:    R_X86_64_REX_GOTPCRELX
	//   Mach-O: X86_64_RELOC_GOT_LOAD
	RelocGOTLoad

	// RelocTLSIE is a thread-local storage initial-exec access.
	//   ELF:    R_X86_64_GOTTPOFF
	//   Mach-O: X86_64_RELOC_TLV
	RelocTLSIE

	// RelocADRP is an ARM64 page-relative ADRP instruction.
	//   ELF:    R_AARCH64_ADR_PREL_PG_HI21
	//   PE:     RelARM64PagebaseRel21
	//   Mach-O: ARM64_RELOC_PAGE21
	RelocADRP

	// RelocADRPOff12Add is an ARM64 ADD imm12 page offset, paired with ADRP.
	//   ELF:    R_AARCH64_ADD_ABS_LO12_NC
	//   PE:     RelARM64Pageoffset12A
	//   Mach-O: ARM64_RELOC_PAGEOFF12
	RelocADRPOff12Add

	// RelocADRPOff12Load is an ARM64 LDR/STR imm12 page offset, paired with ADRP.
	//   ELF:    R_AARCH64_LDST64_ABS_LO12_NC
	//   PE:     RelARM64Pageoffset12L
	//   Mach-O: ARM64_RELOC_PAGEOFF12
	RelocADRPOff12Load

	// RelocSEHUnwind is a PE .pdata → .xdata back-reference. Windows only.
	//   PE: RelAMD64Addr32NB / RelARM64Addr32NB on the .pdata section
	RelocSEHUnwind
)

// Reloc is a platform-neutral relocation descriptor.
//
// Addend is only meaningful for ELF RELA relocations; it is silently ignored
// on PE and Mach-O, both of which use implicit addends embedded in the
// instruction stream.
type Reloc struct {
	Section string    // canonical section name of the location being patched
	Offset  uint32    // byte offset within that section
	Symbol  string    // target symbol name
	Kind    RelocKind // semantic relocation kind
	Addend  int64     // ELF RELA addend; 0 on PE / Mach-O
}

// ── Object interface ──────────────────────────────────────────────────────────

// Object is the platform-neutral object file being constructed.
// The cpu backend calls only methods on this interface.
type Object interface {
	// Section accessors return the well-known sections, creating them on
	// first call. The cpu backend may call these many times; it always gets
	// the same underlying section back.
	Text() Section
	Data() Section
	Rodata() Section
	Bss() BSSSection

	// AddSymbol registers a symbol using canonical section names.
	AddSymbol(Symbol)

	// AddReloc records a relocation using a semantic RelocKind.
	AddReloc(Reloc)

	// Emit serialises the object to a native byte slice
	// (ELF64 ET_REL, COFF .obj, or Mach-O MH_OBJECT).
	Emit() ([]byte, error)

	// Platform and Arch report the target this object was created for.
	Platform() Platform
	Arch() Arch
}

// New returns a platform-native Object for the given target.
func New(arch Arch, platform Platform) Object {
	switch platform {
	case Darwin:
		return newMachoObject(arch, platform)
	case Windows:
		return newPEObject(arch, platform)
	default: // Linux, FreeBSD, …
		return newELFObject(arch, platform)
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// panicUnsupportedReloc is called by adapter reloc-mapping functions when a
// (RelocKind, Arch) combination has no native equivalent.
func panicUnsupportedReloc(arch Arch, k RelocKind, format string) {
	panic(fmt.Sprintf("object: unsupported reloc kind %d for %s/%s", k, format, arch))
}