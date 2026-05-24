package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Fixed ELF64 structure sizes (bytes).
const (
	elfHeaderSize = 64
	phdrEntrySize = 56
	shdrEntrySize = 64
	symEntrySize  = 24
	relaEntrySize = 24
	dynEntrySize  = 16
)

// Default load address for position-dependent executables on Linux.
const defaultBase uint64 = 0x400000

// pageSize is the minimum segment alignment for LOAD segments.
const pageSize uint64 = 0x1000

// Arch identifies the target machine architecture written into e_machine.
type Arch uint16

const (
	ArchAMD64   Arch = EM_X86_64  // 0x3E
	ArchARM64   Arch = EM_AARCH64 // 0xB7
	ArchRISCV64 Arch = EM_RISCV   // 0xF3
)

// Builder accumulates sections, symbols, and relocations and serializes them
// into a valid ELF64 binary via Emit.
type Builder struct {
	arch     Arch
	fileType uint16 // ET_EXEC or ET_DYN
	entry    string

	sections []Section
	symbols  []Symbol
	relocs   []Reloc

	// Dynamic linking fields.
	interp string   // PT_INTERP path
	needed []string // DT_NEEDED library names
	soname string   // DT_SONAME
	rpath  string   // DT_RUNPATH
}

// NewBuilder returns a Builder targeting arch. The default output type is a
// static position-dependent executable (ET_EXEC).
func NewBuilder(arch Arch) *Builder {
	return &Builder{arch: arch, fileType: ET_EXEC}
}

// SetShared switches the output type to a shared library (ET_DYN).
func (b *Builder) SetShared() { b.fileType = ET_DYN }

// SetInterp sets the dynamic-linker path and implies a PT_INTERP segment.
// Typical value: "/lib64/ld-linux-x86-64.so.2".
func (b *Builder) SetInterp(path string) { b.interp = path }

// AddNeeded records a DT_NEEDED dependency on lib (e.g. "libc.so.6").
func (b *Builder) AddNeeded(lib string) { b.needed = append(b.needed, lib) }

// SetSoname sets the shared-library DT_SONAME tag.
func (b *Builder) SetSoname(name string) { b.soname = name }

// SetRpath sets the DT_RUNPATH runtime search path.
func (b *Builder) SetRpath(path string) { b.rpath = path }

// AddSection appends a section. Sections are laid out in the order they are
// added; allocatable sections (SHF_ALLOC) are automatically grouped into
// PT_LOAD segments by permission flags.
func (b *Builder) AddSection(s Section) { b.sections = append(b.sections, s) }

// AddSymbol records a symbol for the .symtab section.
func (b *Builder) AddSymbol(s Symbol) { b.symbols = append(b.symbols, s) }

// AddReloc records a RELA relocation for the named section. The emitter
// collects all relocations for a section and emits a .rela<name> section.
func (b *Builder) AddReloc(r Reloc) { b.relocs = append(b.relocs, r) }

// SetEntry names the entry-point symbol. Emit returns an error if the symbol
// cannot be resolved to a virtual address.
func (b *Builder) SetEntry(name string) { b.entry = name }

// Emit serializes the binary and returns its raw bytes. The caller is
// responsible for writing the result to a file with executable permissions.
func (b *Builder) Emit() ([]byte, error) {
	em := &emitter{b: b}
	em.secByName = make(map[string]*builtSection)
	em.symAddr = make(map[string]uint64)
	return em.emit()
}

// ── internal emitter ─────────────────────────────────────────────────────────

// builtSection is the mutable in-flight representation of a single ELF section
// throughout the layout and serialization passes.
type builtSection struct {
	name    string
	shType  uint32
	flags   uint64
	data    []byte  // file image; nil/empty for SHT_NOBITS
	memSize uint64  // in-memory size (equals len(data) unless SHT_NOBITS)
	align   uint64  // sh_addralign
	link    uint32
	info    uint32
	entSize uint64
	// Populated by layoutSections:
	fileOff uint64
	addr    uint64
	// Index in the section header table (assigned by addSec).
	shIdx int
}

