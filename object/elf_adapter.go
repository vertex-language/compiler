package object

import "github.com/vertex-language/compiler/object/elf"

// ── elfObject ─────────────────────────────────────────────────────────────────

type elfObject struct {
	inner *elf.Object
	arch  Arch
	plat  Platform
}

func newELFObject(arch Arch, plat Platform) Object {
	a := elf.ArchAMD64
	if arch == ARM64 {
		a = elf.ArchARM64
	}
	return &elfObject{inner: elf.NewObject(a), arch: arch, plat: plat}
}

func (o *elfObject) Platform() Platform        { return o.plat }
func (o *elfObject) Arch() Arch                { return o.arch }
func (o *elfObject) Emit() ([]byte, error)     { return o.inner.Emit() }
func (o *elfObject) Text() Section             { return o.inner.Text() }
func (o *elfObject) Data() Section             { return o.inner.Data() }
func (o *elfObject) Rodata() Section           { return o.inner.Rodata() }
func (o *elfObject) Bss() BSSSection           { return &elfBSSSection{inner: o.inner.Bss()} }

func (o *elfObject) AddSymbol(sym Symbol) {
	o.inner.AddSymbol(elf.Symbol{
		Name:       sym.Name,
		Section:    sym.Section,
		Offset:     sym.Offset,
		Global:     sym.Global,
		Weak:       sym.Weak,
		IsFunction: sym.IsFunction,
		Abs:        sym.Abs,
		AbsValue:   sym.AbsValue,
	})
}

func (o *elfObject) AddReloc(r Reloc) {
	o.inner.AddReloc(elf.Reloc{
		Section: r.Section,
		Offset:  uint64(r.Offset),
		Symbol:  r.Symbol,
		Type:    elfRelocType(o.arch, r.Kind),
		Addend:  r.Addend,
	})
}

// elfRelocType maps a semantic RelocKind to the native ELF relocation constant
// for the given architecture.
func elfRelocType(arch Arch, k RelocKind) uint32 {
	// R_X86_64_GOTTPOFF (22) is not yet defined in object/elf/types.go.
	const rAMD64GOTTPOFF = uint32(22)

	if arch == AMD64 {
		switch k {
		case RelocCall32:  return elf.RAMD64PLT32
		case RelocAbs64:   return elf.RAMD64_64
		case RelocPCRel32: return elf.RAMD64PC32
		case RelocGOTLoad: return elf.RAMD64RexGOTPCRelX
		case RelocTLSIE:   return rAMD64GOTTPOFF
		}
	} else { // ARM64
		switch k {
		case RelocCall32:        return elf.RARM64Call26
		case RelocAbs64:         return elf.RARM64Abs64
		case RelocADRP:          return elf.RARM64AdrPrelPgHi21
		case RelocADRPOff12Add:  return elf.RARM64AddAbsLo12Nc
		case RelocADRPOff12Load: return elf.RARM64Ldst64AbsLo12Nc
		}
	}
	panicUnsupportedReloc(arch, k, "ELF")
	return 0
}

// ── elfBSSSection ─────────────────────────────────────────────────────────────

// elfBSSSection wraps the unexported *elf.section to satisfy BSSSection.
// It reaches through the package boundary via a structural interface that
// matches the concrete type's exported method set.
type elfBSSSection struct {
	inner interface {
		Len() int
		Write([]byte) (int, error)
		WriteByte(byte) error
		GrowNobits(uint64)
	}
}

func (b *elfBSSSection) Len() int                    { return b.inner.Len() }
func (b *elfBSSSection) Write(p []byte) (int, error) { return b.inner.Write(p) }
func (b *elfBSSSection) WriteByte(c byte) error      { return b.inner.WriteByte(c) }
func (b *elfBSSSection) Grow(sz uint64)              { b.inner.GrowNobits(sz) }