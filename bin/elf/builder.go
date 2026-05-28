// builder.go
package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	elfHeaderSize = 64
	phdrEntrySize = 56
	shdrEntrySize = 64
	symEntrySize  = 24
	relaEntrySize = 24
	dynEntrySize  = 16
)

const defaultBase uint64 = 0x400000
const pageSize uint64 = 0x1000

type Arch uint16

const (
	ArchAMD64   Arch = EM_X86_64
	ArchARM64   Arch = EM_AARCH64
	ArchRISCV64 Arch = EM_RISCV
)

// Builder accumulates sections, symbols, and relocations and serializes them
// into a valid ELF64 binary via Emit.
type Builder struct {
	arch     Arch
	fileType uint16
	flags    uint32
	entry    string

	sections      []Section
	symbols       []Symbol
	relocs        []Reloc
	extraSegments []Segment

	interp  string
	needed  []string
	soname  string
	rpath   string
	dynSyms []string // PLT symbol names for .dynsym, in stub order
}

func NewBuilder(arch Arch) *Builder {
	return &Builder{arch: arch, fileType: ET_EXEC}
}

func (b *Builder) SetShared()              { b.fileType = ET_DYN }
func (b *Builder) SetFlags(f uint32)       { b.flags = f }
func (b *Builder) SetInterp(path string)   { b.interp = path }
func (b *Builder) AddNeeded(lib string)    { b.needed = append(b.needed, lib) }
func (b *Builder) SetSoname(name string)   { b.soname = name }
func (b *Builder) SetRpath(path string)    { b.rpath = path }
func (b *Builder) AddSection(s Section)   { b.sections = append(b.sections, s) }
func (b *Builder) AddSymbol(s Symbol)     { b.symbols = append(b.symbols, s) }
func (b *Builder) AddReloc(r Reloc)       { b.relocs = append(b.relocs, r) }
func (b *Builder) SetEntry(name string)   { b.entry = name }

// AddDynSym registers name as a global undefined symbol in .dynsym.
// Call once per PLT symbol in stub order; the resulting .dynsym indices must
// match the symIdx values encoded in .rela.plt (entry i → symIdx i+1).
func (b *Builder) AddDynSym(name string) { b.dynSyms = append(b.dynSyms, name) }

// Emit serializes the binary and returns its raw bytes.
func (b *Builder) Emit() ([]byte, error) {
	em := &emitter{b: b}
	em.secByName = make(map[string]*builtSection)
	em.symAddr = make(map[string]uint64)
	return em.emit()
}

// ── internal emitter ─────────────────────────────────────────────────────────

type builtSection struct {
	name    string
	shType  uint32
	flags   uint64
	data    []byte
	memSize uint64
	align   uint64
	link    uint32
	info    uint32
	entSize uint64
	fileOff uint64
	addr    uint64
	shIdx   int

	// When set, layoutSections uses these values directly instead of computing
	// new addresses. Populated from Section.PreassignedAddr/FileOffset so that
	// linker-patched section bytes are serialized at the exact addresses the
	// linker used when patching.
	hasPreassigned        bool
	preassignedAddr       uint64
	preassignedFileOffset uint64
}

type emitter struct {
	b         *Builder
	secs      []*builtSection
	secByName map[string]*builtSection
	shstrtab  strTab
	strtab    strTab
	symAddr   map[string]uint64
}

func (e *emitter) addSec(sec *builtSection) {
	sec.shIdx = len(e.secs)
	e.secs = append(e.secs, sec)
	if sec.name != "" {
		e.secByName[sec.name] = sec
	}
}

