package elf

import "bytes"

// ── Section ───────────────────────────────────────────────────────────────────

// section is one named ELF section within the object being constructed.
// The cpu backend holds a *section directly and calls Write on it.
// The type is unexported; callers receive it through Object accessor methods.
type section struct {
	Name       string
	Type       uint32       // SHTProgbits, SHTNobits, or another SHT_* constant
	Flags      uint64       // SHF_* flags
	Align      uint64       // section alignment in bytes (power of two; 0 or 1 = none)
	buf        bytes.Buffer // raw data for non-nobits sections
	nobitsSize uint64       // reserved byte count for SHT_NOBITS (BSS) sections
}

// isNobits reports whether the section carries no file bytes (e.g. .bss).
func (s *section) isNobits() bool { return s.Type == SHTNobits }

// Len returns the number of bytes written so far (always 0 for nobits sections).
func (s *section) Len() int { return s.buf.Len() }

// Write appends b to the section's data buffer.
func (s *section) Write(b []byte) (int, error) { return s.buf.Write(b) }

// WriteByte appends a single byte.
func (s *section) WriteByte(b byte) error { return s.buf.WriteByte(b) }

// GrowNobits extends the SHT_NOBITS reservation to at least sz bytes.
// Only meaningful for sections created with Type == SHTNobits (i.e. via Bss or
// Section with SHTNobits). Calling Write on a nobits section is an error.
func (s *section) GrowNobits(sz uint64) {
	if sz > s.nobitsSize {
		s.nobitsSize = sz
	}
}

// ── Symbol ────────────────────────────────────────────────────────────────────

// Symbol describes an Elf64_Sym entry to be written into the object.
type Symbol struct {
	Name    string
	Section string // section name; "" = SHN_UNDEF (undefined / imported)
	Offset  uint64 // byte offset within Section (becomes st_value for defined syms)
	Size    uint64 // st_size — 0 is valid

	Global     bool  // STB_GLOBAL — visible to the linker across translation units
	Weak       bool  // STB_WEAK   — globally visible but overridable
	IsFunction bool  // STT_FUNC   — symbol marks executable code
	IsData     bool  // STT_OBJECT — symbol marks a data variable
	Vis        uint8 // STV_* visibility; 0 = STVDefault

	Abs      bool   // SHN_ABS: symbol has an absolute value (Section ignored)
	AbsValue uint64 // st_value for absolute symbols

	// Common block (SHN_COMMON): used for C tentative definitions.
	// The linker resolves multiple commons to the largest, aligned to CommonAlign.
	Common      bool
	CommonAlign uint64 // st_value for SHN_COMMON (alignment, must be power of two)
}

// ── Reloc ─────────────────────────────────────────────────────────────────────

// Reloc describes an Elf64_Rela relocation record.
// ELF uses explicit addends (RELA), so every reloc carries its own Addend.
type Reloc struct {
	Section string // name of the section being patched
	Offset  uint64 // byte offset within that section's data
	Symbol  string // target symbol name (must be resolvable at link time)
	Type    uint32 // RAMD64*, RARM64*, or RRISCV* constant
	Addend  int64  // r_addend: explicit addend (e.g. −4 for PC-relative calls)
}

// ── Object ────────────────────────────────────────────────────────────────────

// Object is an ELF64 ET_REL relocatable object being constructed.
//
// The cpu backend obtains section handles via Text(), Data(), etc., writes
// machine code and data into them, then registers symbols and relocations with
// AddSymbol / AddReloc. Emit() produces a byte slice that is a valid ELF64
// ET_REL file — linker/elf.ParseObject consumes it directly.
type Object struct {
	arch     Arch
	eflags   uint32 // e_flags — required for RISC-V (EF_RISCV_RVC etc.)
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

// SetEFlags sets the ELF e_flags field. Required for RISC-V to encode the ABI
// and extension flags (e.g. EF_RISCV_RVC | EF_RISCV_FLOAT_ABI_DOUBLE).
func (o *Object) SetEFlags(f uint32) { o.eflags = f }

// ── Section accessors ─────────────────────────────────────────────────────────

// getOrAdd returns the existing section with the given name, or creates one
// with the supplied attributes.
func (o *Object) getOrAdd(name string, stype uint32, flags uint64, align uint64) *section {
	if idx, ok := o.secIndex[name]; ok {
		return o.sections[idx]
	}
	if align < 1 {
		align = 1
	}
	s := &section{Name: name, Type: stype, Flags: flags, Align: align}
	o.secIndex[name] = len(o.sections)
	o.sections = append(o.sections, s)
	return s
}

// Text returns the .text section (executable machine code).
func (o *Object) Text() *section {
	return o.getOrAdd(".text", SHTProgbits, SHFAlloc|SHFExecinstr, 16)
}

// Data returns the .data section (read-write initialized data).
func (o *Object) Data() *section {
	return o.getOrAdd(".data", SHTProgbits, SHFAlloc|SHFWrite, 8)
}

// Rodata returns the .rodata section (read-only initialized data).
func (o *Object) Rodata() *section {
	return o.getOrAdd(".rodata", SHTProgbits, SHFAlloc, 8)
}

// Bss returns the .bss section (zero-initialized uninitialized data).
// Use GrowNobits to reserve bytes within it; do not call Write on it.
func (o *Object) Bss() *section {
	return o.getOrAdd(".bss", SHTNobits, SHFAlloc|SHFWrite, 8)
}

// EhFrame returns the .eh_frame section (call-frame information for unwinding).
func (o *Object) EhFrame() *section {
	return o.getOrAdd(".eh_frame", SHTProgbits, SHFAlloc, 8)
}

// Section returns (or creates) an arbitrary named section with the supplied
// type, flags, and alignment. Use this for non-standard sections such as
// .note.*, .debug_*, or custom COMDAT groups.
func (o *Object) Section(name string, stype uint32, flags uint64, align uint64) *section {
	return o.getOrAdd(name, stype, flags, align)
}

// ── Symbol and reloc registration ─────────────────────────────────────────────

// AddSymbol appends sym to the symbol table.
func (o *Object) AddSymbol(sym Symbol) { o.symbols = append(o.symbols, sym) }

// AddReloc appends r to the relocation list.
func (o *Object) AddReloc(r Reloc) { o.relocs = append(o.relocs, r) }

// Emit serialises the object into a valid ELF64 ET_REL byte slice.
// The result can be passed directly to linker/elf.ParseObject.
func (o *Object) Emit() ([]byte, error) { return emit(o) }