package pe

import "sort"

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
	// Determine which COMDAT sections survive: key = "objPath/secIndex".
	// We use the global symbol table's decisions (already made during ingest).
	// Here we just follow a simple rule: first definition wins for ANY/etc.
	type comdatKey struct {
		name string
	}
	comdatWinner := make(map[comdatKey]*RawSection)

	layout := &Layout{}
	order := []string{} // ordered unique section names
	byName := make(map[string]*MergedSection)

	for _, obj := range objs {
		for _, sec := range obj.Sections {
			if sec.Name == ".drectve" || sec.Name == ".llvm_addrsig" {
				continue // meta-sections, not emitted
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
					// If selection says discard incoming, skip it.
					if winner != sec {
						continue // this contribution is discarded
					}
					// Otherwise (e.g. LARGEST picked incoming), replace winner.
					comdatWinner[k] = sec
				} else {
					comdatWinner[k] = sec
				}
			}

			ms, exists := byName[sec.Name]
			if !exists {
				ms = &MergedSection{
					Name:  sec.Name,
					Chars: sec.Chars &^ (0x00F00000), // strip alignment field from output
				}
				byName[sec.Name] = ms
				order = append(order, sec.Name)
				layout.Sections = append(layout.Sections, ms)
			}
			// Update Chars (take union of flags except alignment).
			ms.Chars |= sec.Chars &^ (0x00F00000)

			// Compute alignment for this section (from Chars alignment field).
			alignment := sectionAlignFromChars(sec.Chars)

			// Append piece.
			isBSS := sec.Chars&0x00000080 != 0
			var off uint32
			if isBSS {
				// BSS: only add to virtual size.
				off = align32(ms.VirtualSize, alignment)
				sz := sec.VirtualSize
				if sz == 0 {
					sz = uint32(len(sec.Data))
				}
				ms.VirtualSize = off + sz
			} else {
				// Initialized data or code.
				if len(ms.Data) > 0 || ms.VirtualSize > 0 {
					// Pad to alignment.
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

	// Sort sections into canonical output order.
	sortSections(layout.Sections)

	return layout, nil
}

// applyComdatSelection decides whether to keep the existing winner or switch
// to incoming. Returns an error for NODUPLICATES conflicts.
// Mutates winner in-place for LARGEST (swaps to incoming).
func applyComdatSelection(winner, incoming *RawSection) error {
	sel := winner.ComdatSel
	if sel == 0 {
		sel = incoming.ComdatSel
	}
	switch sel {
	case 1: // NODUPLICATES
		return fmt.Errorf("duplicate COMDAT section %q (SELECT_NODUPLICATES)", winner.Name)
	case 2: // ANY
		return nil // keep existing
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
		return nil // leader governs; handled separately
	case 6: // LARGEST
		if len(incoming.Data) > len(winner.Data) {
			*winner = *incoming // take incoming
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

// sortSections places sections in the canonical PE output order:
// .text, .rdata, .data, .bss, then alphabetical for the rest.
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
// It stores VAddr in each MergedSection and returns the SyntheticLayout.
func AssignLayout(layout *Layout, imports []*CollectedImport, exports []ExportRecord,
	dynamicBase bool, dllName string, hasLoadCfg bool, hasDebugEntries bool) SyntheticLayout {
	return assignLayout(layout, imports, exports, dynamicBase, dllName, hasLoadCfg, hasDebugEntries)
}

// SyntheticLayout holds the computed VAs for bin/pe-synthesized sections.
type SyntheticLayout struct {
	IdataVA    uint32 // .idata section VA (0 if no imports)
	EdataVA    uint32 // .edata section VA (0 if no exports)
	RelocVA    uint32 // .reloc section VA (0 if not dynamic base)
	LcfgVA     uint32 // .rdata$lc section VA (0 if no load config)
	DebugVA    uint32 // .debug section VA (0 if no explicit debug entries)
	// IAT slot RVAs for each import symbol: "DLL\x00sym" → RVA.
	IATSlotRVA map[string]uint32
}

func assignLayout(layout *Layout, imports []*CollectedImport, exports []ExportRecord,
	dynamicBase bool, dllName string, hasLoadCfg bool, hasDebugEntries bool) SyntheticLayout {

	// Count synthetic sections bin/pe will add.
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

	// Assign VAs to user sections.
	for _, sec := range layout.Sections {
		sec.VAddr = va
		vsz := sec.VirtualSize
		if vsz == 0 {
			vsz = uint32(len(sec.Data))
		}
		va = align32(va+vsz, sectAlign)
	}

	sl := SyntheticLayout{IATSlotRVA: make(map[string]uint32)}

	// .idata VA and IAT slot computation.
	if hasImports {
		sl.IdataVA = va
		// Mirror bin/pe's buildIDATA layout to compute IAT slot RVAs.
		computeIATSlots(&sl, imports, va)
		idataSz := measureIDATA(imports)
		va = align32(va+idataSz, sectAlign)
	}

	// .edata VA.
	if hasExports {
		sl.EdataVA = va
		edataSz := measureEDATA(exports, dllName)
		va = align32(va+edataSz, sectAlign)
	}

	// .rdata$lc VA.
	if hasLoadCfg {
		sl.LcfgVA = va
		va = align32(va+148, sectAlign) // loadConfigSize = 148
	}

	// .debug VA.
	if hasDebugEntries {
		sl.DebugVA = va
		va = align32(va+sectAlign, sectAlign) // placeholder; will be exact after entries known
	}

	// .reloc VA.
	if dynamicBase {
		sl.RelocVA = va
	}

	return sl
}

// measureIDATA returns the byte size of the .idata section for the given imports.
// Mirrors bin/pe's buildIDATA layout.
func measureIDATA(imports []*CollectedImport) uint32 {
	N := len(imports)
	cur := uint32((N + 1) * 20) // descriptor table
	// ILT arrays.
	for _, imp := range imports {
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	// IAT arrays.
	for _, imp := range imports {
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	// Hint/Name entries.
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
	// DLL name strings.
	for _, imp := range imports {
		cur += uint32(len(imp.DLL)) + 1
	}
	return align32(cur, 4)
}

// computeIATSlots fills sl.IATSlotRVA for every import symbol.
func computeIATSlots(sl *SyntheticLayout, imports []*CollectedImport, baseRVA uint32) {
	N := len(imports)
	descEnd := uint32((N + 1) * 20)
	// ILT arrays.
	cur := descEnd
	iltOffsets := make([]uint32, N)
	for i, imp := range imports {
		iltOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	// IAT arrays start here.
	iatBase := cur
	iatOffsets := make([]uint32, N)
	for i, imp := range imports {
		iatOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	_ = iatBase
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