func (e *emitter) emit() ([]byte, error) {
	// ── Pass 1: collect sections ──────────────────────────────────────────

	e.addSec(&builtSection{shType: SHT_NULL, align: 1})

	for _, s := range e.b.sections {
		align := s.Align
		if align == 0 {
			align = 1
		}
		memSz := uint64(len(s.Data))
		if s.Type == SHT_NOBITS && s.Size > memSz {
			memSz = s.Size
		}
		bs := &builtSection{
			name:    s.Name,
			shType:  s.Type,
			flags:   s.Flags,
			data:    s.Data,
			memSize: memSz,
			align:   align,
			link:    s.Link,
			info:    s.Info,
			entSize: s.EntSize,
		}
		if s.PreassignedAddr != 0 || s.PreassignedFileOffset != 0 {
			bs.hasPreassigned        = true
			bs.preassignedAddr       = s.PreassignedAddr
			bs.preassignedFileOffset = s.PreassignedFileOffset
		}
		e.addSec(bs)
	}

	relaMap := make(map[string][]Reloc)
	for _, r := range e.b.relocs {
		relaMap[r.Section] = append(relaMap[r.Section], r)
	}

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
		e.addSec(&builtSection{
			name:   ".dynstr",
			shType: SHT_STRTAB,
			flags:  SHF_ALLOC,
			align:  1,
		})
		e.addSec(&builtSection{
			name:    ".dynsym",
			shType:  SHT_DYNSYM,
			flags:   SHF_ALLOC,
			align:   8,
			entSize: symEntrySize,
			info:    1,
		})
		dynSec = &builtSection{
			name:    ".dynamic",
			shType:  SHT_DYNAMIC,
			flags:   SHF_ALLOC | SHF_WRITE,
			align:   8,
			entSize: dynEntrySize,
		}
		e.addSec(dynSec)
	}

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
	e.shstrtab.add("")
	for _, sec := range e.secs {
		if sec.name != "" {
			e.shstrtab.add(sec.name)
		}
	}
	shstrtabSec.data = e.shstrtab.bytes()
	shstrtabSec.memSize = uint64(len(shstrtabSec.data))

	// ── Pass 3: build .symtab and .strtab ─────────────────────────────────
	e.strtab.add("")
	var localSyms, globalSyms []Symbol
	for _, sym := range e.b.symbols {
		if sym.Weak || sym.Global {
			globalSyms = append(globalSyms, sym)
		} else {
			localSyms = append(localSyms, sym)
		}
	}
	firstGlobal := 1 + len(localSyms)

	var symBuf bytes.Buffer
	symBuf.Write(make([]byte, symEntrySize))
	for _, sym := range append(localSyms, globalSyms...) {
		e.appendSym(&symBuf, sym)
	}
	symtabSec.data = symBuf.Bytes()
	symtabSec.memSize = uint64(len(symtabSec.data))
	symtabSec.link = uint32(strtabSec.shIdx)
	symtabSec.info = uint32(firstGlobal)

	strtabSec.data = e.strtab.bytes()
	strtabSec.memSize = uint64(len(strtabSec.data))

	// ── Pass 4: first-pass dynamic sections (establishes sizes) ───────────
	if hasDynamic {
		e.buildDynamicSections(dynSec)
	}

	// ── Pass 5: layout ─────────────────────────────────────────────────────
	estimatedPhdrs := e.estimatePhdrs(hasDynamic)
	headerArea := uint64(elfHeaderSize) + uint64(estimatedPhdrs)*phdrEntrySize
	e.layoutSections(headerArea)

	// ── Pass 6: resolve symbol virtual addresses ───────────────────────────
	for _, sym := range e.b.symbols {
		switch sym.Section {
		case "":
		case "*ABS*":
			e.symAddr[sym.Name] = sym.Offset
		default:
			if sec, ok := e.secByName[sym.Section]; ok {
				e.symAddr[sym.Name] = sec.addr + sym.Offset
			}
		}
	}

	// ── Pass 7: rebuild .symtab with resolved addresses ───────────────────
	symBuf.Reset()
	symBuf.Write(make([]byte, symEntrySize))
	for _, sym := range append(localSyms, globalSyms...) {
		e.appendSym(&symBuf, sym)
	}
	symtabSec.data = symBuf.Bytes()
	symtabSec.memSize = uint64(len(symtabSec.data))

	e.layoutSections(headerArea)

	// ── Pass 4b: rebuild dynamic sections with final addresses ─────────────
	// buildDynamicSections reads sec.addr for .dynstr and .dynsym to set
	// DT_STRTAB / DT_SYMTAB. Those addresses are 0 on the first pass since
	// .dynstr and .dynsym are bin/elf-internal sections laid out by Pass 5.
	// After re-layout those addresses are final, so this pass produces the
	// correct .dynamic content.
	if hasDynamic {
		e.buildDynamicSections(dynSec)
	}

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
	if len(phdrs) != estimatedPhdrs {
		headerArea = uint64(elfHeaderSize) + uint64(len(phdrs))*phdrEntrySize
		e.layoutSections(headerArea)
		if hasDynamic {
			e.buildDynamicSections(dynSec)
		}
		phdrs = e.buildPhdrs(hasDynamic)
	}

	// ── Pass 11: locate end of file content ───────────────────────────────
	var maxFileOff uint64
	for _, sec := range e.secs {
		if sec.shType == SHT_NOBITS || sec.shType == SHT_NULL {
			continue
		}
		if end := sec.fileOff + uint64(len(sec.data)); end > maxFileOff {
			maxFileOff = end
		}
	}
	shoff := alignUp(maxFileOff, 8)

	// ── Pass 12: serialize ─────────────────────────────────────────────────
	fileSize := shoff + uint64(len(e.secs))*shdrEntrySize
	buf := make([]byte, fileSize)

	shnum := uint32(len(e.secs))
	phnum := uint32(len(phdrs))
	shstrndx := uint32(shstrtabSec.shIdx)

	e.writeEhdr(buf, entryAddr, phnum, shoff, shnum, shstrndx)

	if shnum >= SHN_LORESERVE || phnum >= PN_XNUM || shstrndx >= SHN_LORESERVE {
		off := int(shoff)
		if shnum >= SHN_LORESERVE {
			putU64le(buf[off+32:], uint64(shnum))
		}
		if phnum >= PN_XNUM {
			putU32le(buf[off+44:], phnum)
		}
		if shstrndx >= SHN_LORESERVE {
			putU32le(buf[off+40:], shstrndx)
		}
	}

	for i, ph := range phdrs {
		e.writePhdr(buf[elfHeaderSize+i*phdrEntrySize:], ph)
	}

	for _, sec := range e.secs {
		if sec.shType == SHT_NULL || sec.shType == SHT_NOBITS || len(sec.data) == 0 {
			continue
		}
		copy(buf[sec.fileOff:], sec.data)
	}

	for i, sec := range e.secs {
		e.writeShdr(buf[int(shoff)+i*shdrEntrySize:], sec)
	}

	return buf, nil
}

