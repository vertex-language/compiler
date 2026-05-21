// Package output produces native binary formats from linked machine code.
package output

import (
	"encoding/binary"
	"sort"

	"github.com/vertex-language/compiler/object"
)

// ── ELF64 type constants ──────────────────────────────────────────────────────

const (
	etExec    = uint16(2)
	emX86_64  = uint16(62)
	ptLoad    = uint32(1)
	ptDynamic = uint32(2)
	ptInterp  = uint32(3)
	ptTLS     = uint32(7)
	pfRead    = uint32(0x4)
	pfWrite   = uint32(0x2)
	pfExec    = uint32(0x1)

	shtNull     = uint32(0)
	shtProgbits = uint32(1)
	shtSymtab   = uint32(2)
	shtStrtab   = uint32(3)
	shtRela     = uint32(4)
	shtDynamic  = uint32(6)
	shtNobits   = uint32(8)
	shtDynSym   = uint32(11)

	shfWrite     = uint64(0x1)
	shfAlloc     = uint64(0x2)
	shfExecInstr = uint64(0x4)
	shfInfoLink  = uint64(0x40)
	shfTLS       = uint64(0x400)

	stbGlobal = byte(1)
	sttFunc   = byte(2)

	elfHdrSize = 64
	phdrSize   = 56
	shdrSize   = 64
	symEntSize = 24
	relaEntSize = 24

	loadBase = uint64(0x400000)
	segAlign = uint64(0x200000) // 2 MiB standard segment alignment
)

// ── Layout ────────────────────────────────────────────────────────────────────

// DynamicSizes holds the byte sizes of synthesized dynamic sections.
type DynamicSizes struct {
	InterpSize  uint64
	DynStrSize  uint64
	DynSymSize  uint64
	RelaPltSize uint64
	PltSize     uint64
	DynamicSize uint64
	GotPltSize  uint64
}

// ELFLayout holds every file offset and virtual address for the output binary.
type ELFLayout struct {
	// Virtual addresses
	TextVA   uint64
	RodataVA uint64
	TdataVA  uint64
	DataVA   uint64
	GotVA    uint64
	BssVA    uint64

	// File offsets
	TextOff   uint64
	RodataOff uint64
	TdataOff  uint64
	DataOff   uint64
	GotOff    uint64

	// Section sizes (from input)
	TextSize   uint64
	RodataSize uint64
	TdataSize  uint64
	DataSize   uint64
	GotSize    uint64
	BssSize    uint64
	TbssSize   uint64

	// Dynamic layout variables
	IsDynamic                                       bool
	InterpVA, DynStrVA, DynSymVA, RelaPltVA, PltVA  uint64
	DynamicVA, GotPltVA                             uint64
	InterpOff, DynStrOff, DynSymOff, RelaPltOff     uint64
	PltOff, DynamicOff, GotPltOff                   uint64
	InterpSize, DynStrSize, DynSymSize, RelaPltSize uint64
	PltSize, DynamicSize, GotPltSize                uint64

	// Segment geometry
	RxFileSize uint64
	RwFileOff  uint64
	RwVA       uint64
	RwFileSize uint64
	RwMemSize  uint64

	// TLS
	HasTLS     bool
	TLSMemSize uint64

	NumPHdrs int
}

const tlsAlign = uint64(16)

func alignUp(x, a uint64) uint64 {
	if a <= 1 {
		return x
	}
	return (x + a - 1) &^ (a - 1)
}

