package pe

import "bytes"

// ── Section ───────────────────────────────────────────────────────────────────

// section is one named COFF section within the object being constructed.
// The cpu backend holds a *section directly and calls Write on it.
// The type is unexported; callers receive it through Object accessor methods.
type section struct {
	Name       string
	Chars      uint32       // IMAGE_SCN_* flags including alignment bits
	buf        bytes.Buffer // raw data for initialized sections
	isBSS      bool
	nobitsSize uint32 // reserved byte count for BSS (IMAGE_SCN_CNT_UNINITIALIZED_DATA)
}

// Len returns the number of bytes written so far (always 0 for BSS sections).
func (s *section) Len() int { return s.buf.Len() }

// Write appends b to the section data.
func (s *section) Write(b []byte) (int, error) { return s.buf.Write(b) }

// WriteByte appends a single byte.
func (s *section) WriteByte(b byte) error { return s.buf.WriteByte(b) }

// GrowBSS extends the BSS reservation to at least sz bytes.
// Only meaningful for sections created with the IMAGE_SCN_CNT_UNINITIALIZED_DATA
// characteristic (i.e. via Object.Bss or Object.Section with that flag).
func (s *section) GrowBSS(sz uint32) {
	if sz > s.nobitsSize {
		s.nobitsSize = sz
	}
}

// ── Symbol ────────────────────────────────────────────────────────────────────

// Symbol describes a COFF symbol table entry to write into the object.
type Symbol struct {
	Name    string
	Section string // section name; "" = undefined external reference
	Offset  uint32 // byte offset within Section (becomes the COFF symbol Value)

	Global      bool   // IMAGE_SYM_CLASS_EXTERNAL — visible to the linker
	Weak        bool   // IMAGE_SYM_CLASS_WEAK_EXTERNAL
	WeakDefault string // for Weak=true: default-resolution symbol name
	WeakChars   uint32 // IMAGE_WEAK_EXTERN_SEARCH_* (0 → WeakSearchLibrary)

	IsFunction bool   // sets Type = IMAGE_SYM_DTYPE_FUNCTION (0x20)
	Abs        bool   // IMAGE_SYM_ABSOLUTE: SectionNumber = −1
	AbsValue   uint32 // Value for absolute symbols (Abs = true)
}

// ── Reloc ─────────────────────────────────────────────────────────────────────

// Reloc describes a COFF relocation record.
type Reloc struct {
	Section string // name of the section being patched
	Offset  uint32 // byte offset within that section's data
	Symbol  string // target symbol name (must be resolvable at link time)
	Type    uint16 // RelAMD64* or RelARM64* constant
}

// ── Object ────────────────────────────────────────────────────────────────────

// Object is a COFF PE32+ relocatable object file being constructed.
//
// The cpu backend obtains section handles via Text(), Data(), etc., writes
// machine code and data into them, then registers symbols and relocations with
// AddSymbol / AddReloc.  Emit() produces a byte slice that is a valid COFF
// .obj file — linker/pe.ParseObject consumes it directly.
type Object struct {
	arch     Arch
	sections []*section
	secIndex map[string]int // section name → 0-based index in sections
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

// getOrAdd returns the existing section with the given name, or creates one
// with the supplied characteristics.
func (o *Object) getOrAdd(name string, chars uint32) *section {
	if idx, ok := o.secIndex[name]; ok {
		return o.sections[idx]
	}
	s := &section{Name: name, Chars: chars}
	o.secIndex[name] = len(o.sections)
	o.sections = append(o.sections, s)
	return s
}

// Text returns the .text section (executable machine code).
func (o *Object) Text() *section {
	return o.getOrAdd(".text",
		ScnCntCode|ScnMemExecute|ScnMemRead|ScnAlign16)
}

// Data returns the .data section (read-write initialized data).
func (o *Object) Data() *section {
	return o.getOrAdd(".data",
		ScnCntInitialized|ScnMemRead|ScnMemWrite|ScnAlign8)
}

// Rodata returns the .rdata section (read-only initialized data).
func (o *Object) Rodata() *section {
	return o.getOrAdd(".rdata",
		ScnCntInitialized|ScnMemRead|ScnAlign8)
}

// Bss returns the .bss section (zero-initialized uninitialized data).
// Use section.GrowBSS to reserve bytes; do not call Write on this section.
func (o *Object) Bss() *section {
	s := o.getOrAdd(".bss",
		ScnCntUninitialized|ScnMemRead|ScnMemWrite|ScnAlign8)
	s.isBSS = true
	return s
}

// Pdata returns the .pdata section (procedure data / SEH unwind table).
func (o *Object) Pdata() *section {
	return o.getOrAdd(".pdata",
		ScnCntInitialized|ScnMemRead|ScnAlign4)
}

// Xdata returns the .xdata section (SEH unwind info).
func (o *Object) Xdata() *section {
	return o.getOrAdd(".xdata",
		ScnCntInitialized|ScnMemRead|ScnAlign4)
}

// Drectve returns the .drectve section for linker directive strings.
//
// Write space-separated directives directly, e.g.:
//
//	obj.Drectve().Write([]byte(" -defaultlib:libcmt"))
//
// See also AddExportDirective for a typed helper.
func (o *Object) Drectve() *section {
	return o.getOrAdd(".drectve", ScnLnkInfo|ScnLnkRemove|ScnAlign1)
}

// Section returns (or creates) an arbitrary named section with the supplied
// characteristics.
func (o *Object) Section(name string, chars uint32) *section {
	return o.getOrAdd(name, chars)
}

// ── Directive helpers ─────────────────────────────────────────────────────────

// AddExportDirective appends a -export: linker directive to the .drectve
// section.  If exportName is empty or equals internalName, only internalName
// is emitted; otherwise the form internalName=exportName is used.
func (o *Object) AddExportDirective(internalName, exportName string, isData bool) {
	arg := internalName
	if exportName != "" && exportName != internalName {
		arg += "=" + exportName
	}
	if isData {
		arg += ",data"
	}
	d := o.Drectve()
	d.buf.WriteString(" -export:")
	d.buf.WriteString(arg)
}

// AddDefaultLib appends a -defaultlib: linker directive.
func (o *Object) AddDefaultLib(lib string) {
	d := o.Drectve()
	d.buf.WriteString(" -defaultlib:")
	d.buf.WriteString(lib)
}

// ── Symbol and reloc registration ─────────────────────────────────────────────

// AddSymbol appends sym to the symbol table.
func (o *Object) AddSymbol(sym Symbol) { o.symbols = append(o.symbols, sym) }

// AddReloc appends r to the relocation list.
func (o *Object) AddReloc(r Reloc) { o.relocs = append(o.relocs, r) }

// Emit serialises the object into a valid COFF .obj byte slice.
// The result can be passed directly to linker/pe.ParseObject.
func (o *Object) Emit() ([]byte, error) { return emit(o) }