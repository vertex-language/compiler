// merge.go
package elf

import "fmt"

// ── Input section piece ───────────────────────────────────────────────────────

// Piece records where one input section's data lands within a merged output section.
type Piece struct {
	Obj    *ObjectFile
	Sec    *RawSection
	Offset uint64 // byte offset within the merged output section
}

// MergedSection is the result of combining all same-named input sections.
type MergedSection struct {
	Name  string
	Type  uint32
	Flags uint64
	Align uint64

	// Pieces, in the order they were appended.
	Pieces []Piece

	// Data holds the concatenated section bytes after Seal().
	// For SHT_NOBITS sections this is nil; use Size.
	Data []byte
	Size uint64 // total byte size (including alignment padding)

	// Assigned during layout.
	VAddr      uint64
	FileOffset uint64
}

// Layout holds the full set of merged sections and their assigned addresses.
type Layout struct {
	Sections []*MergedSection
	// secByName is indexed by output section name for fast relocation patching.
	secByName map[string]*MergedSection
}

// SectionByName looks up an output section by name.
func (l *Layout) SectionByName(name string) (*MergedSection, bool) {
	s, ok := l.secByName[name]
	return s, ok
}

// ── Section merging ───────────────────────────────────────────────────────────

// MergeSections groups input sections from all object files by name and
// concatenates their data, respecting alignment between contributions.
// Sections with different flags (e.g. .text vs .text.cold) are kept separate
// only if their flag bits differ; otherwise same name + same flags → merged.
func MergeSections(objects []*ObjectFile) (*Layout, error) {
	// Preserve insertion order via a slice + map combo.
	var order []string
	byKey := make(map[string]*MergedSection)

	key := func(sec *RawSection) string {
		// Group by name only. Sections with the same name but different flags
		// are a sign of a compiler bug; we merge them anyway (gcc/clang do too).
		return sec.Name
	}

	for _, obj := range objects {
		for _, sec := range obj.Sections {
			if sec == nil || sec.Index == 0 {
				continue // null section
			}
			// Skip metadata sections that the linker synthesizes from scratch:
			// symbol tables, string tables, reloc sections, group sections.
			switch sec.Type {
			case shtSymtab, shtStrtab, shtRela, 17 /*SHT_GROUP*/:
				continue
			}
			if sec.Name == "" {
				continue
			}

			k := key(sec)
			ms, exists := byKey[k]
			if !exists {
				ms = &MergedSection{
					Name:  sec.Name,
					Type:  sec.Type,
					Flags: sec.Flags,
					Align: 1,
				}
				byKey[k] = ms
				order = append(order, k)
			}

			// Alignment of the merged section = max of all contributors.
			if sec.Align > ms.Align {
				ms.Align = sec.Align
			}

			// Record the piece offset (before appending this section's data).
			var pieceOffset uint64
			if sec.Type != shtNobits {
				// Align current tail of Data to this section's requirement.
				currentSize := uint64(len(ms.Data))
				aligned := alignUp(currentSize, sec.Align)
				// Pad with zeros to alignment boundary.
				for uint64(len(ms.Data)) < aligned {
					ms.Data = append(ms.Data, 0)
				}
				pieceOffset = aligned
				ms.Data = append(ms.Data, sec.Data...)
			} else {
				// SHT_NOBITS: account for size but add no bytes to Data.
				aligned := alignUp(ms.Size, sec.Align)
				pieceOffset = aligned
				ms.Size = aligned + sec.Size
			}

			ms.Pieces = append(ms.Pieces, Piece{
				Obj:    obj,
				Sec:    sec,
				Offset: pieceOffset,
			})
		}
	}

	// Finalize Size for data-bearing sections.
	sections := make([]*MergedSection, 0, len(order))
	for _, k := range order {
		ms := byKey[k]
		if ms.Type != shtNobits {
			ms.Size = uint64(len(ms.Data))
		}
		sections = append(sections, ms)
	}

	layout := &Layout{
		Sections:  sections,
		secByName: byKey,
	}
	return layout, nil
}

// ── Virtual address assignment ─────────────────────────────────────────────────

// pageSize is the system page size used for PT_LOAD alignment.
// Changed to 0x1000 (4KB) to avoid padding 2MB of zeroes into small Wasm binaries.
const pageSize = 0x1000 

