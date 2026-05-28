package pe

import (
	"fmt"
	"sort"
)

const (
	fileAlign    = uint32(0x200)
	sectAlign    = uint32(0x1000)
	// Header layout constants (mirrors bin/pe/builder.go).
	dosStubSz     = 64
	peSigSz       = 4
	coffHdrBytes  = 20
	optHdrBytes   = 240
	sectHdrBytes  = 40
	fixedHdrBytes = dosStubSz + peSigSz + coffHdrBytes + optHdrBytes
)

func align32(v, a uint32) uint32 { return (v + a - 1) &^ (a - 1) }

// MergeSections combines all input sections that share a name into a
// single MergedSection, respecting COMDAT deduplication rules.
func MergeSections(objs []*ObjectFile) (*Layout, error) {
	return mergeSections(objs)
}

func mergeSections(objs []*ObjectFile) (*Layout, error) {
	type comdatKey struct {
		name string
	}
	comdatWinner := make(map[comdatKey]*RawSection)

	layout := &Layout{}
	order := []string{}
	byName := make(map[string]*MergedSection)

	for _, obj := range objs {
		for _, sec := range obj.Sections {
			if sec.Name == ".drectve" || sec.Name == ".llvm_addrsig" {
				continue
			}
			if sec.Chars&0x00000200 != 0 { // IMAGE_SCN_LNK_INFO
				continue
			}
			if sec.Chars&0x00000800 != 0 { // IMAGE_SCN_LNK_REMOVE
				continue
			}

			// COMDAT deduplication.
			if sec.IsComdat {
				k := comdatKey{sec.Name}
				if winner, exists := comdatWinner[k]; exists {
					if err := applyComdatSelection(winner, sec); err != nil {
						return nil, err
					}
					if winner != sec {
						continue
					}
					comdatWinner[k] = sec
				} else {
					comdatWinner[k] = sec
				}
			}

			ms, exists := byName[sec.Name]
			if !exists {
				ms = &MergedSection{
					Name:  sec.Name,
					Chars: sec.Chars &^ (0x00F00000),
				}
				byName[sec.Name] = ms
				order = append(order, sec.Name)
				layout.Sections = append(layout.Sections, ms)
			}
			ms.Chars |= sec.Chars &^ (0x00F00000)

			alignment := sectionAlignFromChars(sec.Chars)

			isBSS := sec.Chars&0x00000080 != 0
			var off uint32
			if isBSS {
				off = align32(ms.VirtualSize, alignment)
				sz := sec.VirtualSize
				if sz == 0 {
					sz = uint32(len(sec.Data))
				}
				ms.VirtualSize = off + sz
			} else {
				if len(ms.Data) > 0 || ms.VirtualSize > 0 {
					padded := align32(uint32(len(ms.Data)), alignment)
					for uint32(len(ms.Data)) < padded {
						ms.Data = append(ms.Data, 0)
					}
					off = padded
				}
				ms.Data = append(ms.Data, sec.Data...)
				vsz := sec.VirtualSize
				if vsz == 0 {
					vsz = uint32(len(sec.Data))
				}
				ms.VirtualSize = uint32(len(ms.Data))
				if ms.VirtualSize < off+vsz {
					ms.VirtualSize = off + vsz
				}
			}

			ms.Pieces = append(ms.Pieces, Piece{
				Obj:    obj,
				Sec:    sec,
				Offset: off,
			})
		}
	}
	_ = order

	sortSections(layout.Sections)
	return layout, nil
}

// applyComdatSelection decides whether to keep the existing winner or switch
// to incoming.
func applyComdatSelection(winner, incoming *RawSection) error {
	sel := winner.ComdatSel
	if sel == 0 {
		sel = incoming.ComdatSel
	}
	switch sel {
	case 1: // NODUPLICATES
		return fmt.Errorf("duplicate COMDAT section %q (SELECT_NODUPLICATES)", winner.Name)
	case 2: // ANY
		return nil
	case 3: // SAME_SIZE
		if len(winner.Data) != len(incoming.Data) {
			return fmt.Errorf("COMDAT size mismatch for %q", winner.Name)
		}
		return nil
	case 4: // EXACT_MATCH
		if string(winner.Data) != string(incoming.Data) {
			return fmt.Errorf("COMDAT content mismatch for %q", winner.Name)
		}
		return nil
	case 5: // ASSOCIATIVE
		return nil
	case 6: // LARGEST
		if len(incoming.Data) > len(winner.Data) {
			*winner = *incoming
		}
		return nil
	default:
		return nil
	}
}

// sectionAlignFromChars extracts the alignment (in bytes) from section Chars.
func sectionAlignFromChars(chars uint32) uint32 {
	alignField := (chars >> 20) & 0xF
	if alignField == 0 {
		return 1
	}
	return 1 << (alignField - 1)
}

// sortSections places sections in the canonical PE output order.
func sortSections(secs []*MergedSection) {
	priority := map[string]int{
		".text":  0,
		".rdata": 1,
		".data":  2,
		".bss":   3,
		".pdata": 4,
		".xdata": 5,
		".tls":   6,
		".debug": 7,
		".reloc": 99,
	}
	sort.SliceStable(secs, func(i, j int) bool {
		pi, ok1 := priority[secs[i].Name]
		pj, ok2 := priority[secs[j].Name]
		if ok1 && ok2 {
			return pi < pj
		}
		if ok1 {
			return true
		}
		if ok2 {
			return false
		}
		return secs[i].Name < secs[j].Name
	})
}