// ComputeLayout assigns file offsets and virtual addresses for all output sections.
func ComputeLayout(
	textSize, rodataSize, tdataSize, tbssSize, dataSize, bssSize, gotSize uint64,
	ds DynamicSizes,
) ELFLayout {
	hasTLS := tdataSize > 0 || tbssSize > 0
	isDyn := ds.InterpSize > 0

	numPHdrs := 2
	if hasTLS {
		numPHdrs++
	}
	if isDyn {
		numPHdrs += 2 // PT_INTERP and PT_DYNAMIC
	}

	hdrBytes := uint64(elfHdrSize + numPHdrs*phdrSize)

	l := ELFLayout{
		TextSize: textSize, RodataSize: rodataSize, TdataSize: tdataSize,
		DataSize: dataSize, GotSize: gotSize, BssSize: bssSize, TbssSize: tbssSize,
		HasTLS: hasTLS, IsDynamic: isDyn, NumPHdrs: numPHdrs,
		InterpSize: ds.InterpSize, DynStrSize: ds.DynStrSize, DynSymSize: ds.DynSymSize,
		RelaPltSize: ds.RelaPltSize, PltSize: ds.PltSize, DynamicSize: ds.DynamicSize,
		GotPltSize: ds.GotPltSize,
	}

	offset := hdrBytes

	// ── RX Segment: Headers -> [Dynamic RX Sections] -> Text -> Rodata ──
	if isDyn {
		l.InterpOff = offset
		l.InterpVA = loadBase + offset
		offset += ds.InterpSize

		l.DynStrOff = offset
		l.DynStrVA = loadBase + offset
		offset += ds.DynStrSize

		l.DynSymOff = alignUp(offset, 8)
		l.DynSymVA = loadBase + l.DynSymOff
		offset = l.DynSymOff + ds.DynSymSize

		l.RelaPltOff = alignUp(offset, 8)
		l.RelaPltVA = loadBase + l.RelaPltOff
		offset = l.RelaPltOff + ds.RelaPltSize

		l.PltOff = alignUp(offset, 16)
		l.PltVA = loadBase + l.PltOff
		offset = l.PltOff + ds.PltSize
	}

	l.TextOff = alignUp(offset, 16)
	l.TextVA = loadBase + l.TextOff
	offset = l.TextOff + textSize

	l.RodataOff = alignUp(offset, 8)
	l.RodataVA = loadBase + l.RodataOff
	offset = l.RodataOff + rodataSize

	l.RxFileSize = offset

	// ── RW Segment: Page-aligned in file and VA ──
	l.RwFileOff = alignUp(l.RxFileSize, segAlign)
	l.RwVA = loadBase + l.RwFileOff
	offset = l.RwFileOff

	if isDyn {
		l.DynamicOff = offset
		l.DynamicVA = l.RwVA
		offset += ds.DynamicSize

		l.GotPltOff = alignUp(offset, 8)
		l.GotPltVA = l.RwVA + (l.GotPltOff - l.RwFileOff)
		offset = l.GotPltOff + ds.GotPltSize
	}

	l.TdataOff = offset
	l.TdataVA = l.RwVA + (offset - l.RwFileOff)
	l.DataOff = alignUp(l.TdataOff+tdataSize, 8)
	l.DataVA = l.RwVA + (l.DataOff - l.RwFileOff)
	
	l.GotOff = alignUp(l.DataOff+dataSize, 8)
	l.GotVA = l.RwVA + (l.GotOff - l.RwFileOff)
	l.BssVA = l.GotVA + gotSize

	l.RwFileSize = (l.GotOff + gotSize) - l.RwFileOff
	l.RwMemSize = l.RwFileSize + bssSize

	if hasTLS {
		l.TLSMemSize = alignUp(tdataSize+tbssSize, tlsAlign)
	}
	return l
}

// SymbolVA returns the virtual address of a defined symbol.
func (l *ELFLayout) SymbolVA(sec object.SymSection, off int) uint64 {
	switch sec {
	case object.SymSecText:   return l.TextVA + uint64(off)
	case object.SymSecROData: return l.RodataVA + uint64(off)
	case object.SymSecData:   return l.DataVA + uint64(off)
	case object.SymSecBSS:    return l.BssVA + uint64(off)
	case object.SymSecTData:  return l.TdataVA + uint64(off)
	case object.SymSecTBSS:   return l.TdataVA + l.TdataSize + uint64(off)
	}
	return 0
}