// AssignLayout assigns virtual addresses and file offsets to every merged
// section. Sections are grouped into three PT_LOAD segments:
//
//   RX:  .text, .plt, .rodata (SHF_ALLOC | SHF_EXECINSTR,  or SHF_ALLOC only)
//   RO:  .rodata, .eh_frame (SHF_ALLOC, no write, no exec) — merged into RX if small
//   RW:  .data, .bss, .got, .got.plt (SHF_ALLOC | SHF_WRITE)
//
// The base virtual address for position-dependent executables is 0x400000 (Linux
// convention). PIE/shared libraries start at 0x0 and are relocated at load time.
func AssignLayout(outputType OutputType, layout *Layout) error {
	var baseVA uint64
	switch outputType {
	case OutputExec:
		baseVA = 0x400000
	default: // PIE, shared
		baseVA = 0x0
	}

	// Force the first section to start exactly on a page boundary.
	// This decouples our VAddr math from bin/elf's internal header sizes,
	// guaranteeing reloc.go and bin/elf perfectly agree on absolute addresses.
	fileOff := uint64(pageSize)
	vaddr   := baseVA + fileOff

	// Process sections in three groups by permission.
	type group struct {
		needsWrite bool
		needsExec  bool
	}

	sectionGroup := func(ms *MergedSection) group {
		const shfAlloc = uint64(0x2)
		const shfWrite = uint64(0x1)
		const shfExec  = uint64(0x4)
		if ms.Flags&shfAlloc == 0 {
			return group{} // non-allocatable: file-only, no vaddr
		}
		return group{
			needsWrite: ms.Flags&shfWrite != 0,
			needsExec:  ms.Flags&shfExec != 0,
		}
	}

	// Sort sections: exec first, then read-only, then read-write.
	ordered := make([]*MergedSection, len(layout.Sections))
	copy(ordered, layout.Sections)

	// Simple 3-pass grouping: exec, read-only alloc, read-write.
	var exSecs, roSecs, rwSecs, nonAllocSecs []*MergedSection
	for _, ms := range ordered {
		g := sectionGroup(ms)
		const shfAlloc = uint64(0x2)
		if ms.Flags&shfAlloc == 0 {
			nonAllocSecs = append(nonAllocSecs, ms)
		} else if g.needsWrite {
			rwSecs = append(rwSecs, ms)
		} else if g.needsExec {
			exSecs = append(exSecs, ms)
		} else {
			roSecs = append(roSecs, ms)
		}
	}

	assign := func(secs []*MergedSection, newSegment bool) {
		if len(secs) == 0 {
			return
		}
		if newSegment {
			// Align to page boundary; vaddr and fileOff must be congruent mod pageSize.
			fileOff = alignUp(fileOff, pageSize)
			vaddr   = alignUp(vaddr,   pageSize)
		}

		// CRITICAL FIX: Pass the alignment requirement down to bin/elf!
		// If this is the start of the file OR a new segment, enforce page alignment
		// so bin/elf doesn't tightly pack it and overwrite memory permissions.
		if (newSegment || fileOff == pageSize) && secs[0].Align < pageSize {
			secs[0].Align = pageSize
		}

		for _, ms := range secs {
			fileOff  = alignUp(fileOff, ms.Align)
			vaddr    = alignUp(vaddr,   ms.Align)
			ms.FileOffset = fileOff
			ms.VAddr      = vaddr
			if ms.Type != shtNobits {
				fileOff += ms.Size
			}
			vaddr += ms.Size
		}
	}

	assign(exSecs, false)
	assign(roSecs, len(exSecs) > 0)
	assign(rwSecs, len(exSecs)+len(roSecs) > 0)
	// Non-allocatable sections (debug info etc.) go at the end of the file.
	for _, ms := range nonAllocSecs {
		fileOff = alignUp(fileOff, ms.Align)
		ms.FileOffset = fileOff
		ms.VAddr      = 0
		if ms.Type != shtNobits {
			fileOff += ms.Size
		}
	}

	return nil
}

// ResolveSymbolAddresses fills in VAddr for every symbol in the table
// using the section addresses assigned by AssignLayout.
func ResolveSymbolAddresses(symtab *SymbolTable, layout *Layout) error {
	for _, sym := range symtab.All() {
		if !sym.IsDefined() || sym.RawSym == nil {
			continue
		}
		raw := sym.RawSym
		if raw.SectionName == "*ABS*" {
			sym.VAddr = raw.Value
			continue
		}
		if raw.SectionName == "" {
			continue
		}
		// Find the output section this symbol lands in.
		ms, ok := layout.SectionByName(raw.SectionName)
		if !ok {
			return fmt.Errorf("symbol %q references unknown section %q", sym.Name, raw.SectionName)
		}
		// Find the piece contributed by this symbol's object file.
		var pieceOffset uint64
		found := false
		for _, p := range ms.Pieces {
			if p.Obj == sym.Object && p.Sec.Name == raw.SectionName {
				pieceOffset = p.Offset
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("symbol %q: can't locate its piece in output section %q",
				sym.Name, raw.SectionName)
		}
		sym.VAddr = ms.VAddr + pieceOffset + raw.Value
	}
	return nil
}