package macho

import (
	"errors"
	"fmt"
)

const (
	// pageSize is the VM page size used for segment alignment on both AMD64 and ARM64 macOS.
	pageSize uint64 = 0x4000 // 16 KiB (ARM64 native; x86-64 accepts it too)

	// loadAddressExecutable is the conventional base address for position-dependent executables.
	// PIE executables are loaded at a kernel-chosen address; __PAGEZERO still occupies [0, baseVA).
	baseVA uint64 = 0x100000000 // 4 GiB — standard macOS convention

	// dylinkerPath is the runtime dynamic linker.
	dylinkerPath = "/usr/lib/dyld"
)

// Builder accumulates segments, symbols, dylib references, and an entry
// point, then serialises them all into a complete 64-bit Mach-O binary via
// Emit.
type Builder struct {
	arch     Arch
	segments []Segment
	dylibs   []DylibRef
	symbols  []Symbol
	entry    string // symbol name of the entry point
	isDylib  bool   // emit MH_DYLIB instead of MH_EXECUTE
}

// NewBuilder returns a Builder targeting the given architecture.
func NewBuilder(arch Arch) *Builder {
	return &Builder{arch: arch}
}

// AddSegment appends a segment (and all its sections) to the image.
// Segments are emitted in the order they are added; __TEXT should come first.
func (b *Builder) AddSegment(seg Segment) {
	b.segments = append(b.segments, seg)
}

// AddDylib records a dynamic library to be loaded at runtime (LC_LOAD_DYLIB).
func (b *Builder) AddDylib(ref DylibRef) {
	b.dylibs = append(b.dylibs, ref)
}

// AddSymbol adds an entry to the symbol table.
func (b *Builder) AddSymbol(sym Symbol) {
	b.symbols = append(b.symbols, sym)
}

// SetEntry names the entry-point symbol. The symbol must resolve to a
// location within one of the added segments.
func (b *Builder) SetEntry(name string) {
	b.entry = name
}

// SetDylib switches the output file type to MH_DYLIB.
func (b *Builder) SetDylib() {
	b.isDylib = true
}