// layoutSections assigns fileOff and addr to every section.
// Sections with pre-assigned addresses (from the linker) are placed at their
// designated positions; the offset cursor is advanced past them. All other
// sections are laid out sequentially starting from headerArea.
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

		// Pre-assigned sections: use linker-assigned addresses directly.
		// Advance offset past the end of this section's file extent so
		// subsequent non-pre-assigned sections don't overlap with it.
		if sec.hasPreassigned {
			sec.fileOff = sec.preassignedFileOffset
			sec.addr = sec.preassignedAddr
			if sec.shType != SHT_NOBITS && len(sec.data) > 0 {
				if end := sec.fileOff + uint64(len(sec.data)); end > offset {
					offset = end
				}
			}
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

// ── helpers (appendSym, buildSymNameIdx, buildRelaData, estimatePhdrs,
//            buildPhdrs, writeEhdr, writePhdr, writeShdr, strTab,
//            putU16le/32le/64le/I64le, alignUp, segPermKey)
// — all unchanged from previous version —

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
	if a, ok := e.symAddr[sym.Name]; ok {
		value = a
	}

	var b [symEntrySize]byte
	putU32le(b[0:], nameIdx)
	b[4] = stInfo
	b[5] = sym.Vis & 0x03
	putU16le(b[6:], shndx)
	putU64le(b[8:], value)
	putU64le(b[16:], sym.Size)
	w.Write(b[:])
}

func (e *emitter) buildSymNameIdx(locals, globals []Symbol) map[string]uint32 {
	m := make(map[string]uint32, len(locals)+len(globals))
	idx := uint32(1)
	for _, sym := range append(locals, globals...) {
		m[sym.Name] = idx
		idx++
	}
	return m
}

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

func (e *emitter) estimatePhdrs(hasDynamic bool) int {
	seen := make(map[uint32]bool)
	hasTLS := false
	for _, sec := range e.secs {
		if sec.flags&SHF_ALLOC != 0 && sec.shType != SHT_NULL {
			seen[segPermKey(sec.flags)] = true
		}
		if sec.flags&SHF_TLS != 0 {
			hasTLS = true
		}
	}
	n := len(seen)
	n++ // PT_PHDR
	if e.b.interp != "" {
		n++
	}
	if hasDynamic {
		n++
	}
	if hasTLS {
		n++
	}
	n++
	n += len(e.b.extraSegments)
	return n
}

