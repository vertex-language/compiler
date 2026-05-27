package object

import "github.com/vertex-language/compiler/object/macho"

// ── machoObject ───────────────────────────────────────────────────────────────

type machoObject struct {
	inner *macho.Object
	arch  Arch
	plat  Platform
}

func newMachoObject(arch Arch, plat Platform) Object {
	a := macho.ArchAMD64
	if arch == ARM64 {
		a = macho.ArchARM64
	}
	return &machoObject{inner: macho.NewObject(a), arch: arch, plat: plat}
}

func (o *machoObject) Platform() Platform        { return o.plat }
func (o *machoObject) Arch() Arch                { return o.arch }
func (o *machoObject) Emit() ([]byte, error)     { return o.inner.Emit() }
func (o *machoObject) Text() Section             { return o.inner.Text() }
func (o *machoObject) Data() Section             { return o.inner.Data() }
func (o *machoObject) Rodata() Section           { return o.inner.Rodata() }

func (o *machoObject) Bss() BSSSection {
	// Mach-O is asymmetric: the grow operation lives on the Object, not the
	// section. The wrapper closes over both the section (for Len/Write) and
	// the object + section name (for Grow).
	return &machoBSSSection{
		inner:    o.inner.Bss(),
		obj:      o.inner,
		sectName: "__bss",
	}
}

func (o *machoObject) AddSymbol(sym Symbol) {
	seg, sect := machoSegSect(sym.Section)
	o.inner.AddSymbol(macho.Symbol{
		Name:        sym.Name,
		SegmentName: seg,
		SectionName: sect,
		Offset:      sym.Offset,
		Global:      sym.Global,
		Weak:        sym.Weak,
		Abs:         sym.Abs,
		AbsValue:    sym.AbsValue,
	})
}

func (o *machoObject) AddReloc(r Reloc) {
	nativeSect := machoSectName(r.Section)
	typ, pcrel, length := machoRelocParams(o.arch, r.Kind)
	o.inner.AddReloc(macho.Reloc{
		SectionName: nativeSect,
		Offset:      r.Offset,
		Extern:      true,
		Symbol:      r.Symbol,
		PCRel:       pcrel,
		Length:      length,
		Type:        typ,
	})
}

// machoSegSect converts a canonical section name to the Mach-O
// (segment name, section name) pair.
func machoSegSect(canonical string) (seg, sect string) {
	switch canonical {
	case ".text":
		return "__TEXT", "__text"
	case ".data":
		return "__DATA", "__data"
	case ".rodata":
		return "__TEXT", "__const"
	case ".bss":
		return "__DATA", "__bss"
	default:
		// Pass through for non-canonical sections (e.g. .eh_frame).
		return "", ""
	}
}

// machoSectName converts a canonical section name to the Mach-O section name
// used as the key in relocation entries.
func machoSectName(canonical string) string {
	switch canonical {
	case ".text":
		return "__text"
	case ".data":
		return "__data"
	case ".rodata":
		return "__const"
	case ".bss":
		return "__bss"
	default:
		return canonical
	}
}

// machoRelocParams returns the native Mach-O (type, pcrel, length) triple for
// a semantic RelocKind on the given architecture.
//
// length encodes the field width as log₂(bytes): 0=1B, 1=2B, 2=4B, 3=8B.
func machoRelocParams(arch Arch, k RelocKind) (typ uint8, pcrel bool, length uint8) {
	if arch == AMD64 {
		switch k {
		case RelocCall32:   return macho.RelocAMD64Branch, true, 2   // 4-byte pcrel
		case RelocAbs64:    return macho.RelocAMD64Unsigned, false, 3 // 8-byte absolute
		case RelocPCRel32:  return macho.RelocAMD64Signed, true, 2   // 4-byte pcrel
		case RelocGOTLoad:  return macho.RelocAMD64GotLoad, true, 2  // 4-byte pcrel
		case RelocTLSIE:    return macho.RelocAMD64TLV, true, 2      // 4-byte pcrel
		}
	} else { // ARM64
		switch k {
		case RelocCall32:        return macho.RelocARM64Branch26, true, 2   // 4-byte, pcrel
		case RelocAbs64:         return macho.RelocARM64Unsigned, false, 3  // 8-byte absolute
		case RelocADRP:          return macho.RelocARM64Page21, true, 2     // 4-byte pcrel
		case RelocADRPOff12Add:  return macho.RelocARM64Pageoff12, false, 2 // 4-byte absolute
		case RelocADRPOff12Load: return macho.RelocARM64Pageoff12, false, 2 // 4-byte absolute
		}
	}
	panicUnsupportedReloc(arch, k, "Mach-O")
	return 0, false, 0
}

// ── machoBSSSection ───────────────────────────────────────────────────────────

// machoBSSSection wraps the unexported *macho.section together with a
// reference back to the enclosing Object, because Mach-O BSS growth is
// driven through Object.GrowBss rather than through the section itself.
type machoBSSSection struct {
	inner interface {
		Len() int
		Write([]byte) (int, error)
		WriteByte(byte) error
	}
	obj      *macho.Object
	sectName string // Mach-O section name, e.g. "__bss"
}

func (b *machoBSSSection) Len() int                    { return b.inner.Len() }
func (b *machoBSSSection) Write(p []byte) (int, error) { return b.inner.Write(p) }
func (b *machoBSSSection) WriteByte(c byte) error      { return b.inner.WriteByte(c) }
func (b *machoBSSSection) Grow(sz uint64)              { b.obj.GrowBss(b.sectName, sz) }