// Emit serialises the complete Mach-O image and returns the raw bytes.
func (b *Builder) Emit() ([]byte, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	// ------------------------------------------------------------------ //
	// Phase 1: lay out the load-command region so we know its total size.
	// We do a dry run that just counts bytes; no data is written yet.
	// ------------------------------------------------------------------ //
	lcSize, err := b.computeLoadCommandSize()
	if err != nil {
		return nil, err
	}

	// The load commands immediately follow the Mach-O header.
	headerAndLC := uint64(sizeofMachHeader64) + lcSize

	// ------------------------------------------------------------------ //
	// Phase 2: assign file offsets and virtual addresses to each section.
	// ------------------------------------------------------------------ //
	type sectionLayout struct {
		segIdx  int
		sectIdx int
		fileOff uint64
		vmAddr  uint64
		size    uint64
	}

	// fileOff starts right after the header + load commands.
	fileOff := alignUp(headerAndLC, pageSize)
	vmAddr := baseVA

	// __PAGEZERO: a read-none zero-fill segment [0, baseVA).  We synthesise
	// it automatically for executables so callers do not have to add it.
	// It does not occupy file space.

	var layouts []sectionLayout

	// Build a flat list of sections across all segments.
	for si, seg := range b.segments {
		// Each segment starts on a page boundary in both file and VM space.
		segFileStart := fileOff
		segVMStart := vmAddr

		for ki, sect := range seg.Sections {
			a := uint64(sect.Align)
			if a < 1 {
				a = 1
			}
			fileOff = alignUp(fileOff, a)
			vmAddr = alignUp(vmAddr, a)

			layouts = append(layouts, sectionLayout{
				segIdx:  si,
				sectIdx: ki,
				fileOff: fileOff,
				vmAddr:  vmAddr,
				size:    uint64(len(sect.Data)),
			})

			fileOff += uint64(len(sect.Data))
			vmAddr += uint64(len(sect.Data))
		}

		// Unused if the segment has no sections; avoids "declared but unused" warnings.
		_ = segFileStart
		_ = segVMStart
	}

	// ------------------------------------------------------------------ //
	// Phase 3: build the symbol and string tables.
	// ------------------------------------------------------------------ //
	// Symbol table layout: locals first, then external defs, then undefs
	// (required by LC_DYSYMTAB).
	type symEntry struct {
		strx   uint32
		ntype  uint8
		nsect  uint8 // 1-based section index, 0 = NO_SECT
		ndesc  uint16
		value  uint64
		global bool
	}

	// Build a string table.  Index 0 is always a NUL byte.
	strTable := []byte{0}
	addString := func(s string) uint32 {
		idx := uint32(len(strTable))
		strTable = append(strTable, []byte(s)...)
		strTable = append(strTable, 0)
		return idx
	}

	// Build a flat section index (1-based) for symbol resolution.
	type flatSection struct {
		segName  string
		sectName string
		vmAddr   uint64
	}
	var flatSects []flatSection
	for si, seg := range b.segments {
		for ki := range seg.Sections {
			l := layoutFor(layouts, si, ki)
			flatSects = append(flatSects, flatSection{
				segName:  seg.Name,
				sectName: seg.Sections[ki].Name,
				vmAddr:   l.vmAddr,
			})
		}
	}
	sectionIndex := func(segName, sectName string) uint8 {
		for i, fs := range flatSects {
			if fs.segName == segName && fs.sectName == sectName {
				return uint8(i + 1) // 1-based
			}
		}
		return 0 // NO_SECT
	}

	var localSyms, extSyms []symEntry
	for _, sym := range b.symbols {
		strx := addString(sym.Name)
		nsect := sectionIndex(sym.SegmentName, sym.SectionName)
		ntype := nSect
		if nsect == 0 {
			ntype = nUndf
		}
		e := symEntry{
			strx:   strx,
			ntype:  ntype,
			nsect:  nsect,
			value:  sym.Value,
			global: sym.Global,
		}
		if sym.Global {
			e.ntype |= nExt
			extSyms = append(extSyms, e)
		} else {
			localSyms = append(localSyms, e)
		}
	}
	allSyms := append(localSyms, extSyms...)

	// Pad string table to 8-byte boundary.
	for len(strTable)%8 != 0 {
		strTable = append(strTable, 0)
	}

	// ------------------------------------------------------------------ //
	// Phase 4: collect relocations.
	// ------------------------------------------------------------------ //
	type sectReloc struct {
		sectFlatIdx int
		relocs      []Reloc
	}
	var allRelocs []sectReloc
	symNameToIdx := func(name string) uint32 {
		for i, sym := range b.symbols {
			if sym.Name == name {
				return uint32(i)
			}
		}
		return 0
	}
	for si, seg := range b.segments {
		for ki, sect := range seg.Sections {
			if len(sect.Relocs) == 0 {
				continue
			}
			fi := flatSectIdx(flatSects, seg.Name, sect.Name)
			_ = si
			_ = ki
			allRelocs = append(allRelocs, sectReloc{fi, sect.Relocs})
		}
	}
	_ = symNameToIdx

	// ------------------------------------------------------------------ //
	// Phase 5: assign file offsets to __LINKEDIT data.
	// ------------------------------------------------------------------ //
	// __LINKEDIT is always the last segment.  Its contents (in order):
	//   relocation entries, symbol table, string table.
	linkeditFileStart := alignUp(fileOff, pageSize)

	// Relocations.
	relocFileOff := linkeditFileStart
	relocTotalBytes := uint64(0)
	for _, sr := range allRelocs {
		_ = sr
		relocTotalBytes += uint64(len(sr.relocs)) * uint64(sizeofRelocEntry)
	}

	symFileOff := relocFileOff + relocTotalBytes
	symFileOff = alignUp(symFileOff, 8)
	symBytes := uint64(len(allSyms)) * uint64(sizeofNlist64)

	strFileOff := symFileOff + symBytes

	linkeditSize := strFileOff + uint64(len(strTable)) - linkeditFileStart
	linkeditVMAddr := alignUp(vmAddr, pageSize)

	// ------------------------------------------------------------------ //
	// Phase 6: resolve entry-point file offset for LC_MAIN.
	// ------------------------------------------------------------------ //
	entryFileOff := uint64(0)
	if !b.isDylib && b.entry != "" {
		found := false
		for _, sym := range b.symbols {
			if sym.Name == b.entry {
				// value is a VM address; convert back to file offset.
				for _, l := range layouts {
					if sym.SegmentName == b.segments[l.segIdx].Name &&
						sym.SectionName == b.segments[l.segIdx].Sections[l.sectIdx].Name {
						entryFileOff = l.fileOff + sym.Value
						found = true
						break
					}
				}
				if !found {
					// Treat Value as a raw file offset as a fallback.
					entryFileOff = sym.Value
					found = true
				}
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("macho: entry symbol %q not found in symbol table", b.entry)
		}
	}

	// ------------------------------------------------------------------ //
	// Phase 7: final size and allocation.
	// ------------------------------------------------------------------ //
	totalSize := strFileOff + uint64(len(strTable))
	out := make([]byte, totalSize)

	// ------------------------------------------------------------------ //
	// Phase 8: write Mach-O header.
	// ------------------------------------------------------------------ //
	filetype := mhExecute
	flags := mhNoundefs | mhDyldlink | mhTwolevel | mhPIE
	if b.isDylib {
		filetype = mhDylib
		flags = mhNoundefs | mhDyldlink | mhTwolevel
	}

	// We need the final ncmds and sizeofcmds; compute them now.
	ncmds, sizeofcmdsU64 := b.countLoadCommands(linkeditFileStart, linkeditSize, linkeditVMAddr,
		uint32(symFileOff), uint32(len(allSyms)), uint32(strFileOff), uint32(len(strTable)),
		uint32(len(localSyms)), uint32(len(extSyms)), entryFileOff)
	emitMachHeader64(out, b.arch, uint32(filetype), flags, uint32(ncmds), uint32(sizeofcmdsU64))

	// ------------------------------------------------------------------ //
	// Phase 9: write load commands.
	// ------------------------------------------------------------------ //
	lcOff := sizeofMachHeader64

	// 9a. __PAGEZERO (executables only).
	if !b.isDylib {
		lcOff = emitSegmentCommand64(out, lcOff, "__PAGEZERO",
			0, baseVA, 0, 0,
			0, 0, 0, 0)
	}

	// 9b. User segments.
	for si, seg := range b.segments {
		// Compute segment VM and file extents from section layouts.
		var segVMStart, segVMEnd, segFileStart, segFileEnd uint64
		first := true
		for _, l := range layouts {
			if l.segIdx != si {
				continue
			}
			if first {
				segVMStart = l.vmAddr
				segFileStart = l.fileOff
				first = false
			}
			end := l.vmAddr + l.size
			if end > segVMEnd {
				segVMEnd = end
			}
			fend := l.fileOff + l.size
			if fend > segFileEnd {
				segFileEnd = fend
			}
		}
		if first {
			// Segment with no sections: place at current vmAddr.
			segVMStart = vmAddr
			segVMEnd = vmAddr
			segFileStart = fileOff
			segFileEnd = fileOff
		}
		segVMSize := alignUp(segVMEnd-segVMStart, pageSize)
		segFileSize := segFileEnd - segFileStart

		nsects := uint32(len(seg.Sections))
		lcOff = emitSegmentCommand64(out, lcOff, seg.Name,
			segVMStart, segVMSize,
			segFileStart, segFileSize,
			seg.Prot, seg.Prot, nsects, 0)

		// 9b-i. Section headers.
		for ki, sect := range seg.Sections {
			l := layoutFor(layouts, si, ki)
			nreloc := uint32(0)
			relocOff := uint32(0)
			for _, sr := range allRelocs {
				fi := flatSectIdx(flatSects, seg.Name, sect.Name)
				if sr.sectFlatIdx == fi {
					nreloc = uint32(len(sr.relocs))
					_ = relocOff
					break
				}
			}
			a := sect.Align
			if a < 1 {
				a = 1
			}
			lcOff = emitSection64(out, lcOff,
				sect.Name, seg.Name,
				l.vmAddr, l.size,
				uint32(l.fileOff), a,
				relocOff, nreloc, sect.Flags)
		}
	}

	// 9c. __LINKEDIT.
	lcOff = emitSegmentCommand64(out, lcOff, "__LINKEDIT",
		linkeditVMAddr, alignUp(linkeditSize, pageSize),
		linkeditFileStart, linkeditSize,
		ProtRead, ProtRead, 0, 0)

	// 9d. LC_LOAD_DYLINKER.
	if !b.isDylib {
		lcOff = emitLoadDylinkerCommand(out, lcOff, dylinkerPath)
	}

	// 9e. LC_LOAD_DYLIB entries.
	for _, ref := range b.dylibs {
		lcOff = emitLoadDylibCommand(out, lcOff, ref)
	}

	// 9f. LC_SYMTAB.
	lcOff = emitSymtabCommand(out, lcOff,
		uint32(symFileOff), uint32(len(allSyms)),
		uint32(strFileOff), uint32(len(strTable)))

	// 9g. LC_DYSYMTAB.
	ilocal := uint32(0)
	nlocal := uint32(len(localSyms))
	iextdef := nlocal
	nextdef := uint32(len(extSyms))
	iundef := iextdef + nextdef
	nundef := uint32(0)
	lcOff = emitDysymtabCommand(out, lcOff,
		ilocal, nlocal, iextdef, nextdef, iundef, nundef)

	// 9h. LC_MAIN (executables only).
	if !b.isDylib {
		lcOff = emitMainCommand(out, lcOff, entryFileOff)
	}

	_ = lcOff // consumed

	// ------------------------------------------------------------------ //
	// Phase 10: write section data.
	// ------------------------------------------------------------------ //
	for si, seg := range b.segments {
		for ki, sect := range seg.Sections {
			l := layoutFor(layouts, si, ki)
			copy(out[l.fileOff:], sect.Data)
		}
	}

	// ------------------------------------------------------------------ //
	// Phase 11: write relocation entries.
	// ------------------------------------------------------------------ //
	roff := int(relocFileOff)
	for _, sr := range allRelocs {
		for _, r := range sr.relocs {
			symIdx := symNameToIdx(r.Symbol)
			roff = emitRelocEntry(out, roff, r, symIdx)
		}
	}

	// ------------------------------------------------------------------ //
	// Phase 12: write symbol table (nlist_64 array).
	// ------------------------------------------------------------------ //
	soff := int(symFileOff)
	for _, sym := range allSyms {
		soff = emitNlist64(out, soff, sym.strx, sym.ntype, sym.nsect, sym.ndesc, sym.value)
	}

	// ------------------------------------------------------------------ //
	// Phase 13: write string table.
	// ------------------------------------------------------------------ //
	copy(out[strFileOff:], strTable)

	return out, nil
}

// ------------------------------------------------------------------ //
// Helpers
// ------------------------------------------------------------------ //

func (b *Builder) validate() error {
	if b.arch != ArchAMD64 && b.arch != ArchARM64 {
		return errors.New("macho: unsupported architecture")
	}
	if !b.isDylib && b.entry == "" {
		return errors.New("macho: entry point must be set for executables (call SetEntry)")
	}
	return nil
}

// computeLoadCommandSize does a dry run and returns the total byte size of all load commands.
func (b *Builder) computeLoadCommandSize() (uint64, error) {
	size := uint64(0)
	if !b.isDylib {
		// __PAGEZERO
		size += uint64(sizeofSegmentCommand64)
	}
	for _, seg := range b.segments {
		size += uint64(sizeofSegmentCommand64) + uint64(len(seg.Sections))*uint64(sizeofSection64)
	}
	// __LINKEDIT
	size += uint64(sizeofSegmentCommand64)
	// LC_LOAD_DYLINKER
	if !b.isDylib {
		size += uint64(alignUp(uint64(sizeofLoadDylinkerCommand)+uint64(len(dylinkerPath))+1, 8))
	}
	// LC_LOAD_DYLIB entries
	for _, ref := range b.dylibs {
		size += uint64(alignUp(uint64(sizeofDylibCommand)+uint64(len(ref.Path))+1, 8))
	}
	// LC_SYMTAB
	size += uint64(sizeofSymtabCommand)
	// LC_DYSYMTAB
	size += uint64(sizeofDysymtabCommand)
	// LC_MAIN
	if !b.isDylib {
		size += uint64(sizeofEntryPointCommand)
	}
	return size, nil
}

// countLoadCommands returns the (count, totalBytes) of load commands using
// the same logic as computeLoadCommandSize, but for the final header fields.
func (b *Builder) countLoadCommands(
	linkeditFileStart, linkeditSize, linkeditVMAddr uint64,
	symoff, nsyms, stroff, strsize uint32,
	nlocal, nextdef uint32,
	entryFileOff uint64,
) (ncmds int, sizeofcmds int) {
	if !b.isDylib {
		ncmds++ // __PAGEZERO
		sizeofcmds += sizeofSegmentCommand64
	}
	for _, seg := range b.segments {
		ncmds++
		sizeofcmds += sizeofSegmentCommand64 + len(seg.Sections)*sizeofSection64
	}
	// __LINKEDIT
	ncmds++
	sizeofcmds += sizeofSegmentCommand64
	// LC_LOAD_DYLINKER
	if !b.isDylib {
		ncmds++
		sizeofcmds += int(alignUp(uint64(sizeofLoadDylinkerCommand)+uint64(len(dylinkerPath))+1, 8))
	}
	// LC_LOAD_DYLIB
	for _, ref := range b.dylibs {
		ncmds++
		sizeofcmds += int(alignUp(uint64(sizeofDylibCommand)+uint64(len(ref.Path))+1, 8))
	}
	// LC_SYMTAB
	ncmds++
	sizeofcmds += sizeofSymtabCommand
	// LC_DYSYMTAB
	ncmds++
	sizeofcmds += sizeofDysymtabCommand
	// LC_MAIN
	if !b.isDylib {
		ncmds++
		sizeofcmds += sizeofEntryPointCommand
	}
	return
}

// layoutFor returns the sectionLayout for segment si, section ki.
type sectionLayout struct {
	segIdx  int
	sectIdx int
	fileOff uint64
	vmAddr  uint64
	size    uint64
}

func layoutFor(layouts []sectionLayout, si, ki int) sectionLayout {
	for _, l := range layouts {
		if l.segIdx == si && l.sectIdx == ki {
			return l
		}
	}
	return sectionLayout{}
}

// flatSectIdx returns the 0-based flat section index for a (segName, sectName) pair.
type flatSection struct {
	segName  string
	sectName string
	vmAddr   uint64
}

func flatSectIdx(flatSects []flatSection, segName, sectName string) int {
	for i, fs := range flatSects {
		if fs.segName == segName && fs.sectName == sectName {
			return i
		}
	}
	return -1
}