func (e *emitter) buildPhdrs(hasDynamic bool) []phdrDesc {
	var phs []phdrDesc

	base := uint64(0)
	if e.b.fileType == ET_EXEC {
		base = defaultBase
	}

	nPhdrs := e.estimatePhdrs(hasDynamic)
	phdrFileOff := uint64(elfHeaderSize)
	phdrFileSize := uint64(nPhdrs) * phdrEntrySize

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

	firstLoad := true
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
				if me := s.fileOff + s.memSize; me > memEnd {
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

		startOff := first.fileOff
		startAddr := first.addr
		if firstLoad {
			// Extend the first LOAD back to fileOff=0 / vaddr=base so the
			// ELF header and PT_PHDR are covered by a LOAD segment.
			startOff = 0
			startAddr = base
			firstLoad = false
		}

		phs = append(phs, phdrDesc{
			pType:  PT_LOAD,
			flags:  permFlags,
			off:    startOff,
			vaddr:  startAddr,
			paddr:  startAddr,
			filesz: fileEnd - startOff,
			memsz:  memEnd - startOff,
			align:  pageSize,
		})
	}

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

	var tlsFirst *builtSection
	var tlsFilesz, tlsMemsz uint64
	for _, sec := range e.secs {
		if sec.flags&SHF_TLS == 0 || sec.shType == SHT_NULL {
			continue
		}
		if tlsFirst == nil {
			tlsFirst = sec
		}
		if rel := (sec.fileOff + sec.memSize) - tlsFirst.fileOff; rel > tlsMemsz {
			tlsMemsz = rel
		}
		if sec.shType != SHT_NOBITS {
			if frel := (sec.fileOff + uint64(len(sec.data))) - tlsFirst.fileOff; frel > tlsFilesz {
				tlsFilesz = frel
			}
		}
	}
	if tlsFirst != nil {
		phs = append(phs, phdrDesc{
			pType:  PT_TLS,
			flags:  PF_R,
			off:    tlsFirst.fileOff,
			vaddr:  tlsFirst.addr,
			paddr:  tlsFirst.addr,
			filesz: tlsFilesz,
			memsz:  tlsMemsz,
			align:  tlsFirst.align,
		})
	}

	phs = append(phs, phdrDesc{
		pType: PT_GNU_STACK,
		flags: PF_R | PF_W,
		align: pageSize,
	})

	for _, seg := range e.b.extraSegments {
		if len(seg.Sections) == 0 {
			align := seg.Align
			if align == 0 {
				align = 1
			}
			phs = append(phs, phdrDesc{pType: seg.Type, flags: seg.Flags, align: align})
			continue
		}
		var first *builtSection
		var fileEnd, memEnd uint64
		for _, name := range seg.Sections {
			sec := e.secByName[name]
			if sec == nil {
				continue
			}
			if first == nil {
				first = sec
			}
			if sec.shType == SHT_NOBITS {
				if me := sec.fileOff + sec.memSize; me > memEnd {
					memEnd = me
				}
			} else {
				fe := sec.fileOff + uint64(len(sec.data))
				if fe > fileEnd {
					fileEnd = fe
				}
				if fe > memEnd {
					memEnd = fe
				}
			}
		}
		if first == nil {
			continue
		}
		align := seg.Align
		if align == 0 {
			align = 1
		}
		phs = append(phs, phdrDesc{
			pType:  seg.Type,
			flags:  seg.Flags,
			off:    first.fileOff,
			vaddr:  first.addr,
			paddr:  first.addr,
			filesz: fileEnd - first.fileOff,
			memsz:  memEnd - first.fileOff,
			align:  align,
		})
	}

	return phs
}

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

func (e *emitter) writeEhdr(buf []byte, entry uint64, phnum uint32, shoff uint64, shnum, shstrndx uint32) {
	buf[EI_MAG0] = ELFMAG0
	buf[EI_MAG1] = ELFMAG1
	buf[EI_MAG2] = ELFMAG2
	buf[EI_MAG3] = ELFMAG3
	buf[EI_CLASS] = ELFCLASS64
	buf[EI_DATA] = ELFDATA2LSB
	buf[EI_VERSION] = EV_CURRENT
	buf[EI_OSABI] = ELFOSABI_NONE

	putU16le(buf[16:], uint16(e.b.fileType))
	putU16le(buf[18:], uint16(e.b.arch))
	putU32le(buf[20:], EV_CURRENT)
	putU64le(buf[24:], entry)
	putU64le(buf[32:], elfHeaderSize)
	putU64le(buf[40:], shoff)
	putU32le(buf[48:], e.b.flags)
	putU16le(buf[52:], elfHeaderSize)
	putU16le(buf[54:], phdrEntrySize)

	wPhnum := phnum
	if wPhnum >= PN_XNUM {
		wPhnum = PN_XNUM
	}
	wShnum := shnum
	if wShnum >= SHN_LORESERVE {
		wShnum = 0
	}
	wShstrndx := shstrndx
	if wShstrndx >= SHN_LORESERVE {
		wShstrndx = SHN_XINDEX
	}

	putU16le(buf[56:], uint16(wPhnum))
	putU16le(buf[58:], shdrEntrySize)
	putU16le(buf[60:], uint16(wShnum))
	putU16le(buf[62:], uint16(wShstrndx))
}

func (e *emitter) writePhdr(buf []byte, ph phdrDesc) {
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

type strTab struct {
	data    []byte
	indices map[string]uint32
}

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

func (t *strTab) index(s string) uint32 {
	if t.indices == nil {
		return 0
	}
	return t.indices[s]
}

func (t *strTab) bytes() []byte { return t.data }

func putU16le(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func putU32le(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64le(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
func putI64le(b []byte, v int64)  { binary.LittleEndian.PutUint64(b, uint64(v)) }

func alignUp(x, align uint64) uint64 {
	if align <= 1 {
		return x
	}
	return (x + align - 1) &^ (align - 1)
}

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