type emitter struct {
	b        *Builder
	secs     []*builtSection
	secByName map[string]*builtSection
	shstrtab  strTab
	strtab    strTab
	symAddr   map[string]uint64 // symbol name → resolved virtual address
}

func (e *emitter) addSec(sec *builtSection) {
	sec.shIdx = len(e.secs)
	e.secs = append(e.secs, sec)
	if sec.name != "" {
		e.secByName[sec.name] = sec
	}
}

// ── emit orchestrates all passes ─────────────────────────────────────────────

func (e *emitter) emit() ([]byte, error) {
	// ── Pass 1: collect sections ──────────────────────────────────────────

	// Index 0: mandatory null section.
	e.addSec(&builtSection{shType: SHT_NULL, align: 1})

	// User-provided sections.
	for _, s := range e.b.sections {
		align := s.Align
		if align == 0 {
			align = 1
		}
		memSz := uint64(len(s.Data))
		if s.Type == SHT_NOBITS && s.Size > memSz {
			memSz = s.Size
		}
		e.addSec(&builtSection{
			name:    s.Name,
			shType:  s.Type,
			flags:   s.Flags,
			data:    s.Data,
			memSize: memSz,
			align:   align,
			link:    s.Link,
			info:    s.Info,
			entSize: s.EntSize,
		})
	}

	// Group caller relocations by target section name.
	relaMap := make(map[string][]Reloc)
	for _, r := range e.b.relocs {
		relaMap[r.Section] = append(relaMap[r.Section], r)
	}

	// Placeholder .rela.* sections (data filled after symbol resolution).
	type relaEntry struct {
		targetName string
		bs         *builtSection
	}
	var relaEntries []relaEntry
	for targetName := range relaMap {
		bs := &builtSection{
			name:    ".rela" + targetName,
			shType:  SHT_RELA,
			flags:   SHF_INFO_LINK,
			align:   8,
			entSize: relaEntrySize,
		}
		e.addSec(bs)
		relaEntries = append(relaEntries, relaEntry{targetName, bs})
	}

	// Dynamic sections (when dynamic linking is requested).
	hasDynamic := e.b.interp != "" || len(e.b.needed) > 0 ||
		e.b.soname != "" || e.b.rpath != "" || e.b.fileType == ET_DYN
	var dynSec *builtSection
	if hasDynamic {
		if e.b.interp != "" {
			interpData := append([]byte(e.b.interp), 0)
			e.addSec(&builtSection{
				name:    ".interp",
				shType:  SHT_PROGBITS,
				flags:   SHF_ALLOC,
				data:    interpData,
				memSize: uint64(len(interpData)),
				align:   1,
			})
		}
		// .dynstr: content built in Pass 3.
		e.addSec(&builtSection{
			name:   ".dynstr",
			shType: SHT_STRTAB,
			flags:  SHF_ALLOC,
			align:  1,
		})
		// .dynsym: minimal — just a null entry for now.
		// Full dynamic symbol population is handled via DynBuilder (dynamic.go).
		e.addSec(&builtSection{
			name:    ".dynsym",
			shType:  SHT_DYNSYM,
			flags:   SHF_ALLOC,
			align:   8,
			entSize: symEntrySize,
			info:    1, // sh_info = index of first non-local symbol
		})
		// .dynamic: content built in Pass 3.
		dynSec = &builtSection{
			name:    ".dynamic",
			shType:  SHT_DYNAMIC,
			flags:   SHF_ALLOC | SHF_WRITE,
			align:   8,
			entSize: dynEntrySize,
		}
		e.addSec(dynSec)
	}

	// Non-allocatable metadata sections.
	symtabSec := &builtSection{
		name:    ".symtab",
		shType:  SHT_SYMTAB,
		align:   8,
		entSize: symEntrySize,
	}
	strtabSec := &builtSection{
		name:   ".strtab",
		shType: SHT_STRTAB,
		align:  1,
	}
	shstrtabSec := &builtSection{
		name:   ".shstrtab",
		shType: SHT_STRTAB,
		align:  1,
	}
	e.addSec(symtabSec)
	e.addSec(strtabSec)
	e.addSec(shstrtabSec)

	// ── Pass 2: build .shstrtab ────────────────────────────────────────────
	e.shstrtab.add("") // index 0 must be the empty string
	for _, sec := range e.secs {
		if sec.name != "" {
			e.shstrtab.add(sec.name)
		}
	}
	shstrtabSec.data = e.shstrtab.bytes()
	shstrtabSec.memSize = uint64(len(shstrtabSec.data))

	// ── Pass 3: build .symtab, .strtab ────────────────────────────────────
	e.strtab.add("") // index 0 = empty string
	var localSyms, globalSyms []Symbol
	for _, sym := range e.b.symbols {
		if sym.Weak || sym.Global {
			globalSyms = append(globalSyms, sym)
		} else {
			localSyms = append(localSyms, sym)
		}
	}
	firstGlobal := 1 + len(localSyms) // +1 for null entry

	var symBuf bytes.Buffer
	symBuf.Write(make([]byte, symEntrySize)) // null entry
	for _, sym := range append(localSyms, globalSyms...) {
		e.appendSym(&symBuf, sym)
	}
	symtabSec.data = symBuf.Bytes()
	symtabSec.memSize = uint64(len(symtabSec.data))
	symtabSec.link = uint32(strtabSec.shIdx)
	symtabSec.info = uint32(firstGlobal)

	strtabSec.data = e.strtab.bytes()
	strtabSec.memSize = uint64(len(strtabSec.data))

	// ── Pass 4: build dynamic sections ────────────────────────────────────
	if hasDynamic {
		e.buildDynamicSections(dynSec)
	}

	// ── Pass 5: layout — assign file offsets and virtual addresses ─────────
	//
	// Estimate phdr count before layout so the header area size is known.
	// (We over-estimate by 1 and trim if needed — simpler than two sub-passes.)
	estimatedPhdrs := e.estimatePhdrs(hasDynamic)
	headerArea := uint64(elfHeaderSize) + uint64(estimatedPhdrs)*phdrEntrySize

	e.layoutSections(headerArea)

	// ── Pass 6: resolve symbol virtual addresses ───────────────────────────
	for _, sym := range e.b.symbols {
		switch sym.Section {
		case "", "*ABS*":
			if sym.Section == "*ABS*" {
				e.symAddr[sym.Name] = sym.Offset
			}
		default:
			if sec, ok := e.secByName[sym.Section]; ok {
				e.symAddr[sym.Name] = sec.addr + sym.Offset
			}
		}
	}

	// ── Pass 7: patch .symtab values with resolved addresses ──────────────
	// Rebuild now that virtual addresses are known.
	symBuf.Reset()
	symBuf.Write(make([]byte, symEntrySize))
	for _, sym := range append(localSyms, globalSyms...) {
		e.appendSym(&symBuf, sym)
	}
	symtabSec.data = symBuf.Bytes()
	symtabSec.memSize = uint64(len(symtabSec.data))

	// Re-layout to pick up any size changes (symtab is non-alloc so only
	// file offsets of later sections shift — shstrtab, shdrs).
	e.layoutSections(headerArea)

	// ── Pass 8: build .rela.* section data ────────────────────────────────
	symNameIdx := e.buildSymNameIdx(localSyms, globalSyms)
	for _, re := range relaEntries {
		relocs := relaMap[re.targetName]
		if sec := e.secByName[re.targetName]; sec != nil {
			re.bs.info = uint32(sec.shIdx)
		}
		re.bs.link = uint32(symtabSec.shIdx)
		re.bs.data = e.buildRelaData(relocs, symNameIdx)
		re.bs.memSize = uint64(len(re.bs.data))
	}

	// ── Pass 9: resolve entry point ───────────────────────────────────────
	var entryAddr uint64
	if e.b.entry != "" {
		addr, ok := e.symAddr[e.b.entry]
		if !ok {
			return nil, fmt.Errorf("elf: entry symbol %q not found", e.b.entry)
		}
		entryAddr = addr
	}

	// ── Pass 10: build program headers ────────────────────────────────────
	phdrs := e.buildPhdrs(hasDynamic)
	// If our estimate was off, re-layout with the exact count.
	if len(phdrs) != estimatedPhdrs {
		headerArea = uint64(elfHeaderSize) + uint64(len(phdrs))*phdrEntrySize
		e.layoutSections(headerArea)
		phdrs = e.buildPhdrs(hasDynamic)
	}

	// ── Pass 11: section header table offset ──────────────────────────────
	var maxFileOff uint64
	for _, sec := range e.secs {
		if sec.shType == SHT_NOBITS || sec.shType == SHT_NULL {
			continue
		}
		end := sec.fileOff + uint64(len(sec.data))
		if end > maxFileOff {
			maxFileOff = end
		}
	}
	shoff := alignUp(maxFileOff, 8)

	// ── Pass 12: serialize ─────────────────────────────────────────────────
	fileSize := shoff + uint64(len(e.secs))*shdrEntrySize
	buf := make([]byte, fileSize)

	e.writeEhdr(buf, entryAddr, uint64(len(phdrs)), shoff,
		uint16(len(e.secs)), uint16(shstrtabSec.shIdx))

	for i, ph := range phdrs {
		off := elfHeaderSize + i*phdrEntrySize
		e.writePhdr(buf[off:], ph)
	}

	for _, sec := range e.secs {
		if sec.shType == SHT_NULL || sec.shType == SHT_NOBITS || len(sec.data) == 0 {
			continue
		}
		copy(buf[sec.fileOff:], sec.data)
	}

	for i, sec := range e.secs {
		off := int(shoff) + i*shdrEntrySize
		e.writeShdr(buf[off:], sec)
	}

	return buf, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// appendSym serializes one Elf64_Sym into w using the current symbol address map.
func (e *emitter) appendSym(w *bytes.Buffer, sym Symbol) {
	nameIdx := e.strtab.add(sym.Name)

	var binding uint8
	switch {
	case sym.Weak:
		binding = STB_WEAK
	case sym.Global:
		binding = STB_GLOBAL
	default:
		binding = STB_LOCAL
	}
	stInfo := (binding << 4) | (sym.Type & 0x0F)

	var shndx uint16
	var value uint64
	switch sym.Section {
	case "":
		shndx = SHN_UNDEF
	case "*ABS*":
		shndx = SHN_ABS
		value = sym.Offset
	default:
		if sec, ok := e.secByName[sym.Section]; ok {
			shndx = uint16(sec.shIdx)
			value = sec.addr + sym.Offset
		}
	}

	// Override value from resolved address table (populated after layout).
	if a, ok := e.symAddr[sym.Name]; ok {
		value = a
	}

	// Elf64_Sym layout:
	//   uint32 st_name  (4)
	//   uint8  st_info  (1)
	//   uint8  st_other (1)
	//   uint16 st_shndx (2)
	//   uint64 st_value (8)
	//   uint64 st_size  (8)
	var b [symEntrySize]byte
	putU32le(b[0:], nameIdx)
	b[4] = stInfo
	b[5] = sym.Vis & 0x03
	putU16le(b[6:], shndx)
	putU64le(b[8:], value)
	putU64le(b[16:], sym.Size)
	w.Write(b[:])
}

// buildSymNameIdx maps symbol names to their 1-based symtab indices
// (index 0 is the null entry).
func (e *emitter) buildSymNameIdx(locals, globals []Symbol) map[string]uint32 {
	m := make(map[string]uint32, len(locals)+len(globals))
	idx := uint32(1)
	for _, sym := range append(locals, globals...) {
		m[sym.Name] = idx
		idx++
	}
	return m
}

// buildRelaData serializes a slice of Reloc into a .rela section byte slice.
func (e *emitter) buildRelaData(relocs []Reloc, symNameIdx map[string]uint32) []byte {
	buf := make([]byte, len(relocs)*relaEntrySize)
	for i, r := range relocs {
		symIdx := symNameIdx[r.Symbol]
		info := (uint64(symIdx) << 32) | uint64(r.Type)
		off := i * relaEntrySize
		putU64le(buf[off:], r.Offset)
		putU64le(buf[off+8:], info)
		putI64le(buf[off+16:], r.Addend)
	}
	return buf
}

// layoutSections assigns fileOff and addr to every section given that section
// data begins at headerArea bytes into the file.
func (e *emitter) layoutSections(headerArea uint64) {
	base := uint64(0)
	if e.b.fileType == ET_EXEC {
		base = defaultBase
	}
	offset := headerArea
	for _, sec := range e.secs {
		if sec.shType == SHT_NULL {
			sec.fileOff, sec.addr = 0, 0
			continue
		}
		if sec.shType == SHT_NOBITS {
			offset = alignUp(offset, sec.align)
			sec.fileOff = offset
			if sec.flags&SHF_ALLOC != 0 {
				sec.addr = base + offset
			} else {
				sec.addr = 0
			}
			// NOBITS consumes no file space.
			continue
		}
		dataLen := uint64(len(sec.data))
		if dataLen == 0 {
			sec.fileOff = offset
			if sec.flags&SHF_ALLOC != 0 {
				sec.addr = base + offset
			} else {
				sec.addr = 0
			}
			continue
		}
		offset = alignUp(offset, sec.align)
		sec.fileOff = offset
		if sec.flags&SHF_ALLOC != 0 {
			sec.addr = base + offset
		} else {
			sec.addr = 0
		}
		offset += dataLen
	}
}

// estimatePhdrs returns the approximate number of program headers.
func (e *emitter) estimatePhdrs(hasDynamic bool) int {
	seen := make(map[uint32]bool)
	for _, sec := range e.secs {
		if sec.flags&SHF_ALLOC != 0 && sec.shType != SHT_NULL {
			seen[segPermKey(sec.flags)] = true
		}
	}
	n := len(seen) // PT_LOAD segments
	n++             // PT_PHDR
	if e.b.interp != "" {
		n++
	}
	if hasDynamic {
		n++
	}
	n++ // PT_GNU_STACK
	return n
}

// buildPhdrs constructs the final slice of program header descriptors.
func (e *emitter) buildPhdrs(hasDynamic bool) []phdrDesc {
	var phs []phdrDesc
	base := uint64(0)
	if e.b.fileType == ET_EXEC {
		base = defaultBase
	}
	nPhdrs := e.estimatePhdrs(hasDynamic)
	phdrFileOff := uint64(elfHeaderSize)
	phdrFileSize := uint64(nPhdrs) * phdrEntrySize

	// PT_PHDR
	phs = append(phs, phdrDesc{
		pType:  PT_PHDR,
		flags:  PF_R,
		off:    phdrFileOff,
		vaddr:  base + phdrFileOff,
		paddr:  base + phdrFileOff,
		filesz: phdrFileSize,
		memsz:  phdrFileSize,
		align:  8,
	})

	// PT_INTERP
	if sec := e.secByName[".interp"]; sec != nil {
		sz := uint64(len(sec.data))
		phs = append(phs, phdrDesc{
			pType:  PT_INTERP,
			flags:  PF_R,
			off:    sec.fileOff,
			vaddr:  sec.addr,
			paddr:  sec.addr,
			filesz: sz,
			memsz:  sz,
			align:  1,
		})
	}

	// PT_LOAD: one segment per distinct permission group, in standard order.
	for _, permFlags := range []uint32{PF_R | PF_X, PF_R, PF_R | PF_W} {
		var groupSecs []*builtSection
		for _, sec := range e.secs {
			if sec.flags&SHF_ALLOC == 0 || sec.shType == SHT_NULL {
				continue
			}
			if segPermKey(sec.flags) == permFlags {
				groupSecs = append(groupSecs, sec)
			}
		}
		if len(groupSecs) == 0 {
			continue
		}
		first := groupSecs[0]
		var fileEnd, memEnd uint64
		for _, s := range groupSecs {
			if s.shType == SHT_NOBITS {
				me := s.fileOff + s.memSize
				if me > memEnd {
					memEnd = me
				}
			} else {
				fe := s.fileOff + uint64(len(s.data))
				if fe > fileEnd {
					fileEnd = fe
				}
				if fe > memEnd {
					memEnd = fe
				}
			}
		}
		phs = append(phs, phdrDesc{
			pType:  PT_LOAD,
			flags:  permFlags,
			off:    first.fileOff,
			vaddr:  first.addr,
			paddr:  first.addr,
			filesz: fileEnd - first.fileOff,
			memsz:  memEnd - first.fileOff,
			align:  pageSize,
		})
	}

	// PT_DYNAMIC
	if hasDynamic {
		if sec := e.secByName[".dynamic"]; sec != nil {
			sz := uint64(len(sec.data))
			phs = append(phs, phdrDesc{
				pType:  PT_DYNAMIC,
				flags:  PF_R | PF_W,
				off:    sec.fileOff,
				vaddr:  sec.addr,
				paddr:  sec.addr,
				filesz: sz,
				memsz:  sz,
				align:  8,
			})
		}
	}

	// PT_GNU_STACK — marks the stack non-executable. No file content.
	phs = append(phs, phdrDesc{
		pType: PT_GNU_STACK,
		flags: PF_R | PF_W,
		align: pageSize,
	})

	return phs
}

// ── binary serialization ──────────────────────────────────────────────────────

type phdrDesc struct {
	pType  uint32
	flags  uint32
	off    uint64
	vaddr  uint64
	paddr  uint64
	filesz uint64
	memsz  uint64
	align  uint64
}

func (e *emitter) writeEhdr(buf []byte, entry, phnum, shoff uint64, shnum, shstrndx uint16) {
	// e_ident
	buf[EI_MAG0] = ELFMAG0
	buf[EI_MAG1] = ELFMAG1
	buf[EI_MAG2] = ELFMAG2
	buf[EI_MAG3] = ELFMAG3
	buf[EI_CLASS] = ELFCLASS64
	buf[EI_DATA] = ELFDATA2LSB
	buf[EI_VERSION] = EV_CURRENT
	buf[EI_OSABI] = ELFOSABI_NONE
	// bytes 8-15: padding (zero)

	putU16le(buf[16:], uint16(e.b.fileType)) // e_type
	putU16le(buf[18:], uint16(e.b.arch))     // e_machine
	putU32le(buf[20:], EV_CURRENT)            // e_version
	putU64le(buf[24:], entry)                 // e_entry
	putU64le(buf[32:], elfHeaderSize)         // e_phoff
	putU64le(buf[40:], shoff)                 // e_shoff
	putU32le(buf[48:], 0)                     // e_flags
	putU16le(buf[52:], elfHeaderSize)         // e_ehsize
	putU16le(buf[54:], phdrEntrySize)         // e_phentsize
	putU16le(buf[56:], uint16(phnum))         // e_phnum
	putU16le(buf[58:], shdrEntrySize)         // e_shentsize
	putU16le(buf[60:], shnum)                 // e_shnum
	putU16le(buf[62:], shstrndx)              // e_shstrndx
}

func (e *emitter) writePhdr(buf []byte, ph phdrDesc) {
	// Elf64_Phdr layout (note: p_flags is at offset 4, before p_offset):
	//   uint32 p_type   (0)
	//   uint32 p_flags  (4)
	//   uint64 p_offset (8)
	//   uint64 p_vaddr  (16)
	//   uint64 p_paddr  (24)
	//   uint64 p_filesz (32)
	//   uint64 p_memsz  (40)
	//   uint64 p_align  (48)
	putU32le(buf[0:], ph.pType)
	putU32le(buf[4:], ph.flags)
	putU64le(buf[8:], ph.off)
	putU64le(buf[16:], ph.vaddr)
	putU64le(buf[24:], ph.paddr)
	putU64le(buf[32:], ph.filesz)
	putU64le(buf[40:], ph.memsz)
	putU64le(buf[48:], ph.align)
}

func (e *emitter) writeShdr(buf []byte, sec *builtSection) {
	nameIdx := uint32(0)
	if sec.name != "" {
		nameIdx = e.shstrtab.index(sec.name)
	}
	align := sec.align
	if align == 0 {
		align = 1
	}
	sz := uint64(len(sec.data))
	if sec.shType == SHT_NOBITS {
		sz = sec.memSize
	}

	// Elf64_Shdr layout:
	//   uint32  sh_name      (0)
	//   uint32  sh_type      (4)
	//   uint64  sh_flags     (8)
	//   uint64  sh_addr      (16)
	//   uint64  sh_offset    (24)
	//   uint64  sh_size      (32)
	//   uint32  sh_link      (40)
	//   uint32  sh_info      (44)
	//   uint64  sh_addralign (48)
	//   uint64  sh_entsize   (56)
	putU32le(buf[0:], nameIdx)
	putU32le(buf[4:], sec.shType)
	putU64le(buf[8:], sec.flags)
	putU64le(buf[16:], sec.addr)
	putU64le(buf[24:], sec.fileOff)
	putU64le(buf[32:], sz)
	putU32le(buf[40:], sec.link)
	putU32le(buf[44:], sec.info)
	putU64le(buf[48:], align)
	putU64le(buf[56:], sec.entSize)
}

// ── strTab ────────────────────────────────────────────────────────────────────

// strTab is a compact ELF string table: null-terminated strings concatenated,
// with deduplication so identical strings share a single entry.
type strTab struct {
	data    []byte
	indices map[string]uint32
}

// add interns s and returns its byte offset into the table.
func (t *strTab) add(s string) uint32 {
	if t.indices == nil {
		t.indices = make(map[string]uint32)
	}
	if idx, ok := t.indices[s]; ok {
		return idx
	}
	idx := uint32(len(t.data))
	t.indices[s] = idx
	t.data = append(t.data, s...)
	t.data = append(t.data, 0)
	return idx
}

// index returns the previously added offset for s, or 0 if not present.
func (t *strTab) index(s string) uint32 {
	if t.indices == nil {
		return 0
	}
	return t.indices[s]
}

func (t *strTab) bytes() []byte { return t.data }

// ── little-endian write helpers ───────────────────────────────────────────────

func putU16le(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func putU32le(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64le(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
func putI64le(b []byte, v int64)  { binary.LittleEndian.PutUint64(b, uint64(v)) }

// alignUp rounds x up to the next multiple of align (which must be a power of 2).
func alignUp(x, align uint64) uint64 {
	if align <= 1 {
		return x
	}
	return (x + align - 1) &^ (align - 1)
}

// segPermKey maps a section's sh_flags to a canonical PF_* key used to group
// sections into PT_LOAD segments with matching permissions.
func segPermKey(shFlags uint64) uint32 {
	f := uint32(PF_R)
	if shFlags&SHF_WRITE != 0 {
		f |= PF_W
	}
	if shFlags&SHF_EXECINSTR != 0 {
		f |= PF_X
	}
	return f
}