// SectionBaseVA returns the base VA of a linker sectionKind (int enum to avoid cycle).
func (l *ELFLayout) SectionBaseVA(secKind int) uint64 {
	switch secKind {
	case 0: return l.TextVA
	case 1: return l.RodataVA
	case 2: return l.DataVA
	}
	return 0
}

// ── BuildELF64 ────────────────────────────────────────────────────────────────

// BuildParams bundles everything BuildELF64 needs.
type BuildParams struct {
	Lay      ELFLayout
	Text     []byte
	ROData   []byte
	TData    []byte
	TBSSSize uint64
	Data     []byte
	BSS      uint64
	GOT      []byte
	Syms     map[string]int
	EntryVA  uint64

	// Dynamic sections
	Interp   []byte
	DynStr   []byte
	DynSym   []byte
	RelaPlt  []byte
	Plt      []byte
	Dynamic  []byte
	GotPlt   []byte
}

func BuildELF64(p BuildParams) []byte {
	lay := p.Lay
	le := binary.LittleEndian

	// ── .strtab and .symtab (Static Symbol Table) ──
	names := make([]string, 0, len(p.Syms))
	for n := range p.Syms {
		names = append(names, n)
	}
	sort.Strings(names)

	strtab := []byte{0}
	nameOff := make(map[string]uint32, len(names))
	for _, n := range names {
		nameOff[n] = uint32(len(strtab))
		strtab = append(strtab, n...)
		strtab = append(strtab, 0)
	}

	symtab := make([]byte, symEntSize) // entry 0: null
	for _, n := range names {
		var s [symEntSize]byte
		le.PutUint32(s[0:], nameOff[n])
		s[4] = (stbGlobal << 4) | sttFunc
		le.PutUint16(s[6:], 0) // Will patch section index later
		le.PutUint64(s[8:], lay.TextVA+uint64(p.Syms[n]))
		symtab = append(symtab, s[:]...)
	}

	// ── .shstrtab builder ──
	var shstrtab []byte
	shNameOffs := make(map[string]uint32)
	addShStr := func(name string) uint32 {
		if off, ok := shNameOffs[name]; ok {
			return off
		}
		off := uint32(len(shstrtab))
		shstrtab = append(shstrtab, name...)
		shstrtab = append(shstrtab, 0)
		shNameOffs[name] = off
		return off
	}
	addShStr("") // null string at 0

	// ── Dynamic Section Header Construction ──
	type outSec struct {
		name   string
		header shdr
		data   []byte
	}
	var sections []outSec

	// [0] NULL section
	sections = append(sections, outSec{name: "", header: shdr{}})

	// Add a section helper
	addSection := func(name string, typ uint32, flags, addr, off, size uint64, link, info uint32, align, entsize uint64, data []byte) uint32 {
		idx := uint32(len(sections))
		sections = append(sections, outSec{
			name: name,
			header: shdr{
				name: addShStr(name), typ: typ, flags: flags,
				addr: addr, off: off, size: size,
				link: link, info: info, align: align, entsize: entsize,
			},
			data: data,
		})
		return idx
	}

	// Track indices to resolve links (Removed unused pltIdx and relaPltIdx)
	var dynStrIdx, dynSymIdx, gotPltIdx, textIdx uint32

	if lay.IsDynamic {
		addSection(".interp", shtProgbits, shfAlloc, lay.InterpVA, lay.InterpOff, lay.InterpSize, 0, 0, 1, 0, p.Interp)
		dynStrIdx = addSection(".dynstr", shtStrtab, shfAlloc, lay.DynStrVA, lay.DynStrOff, lay.DynStrSize, 0, 0, 1, 0, p.DynStr)
		dynSymIdx = addSection(".dynsym", shtDynSym, shfAlloc, lay.DynSymVA, lay.DynSymOff, lay.DynSymSize, dynStrIdx, 1, 8, symEntSize, p.DynSym)
		
		// .rela.plt links to .dynsym and info points to .got.plt (we patch info below)
		addSection(".rela.plt", shtRela, shfAlloc|shfInfoLink, lay.RelaPltVA, lay.RelaPltOff, lay.RelaPltSize, dynSymIdx, 0, 8, relaEntSize, p.RelaPlt)
		
		// We don't need to save the returned index for .plt
		addSection(".plt", shtProgbits, shfAlloc|shfExecInstr, lay.PltVA, lay.PltOff, lay.PltSize, 0, 0, 16, 16, p.Plt)
	}

	textIdx = addSection(".text", shtProgbits, shfAlloc|shfExecInstr, lay.TextVA, lay.TextOff, lay.TextSize, 0, 0, 16, 0, p.Text)
	addSection(".rodata", shtProgbits, shfAlloc, lay.RodataVA, lay.RodataOff, lay.RodataSize, 0, 0, 8, 0, p.ROData)

	if lay.IsDynamic {
		addSection(".dynamic", shtDynamic, shfAlloc|shfWrite, lay.DynamicVA, lay.DynamicOff, lay.DynamicSize, dynStrIdx, 0, 8, 16, p.Dynamic)
		gotPltIdx = addSection(".got.plt", shtProgbits, shfAlloc|shfWrite, lay.GotPltVA, lay.GotPltOff, lay.GotPltSize, 0, 0, 8, 8, p.GotPlt)
		
		// Patch .rela.plt info to point to .got.plt.
		for i := range sections {
			if sections[i].name == ".rela.plt" {
				sections[i].header.info = gotPltIdx
			}
		}
	}

	addSection(".tdata", shtProgbits, shfAlloc|shfWrite|shfTLS, lay.TdataVA, lay.TdataOff, lay.TdataSize, 0, 0, 8, 0, p.TData)
	addSection(".tbss", shtNobits, shfAlloc|shfWrite|shfTLS, lay.TdataVA+lay.TdataSize, lay.TdataOff+lay.TdataSize, lay.TbssSize, 0, 0, 8, 0, nil)
	addSection(".data", shtProgbits, shfAlloc|shfWrite, lay.DataVA, lay.DataOff, lay.DataSize, 0, 0, 8, 0, p.Data)
	addSection(".got", shtProgbits, shfAlloc|shfWrite, lay.GotVA, lay.GotOff, lay.GotSize, 0, 0, 8, 8, p.GOT)
	addSection(".bss", shtNobits, shfAlloc|shfWrite, lay.BssVA, lay.GotOff+lay.GotSize, lay.BssSize, 0, 0, 8, 0, nil)

	// Patch the .symtab to point defined text symbols to the actual .text section index
	for i := 1; i < len(names)+1; i++ {
		le.PutUint16(symtab[i*symEntSize+6:], uint16(textIdx))
	}

	// Calculate metadata offsets
	dataEnd := lay.BssVA - loadBase // equivalent to max file offset thus far conceptually
	if lay.GotOff+lay.GotSize > dataEnd {
		dataEnd = lay.GotOff + lay.GotSize
	}

	symtabOff := alignUp(dataEnd, 8)
	strtabOff := symtabOff + uint64(len(symtab))
	
	strtabIdx := uint32(len(sections) + 1) // Symtab will be here, Strtab next
	addSection(".symtab", shtSymtab, 0, 0, symtabOff, uint64(len(symtab)), strtabIdx, 1, 8, symEntSize, symtab)
	addSection(".strtab", shtStrtab, 0, 0, strtabOff, uint64(len(strtab)), 0, 0, 1, 0, strtab)

	// Finally, the Section String Table itself
	shstrtabOff := strtabOff + uint64(len(strtab))
	// Add it to our builder to get its name offset, but we write it manually since its size changes
	shstrNameOff := addShStr(".shstrtab")
	
	shdrOff := alignUp(shstrtabOff+uint64(len(shstrtab)), 8)
	numSections := uint16(len(sections) + 1) // +1 for the .shstrtab section we append manually
	totalSize := shdrOff + uint64(numSections)*shdrSize

	out := make([]byte, totalSize)

	// ── ELF Header ──
	copy(out[0:], []byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	le.PutUint16(out[16:], etExec)
	le.PutUint16(out[18:], emX86_64)
	le.PutUint32(out[20:], 1) // EV_CURRENT
	le.PutUint64(out[24:], p.EntryVA)
	le.PutUint64(out[32:], elfHdrSize)
	le.PutUint64(out[40:], shdrOff)
	le.PutUint16(out[52:], elfHdrSize)
	le.PutUint16(out[54:], phdrSize)
	le.PutUint16(out[56:], uint16(lay.NumPHdrs))
	le.PutUint16(out[58:], shdrSize)
	le.PutUint16(out[60:], numSections)
	le.PutUint16(out[62:], numSections-1) // .shstrtab is the last section

	// ── Program Headers ──
	phBase := uint64(elfHdrSize)
	phIdx := uint64(0)

	if lay.IsDynamic {
		writePHdr(out[phBase+phIdx*phdrSize:], ptInterp, pfRead, lay.InterpOff, lay.InterpVA, lay.InterpSize, lay.InterpSize, 1)
		phIdx++
	}

	// PT_LOAD RX
	writePHdr(out[phBase+phIdx*phdrSize:], ptLoad, pfRead|pfExec, 0, loadBase, lay.RxFileSize, lay.RxFileSize, segAlign)
	phIdx++

	// PT_LOAD RW
	writePHdr(out[phBase+phIdx*phdrSize:], ptLoad, pfRead|pfWrite, lay.RwFileOff, lay.RwVA, lay.RwFileSize, lay.RwMemSize, segAlign)
	phIdx++

	if lay.IsDynamic {
		writePHdr(out[phBase+phIdx*phdrSize:], ptDynamic, pfRead|pfWrite, lay.DynamicOff, lay.DynamicVA, lay.DynamicSize, lay.DynamicSize, 8)
		phIdx++
	}

	if lay.HasTLS {
		writePHdr(out[phBase+phIdx*phdrSize:], ptTLS, pfRead, lay.TdataOff, lay.TdataVA, lay.TdataSize, lay.TLSMemSize, tlsAlign)
		phIdx++
	}

	// ── Copy Section Data & Write Headers ──
	shBase := out[shdrOff:]
	for i, sec := range sections {
		if sec.data != nil {
			copy(out[sec.header.off:], sec.data)
		}
		writeSHdr(shBase[uint64(i)*shdrSize:], sec.header)
	}

	// Write .shstrtab data and its header
	copy(out[shstrtabOff:], shstrtab)
	writeSHdr(shBase[uint64(numSections-1)*shdrSize:], shdr{
		name:  shstrNameOff, typ: shtStrtab,
		off:   shstrtabOff, size: uint64(len(shstrtab)),
		align: 1,
	})

	return out
}

// ── Low-level struct writers ──────────────────────────────────────────────────

func writePHdr(dst []byte, typ, flags uint32, fileOff, va, filesz, memsz, align uint64) {
	le := binary.LittleEndian
	le.PutUint32(dst[0:], typ)
	le.PutUint32(dst[4:], flags)
	le.PutUint64(dst[8:], fileOff)
	le.PutUint64(dst[16:], va)
	le.PutUint64(dst[24:], va)
	le.PutUint64(dst[32:], filesz)
	le.PutUint64(dst[40:], memsz)
	le.PutUint64(dst[48:], align)
}

type shdr struct {
	name, typ       uint32
	flags, addr     uint64
	off, size       uint64
	link, info      uint32
	align, entsize  uint64
}

func writeSHdr(dst []byte, s shdr) {
	le := binary.LittleEndian
	le.PutUint32(dst[0:], s.name)
	le.PutUint32(dst[4:], s.typ)
	le.PutUint64(dst[8:], s.flags)
	le.PutUint64(dst[16:], s.addr)
	le.PutUint64(dst[24:], s.off)
	le.PutUint64(dst[32:], s.size)
	le.PutUint32(dst[40:], s.link)
	le.PutUint32(dst[44:], s.info)
	le.PutUint64(dst[48:], s.align)
	le.PutUint64(dst[56:], s.entsize)
}