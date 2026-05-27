package object

import "github.com/vertex-language/compiler/object/pe"

// ── peObject ──────────────────────────────────────────────────────────────────

type peObject struct {
	inner *pe.Object
	arch  Arch
	plat  Platform
}

func newPEObject(arch Arch, plat Platform) Object {
	a := pe.ArchAMD64
	if arch == ARM64 {
		a = pe.ArchARM64
	}
	return &peObject{inner: pe.NewObject(a), arch: arch, plat: plat}
}

func (o *peObject) Platform() Platform        { return o.plat }
func (o *peObject) Arch() Arch                { return o.arch }
func (o *peObject) Emit() ([]byte, error)     { return o.inner.Emit() }
func (o *peObject) Text() Section             { return o.inner.Text() }
func (o *peObject) Data() Section             { return o.inner.Data() }
func (o *peObject) Rodata() Section           { return o.inner.Rodata() }
func (o *peObject) Bss() BSSSection           { return &peBSSSection{inner: o.inner.Bss()} }

func (o *peObject) AddSymbol(sym Symbol) {
	o.inner.AddSymbol(pe.Symbol{
		Name:       sym.Name,
		Section:    sym.Section,
		Offset:     uint32(sym.Offset),
		Global:     sym.Global,
		Weak:       sym.Weak,
		IsFunction: sym.IsFunction,
		Abs:        sym.Abs,
		AbsValue:   uint32(sym.AbsValue),
	})
}

func (o *peObject) AddReloc(r Reloc) {
	o.inner.AddReloc(pe.Reloc{
		Section: r.Section,
		Offset:  r.Offset,
		Symbol:  r.Symbol,
		Type:    peRelocType(o.arch, r.Kind),
	})
}

// peRelocType maps a semantic RelocKind to the native COFF relocation constant
// for the given architecture.
func peRelocType(arch Arch, k RelocKind) uint16 {
	if arch == AMD64 {
		switch k {
		case RelocCall32:   return pe.RelAMD64Rel32
		case RelocAbs64:    return pe.RelAMD64Addr64
		case RelocAbs32NB:  return pe.RelAMD64Addr32NB
		case RelocPCRel32:  return pe.RelAMD64Rel32
		case RelocSEHUnwind: return pe.RelAMD64Addr32NB
		}
	} else { // ARM64
		switch k {
		case RelocCall32:        return pe.RelARM64Branch26
		case RelocAbs64:         return pe.RelARM64Addr64
		case RelocAbs32NB:       return pe.RelARM64Addr32NB
		case RelocADRP:          return pe.RelARM64PagebaseRel21
		case RelocADRPOff12Add:  return pe.RelARM64Pageoffset12A
		case RelocADRPOff12Load: return pe.RelARM64Pageoffset12L
		case RelocSEHUnwind:     return pe.RelARM64Addr32NB
		}
	}
	panicUnsupportedReloc(arch, k, "PE")
	return 0
}

// ── peBSSSection ──────────────────────────────────────────────────────────────

// peBSSSection wraps the unexported *pe.section to satisfy BSSSection.
type peBSSSection struct {
	inner interface {
		Len() int
		Write([]byte) (int, error)
		WriteByte(byte) error
		GrowBSS(uint32)
	}
}

func (b *peBSSSection) Len() int                    { return b.inner.Len() }
func (b *peBSSSection) Write(p []byte) (int, error) { return b.inner.Write(p) }
func (b *peBSSSection) WriteByte(c byte) error      { return b.inner.WriteByte(c) }
func (b *peBSSSection) Grow(sz uint64)              { b.inner.GrowBSS(uint32(sz)) }