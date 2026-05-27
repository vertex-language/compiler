package macho

import "sort"

// ──────────────────────────────────────────────────────────────────────────────
// Layout
// ──────────────────────────────────────────────────────────────────────────────

// Layout is the result of MergeSections: a flat list of merged sections
// grouped into output segments.
type Layout struct {
	Sections []*MergedSection
	Segments []*OutputSegment
}

// SectionByKey returns the merged section for (segName, sectName), or nil.
func (l *Layout) SectionByKey(segName, sectName string) *MergedSection {
	for _, s := range l.Sections {
		if s.SegName == segName && s.SectName == sectName {
			return s
		}
	}
	return nil
}

// OutputSegment groups merged sections that share a segment name.
type OutputSegment struct {
	Name     string
	Sections []*MergedSection
	// Filled by AssignLayout:
	VMAddr   uint64
	VMSize   uint64
	FileOff  uint64
	FileSize uint64
}

// MergedSection is a single output section combining contributions from
// multiple input object files.
type MergedSection struct {
	SegName  string
	SectName string
	Type     uint32 // section type (low byte of flags)
	Attrs    uint32 // section attributes (high 3 bytes of flags)
	Align    uint32 // maximum alignment over all contributions
	Pieces   []Piece
	Data     []byte // nil for zerofill sections
	Size     uint64 // authoritative size (includes zero-fill contributions)
	// Filled by AssignLayout:
	VAddr      uint64
	FileOffset uint64
}

// Flags returns the combined section flags word.
func (ms *MergedSection) Flags() uint32 { return ms.Attrs | ms.Type }

// IsZerofill returns true when the section carries no file bytes.
func (ms *MergedSection) IsZerofill() bool {
	t := ms.Type
	return t == 0x01 || t == 0x0c || t == 0x12
}

// Piece records one input section's contribution to a MergedSection.
type Piece struct {
	Obj    *ObjectFile
	Sec    *RawSection
	Offset uint64 // byte offset of this contribution within the merged section
}

// ──────────────────────────────────────────────────────────────────────────────
// Segment ordering
// ──────────────────────────────────────────────────────────────────────────────

// segOrder defines the canonical output segment ordering.
var segOrder = []string{"__TEXT", "__DATA_CONST", "__DATA", "__LINKEDIT"}

func segIndex(name string) int {
	for i, s := range segOrder {
		if s == name {
			return i
		}
	}
	return len(segOrder) // unknown segments go last (before LINKEDIT)
}

// ──────────────────────────────────────────────────────────────────────────────
// MergeSections
// ──────────────────────────────────────────────────────────────────────────────

// MergeSections combines all input sections by (SegName, SectName) key and
// produces a Layout.  Metadata sections (debug, symtab, etc.) are skipped.
func MergeSections(objects []*ObjectFile) (*Layout, error) {
	merged := make(map[string]*MergedSection) // key = "__SEG,__sect"
	var order []string                        // insertion order for determinism

	for _, obj := range objects {
		for _, sec := range obj.Sections {
			if sec == nil {
				continue
			}
			// Skip metadata/debug sections.
			if shouldSkipSection(sec) {
				continue
			}

			key := sec.SegName + "," + sec.SectName
			ms, exists := merged[key]
			if !exists {
				ms = &MergedSection{
					SegName:  sec.SegName,
					SectName: sec.SectName,
					Type:     sec.Flags & 0xff,
					Attrs:    sec.Flags &^ 0xff,
					Align:    sec.Align,
				}
				if ms.Align < 1 {
					ms.Align = 1
				}
				merged[key] = ms
				order = append(order, key)
			} else {
				// Max alignment.
				if sec.Align > ms.Align {
					ms.Align = sec.Align
				}
				// Merge attributes (OR).
				ms.Attrs |= sec.Flags &^ 0xff
			}

			// Align current size before appending.
			alignedOff := alignUp64(ms.Size, uint64(sec.Align))
			if sec.Align < 1 {
				alignedOff = ms.Size
			}

			p := Piece{
				Obj:    obj,
				Sec:    sec,
				Offset: alignedOff,
			}
			ms.Pieces = append(ms.Pieces, p)

			if ms.IsZerofill() {
				ms.Size = alignedOff + sec.Size
			} else {
				// Append data (with alignment padding).
				padLen := int(alignedOff) - len(ms.Data)
				if padLen > 0 {
					ms.Data = append(ms.Data, make([]byte, padLen)...)
				}
				ms.Data = append(ms.Data, sec.Data...)
				ms.Size = uint64(len(ms.Data))
			}
		}
	}

	// Build the flat section list in segment + canonical order.
	sort.Slice(order, func(i, j int) bool {
		ki, kj := order[i], order[j]
		si, sj := merged[ki], merged[kj]
		oi, oj := segIndex(si.SegName), segIndex(sj.SegName)
		if oi != oj {
			return oi < oj
		}
		return ki < kj
	})

	layout := &Layout{}
	segMap := make(map[string]*OutputSegment)

	for _, key := range order {
		ms := merged[key]
		layout.Sections = append(layout.Sections, ms)

		seg, ok := segMap[ms.SegName]
		if !ok {
			seg = &OutputSegment{Name: ms.SegName}
			segMap[ms.SegName] = seg
			layout.Segments = append(layout.Segments, seg)
		}
		seg.Sections = append(seg.Sections, ms)
	}

	// Sort segments canonically.
	sort.Slice(layout.Segments, func(i, j int) bool {
		return segIndex(layout.Segments[i].Name) < segIndex(layout.Segments[j].Name)
	})

	return layout, nil
}

// shouldSkipSection returns true for sections the linker synthesises itself
// or that should not appear in the merged output.
func shouldSkipSection(s *RawSection) bool {
	// Skip debug / DWARF sections.
	if s.Attrs&0x02000000 != 0 { // S_ATTR_DEBUG
		return true
	}
	switch s.SectName {
	case "__eh_frame":
		// Keep — needed for unwinding.
		return false
	}
	// Skip sections from the empty segment (shouldn't occur in 64-bit MH_OBJECT
	// but just in case).
	return false
}

// alignUp64 rounds v up to the next multiple of align (power of two).
func alignUp64(v, align uint64) uint64 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}