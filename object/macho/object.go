// object/macho/object.go

package macho

import "bytes"

// ── Section ───────────────────────────────────────────────────────────────────

// section is a named section within the MH_OBJECT being constructed.
// The cpu backend holds a *section directly and calls Write on it.
// The type is unexported; callers receive it through Object accessor methods.
type section struct {
	SegName  string
	SectName string
	Type     uint32 // section type  (low byte of section flags)
	Attrs    uint32 // section attrs (high bytes of section flags)
	Align    uint64 // alignment in bytes (power of two; 1 = none)
	buf      bytes.Buffer
	nobits   uint64 // reserved size for zerofill sections
}

// Len returns the number of bytes written to a non-zerofill section so far.
func (s *section) Len() int { return s.buf.Len() }

// Write appends b to the section's data buffer.
func (s *section) Write(b []byte) (int, error) { return s.buf.Write(b) }

// WriteByte appends a single byte.
func (s *section) WriteByte(b byte) error { return s.buf.WriteByte(b) }

// isZerofill reports whether s carries no file bytes (STypeZerofill et al.).
func isZerofill(s *section) bool { return s.Type == STypeZerofill }

// ── Symbol ────────────────────────────────────────────────────────────────────

// Symbol describes an nlist_64 entry to be written into the object.
type Symbol struct {
	Name        string
	SegmentName string // e.g. "__TEXT"; empty for undefined or absolute
	SectionName string // e.g. "__text"; empty for undefined or absolute
	Offset      uint64 // byte offset within the section (becomes n_value for N_SECT)
	Size        uint64 // informational; not encoded in nlist_64
	Global      bool   // N_EXT: visible outside this translation unit
	Weak        bool   // N_WEAK_DEF (defined) or N_WEAK_REF (undefined)
	Abs         bool   // N_ABS: absolute symbol (SectionName ignored)
	AbsValue    uint64 // n_value for absolute symbols
}

// ── Reloc ─────────────────────────────────────────────────────────────────────

// Reloc describes a Mach-O relocation_info entry.
//
// Exactly one of Extern or !Extern is used per entry:
//   - Extern == true  → symbol-relative reloc; Symbol names the target.
//   - Extern == false → section-relative reloc; SectOrdinal is the 1-based
//     target section number within the same object file.
type Reloc struct {
	SectionName string // section being patched (e.g. "__text")
	Offset      uint32 // byte offset within that section
	Extern      bool   // symbol-relative vs section-relative
	Symbol      string // Extern==true:  target symbol name
	SectOrdinal uint32 // Extern==false: 1-based target section number
	PCRel       bool   // r_pcrel
	Length      uint8  // 0=1B  1=2B  2=4B  3=8B
	Type        uint8  // RelocAMD64* or RelocARM64*
}

// ── Object ────────────────────────────────────────────────────────────────────

// Object is a Mach-O MH_OBJECT relocatable object being constructed.
//
// The cpu backend obtains section handles via Text(), Data(), etc., writes
// machine code and data into them, then registers symbols and relocations with
// AddSymbol / AddReloc.  Emit() produces a byte slice that is a valid
// MH_OBJECT file — linker/macho.ParseObject consumes it directly.
type Object struct {
	arch     Arch
	sections []*section
	secIndex map[string]int // SectName → 0-based index in sections
	symbols  []Symbol
	relocs   []Reloc
}

// NewObject returns an empty Object for the given architecture.
func NewObject(arch Arch) *Object {
	return &Object{
		arch:     arch,
		secIndex: make(map[string]int),
	}
}

// ── Section accessors ─────────────────────────────────────────────────────────

// getOrAdd returns the existing section with the given SectName, or creates a
// new one with the supplied attributes.
func (o *Object) getOrAdd(segName, sectName string, typ, attrs uint32, align uint64) *section {
	if idx, ok := o.secIndex[sectName]; ok {
		return o.sections[idx]
	}
	if align < 1 {
		align = 1
	}
	s := &section{
		SegName:  segName,
		SectName: sectName,
		Type:     typ,
		Attrs:    attrs,
		Align:    align,
	}
	o.secIndex[sectName] = len(o.sections)
	o.sections = append(o.sections, s)
	return s
}

// Text returns the __TEXT,__text executable section.
func (o *Object) Text() *section {
	return o.getOrAdd("__TEXT", "__text",
		STypeRegular, SAttrPureInstructions|SAttrSomeInstructions, 4)
}

// Data returns the __DATA,__data read-write section.
func (o *Object) Data() *section {
	return o.getOrAdd("__DATA", "__data", STypeRegular, 0, 8)
}

// Rodata returns the __TEXT,__const read-only data section.
func (o *Object) Rodata() *section {
	return o.getOrAdd("__TEXT", "__const", STypeRegular, 0, 8)
}

// Bss returns the __DATA,__bss zerofill section.
// Use GrowBss to reserve bytes within it; do not call Write on it.
func (o *Object) Bss() *section {
	return o.getOrAdd("__DATA", "__bss", STypeZerofill, 0, 8)
}

// Section returns (or creates) an arbitrary section.
func (o *Object) Section(segName, sectName string, typ, attrs uint32, align uint64) *section {
	return o.getOrAdd(segName, sectName, typ, attrs, align)
}

// GrowBss extends the zerofill reservation of sectName to at least sz bytes.
func (o *Object) GrowBss(sectName string, sz uint64) {
	if idx, ok := o.secIndex[sectName]; ok {
		if sz > o.sections[idx].nobits {
			o.sections[idx].nobits = sz
		}
	}
}

// ── Symbol and reloc registration ─────────────────────────────────────────────

// AddSymbol appends sym to the symbol table.
func (o *Object) AddSymbol(sym Symbol) { o.symbols = append(o.symbols, sym) }

// AddReloc appends r to the relocation list.
func (o *Object) AddReloc(r Reloc) { o.relocs = append(o.relocs, r) }

// Emit serialises the object into a valid MH_OBJECT byte slice.
// The result can be passed directly to linker/macho.ParseObject.
func (o *Object) Emit() ([]byte, error) { return emit(o) }