// ─── Virtual address assignment ───────────────────────────────────────────────

// AssignLayout computes virtual addresses for all merged sections and the
// synthetic sections (.idata, .edata, .reloc) that bin/pe will add.
func AssignLayout(layout *Layout, imports []*CollectedImport, exports []ExportRecord,
	dynamicBase bool, dllName string, hasLoadCfg bool, hasDebugEntries bool) SyntheticLayout {
	return assignLayout(layout, imports, exports, dynamicBase, dllName, hasLoadCfg, hasDebugEntries)
}

// SyntheticLayout holds the computed VAs for bin/pe-synthesized sections.
type SyntheticLayout struct {
	IdataVA    uint32
	EdataVA    uint32
	RelocVA    uint32
	LcfgVA     uint32
	DebugVA    uint32
	IATSlotRVA map[string]uint32
}

func assignLayout(layout *Layout, imports []*CollectedImport, exports []ExportRecord,
	dynamicBase bool, dllName string, hasLoadCfg bool, hasDebugEntries bool) SyntheticLayout {

	numSynthetic := 0
	hasImports := len(imports) > 0
	hasExports := len(exports) > 0
	if hasImports {
		numSynthetic++
	}
	if hasExports {
		numSynthetic++
	}
	if hasLoadCfg {
		numSynthetic++
	}
	if hasDebugEntries {
		numSynthetic++
	}
	if dynamicBase {
		numSynthetic++
	}

	numSections := len(layout.Sections) + numSynthetic
	sizeOfHeaders := align32(uint32(fixedHdrBytes+numSections*sectHdrBytes), fileAlign)
	va := align32(sizeOfHeaders, sectAlign)

	for _, sec := range layout.Sections {
		sec.VAddr = va
		vsz := sec.VirtualSize
		if vsz == 0 {
			vsz = uint32(len(sec.Data))
		}
		va = align32(va+vsz, sectAlign)
	}

	sl := SyntheticLayout{IATSlotRVA: make(map[string]uint32)}

	if hasImports {
		sl.IdataVA = va
		computeIATSlots(&sl, imports, va)
		idataSz := measureIDATA(imports)
		va = align32(va+idataSz, sectAlign)
	}

	if hasExports {
		sl.EdataVA = va
		edataSz := measureEDATA(exports, dllName)
		va = align32(va+edataSz, sectAlign)
	}

	if hasLoadCfg {
		sl.LcfgVA = va
		va = align32(va+148, sectAlign)
	}

	if hasDebugEntries {
		sl.DebugVA = va
		va = align32(va+sectAlign, sectAlign)
	}

	if dynamicBase {
		sl.RelocVA = va
	}

	return sl
}

// measureIDATA returns the byte size of the .idata section for the given imports.
func measureIDATA(imports []*CollectedImport) uint32 {
	N := len(imports)
	cur := uint32((N + 1) * 20)
	for _, imp := range imports {
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	for _, imp := range imports {
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	for _, imp := range imports {
		for _, sym := range imp.Symbols {
			if sym.Name == "" {
				continue
			}
			sz := 2 + uint32(len(sym.Name)) + 1
			if sz&1 != 0 {
				sz++
			}
			cur += sz
		}
	}
	for _, imp := range imports {
		cur += uint32(len(imp.DLL)) + 1
	}
	return align32(cur, 4)
}

// computeIATSlots fills sl.IATSlotRVA for every import symbol.
func computeIATSlots(sl *SyntheticLayout, imports []*CollectedImport, baseRVA uint32) {
	N := len(imports)
	cur := uint32((N + 1) * 20)
	// Skip past ILT arrays.
	iltOffsets := make([]uint32, N)
	for i, imp := range imports {
		iltOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	// IAT arrays.
	iatOffsets := make([]uint32, N)
	for i, imp := range imports {
		iatOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	_ = iltOffsets

	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			key := imp.DLL + "\x00" + sym.Name
			if sym.Name == "" {
				key = fmt.Sprintf("%s\x00#%d", imp.DLL, sym.Ordinal)
			}
			sl.IATSlotRVA[key] = baseRVA + iatOffsets[i] + uint32(j)*8
		}
	}
}

// measureEDATA estimates the .edata section size.
func measureEDATA(exports []ExportRecord, dllName string) uint32 {
	if len(exports) == 0 {
		return 0
	}
	minOrd, maxOrd := uint16(0xFFFF), uint16(0)
	namedCount := 0
	for _, e := range exports {
		if e.Ordinal < minOrd {
			minOrd = e.Ordinal
		}
		if e.Ordinal > maxOrd {
			maxOrd = e.Ordinal
		}
		if e.ExportName != "" {
			namedCount++
		}
	}
	if minOrd > maxOrd {
		minOrd, maxOrd = 1, uint16(len(exports))
	}
	addrTableCount := uint32(maxOrd-minOrd) + 1
	sz := uint32(40) + addrTableCount*4 + uint32(namedCount)*4 +
		uint32(namedCount)*2 + uint32(len(dllName)+1)
	for _, e := range exports {
		if e.ExportName != "" {
			sz += uint32(len(e.ExportName)) + 1
		}
	}
	return align32(sz, 4)
}