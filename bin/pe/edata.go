package pe

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// measureEDATA returns the byte count of the .edata section that buildEDATA
// would produce, without actually building it. Used for VA pre-allocation.
func measureEDATA(exports []Export, dllName string) uint32 {
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
		if e.Name != "" {
			namedCount++
		}
	}
	addrTableCount := uint32(maxOrd-minOrd) + 1
	sz := uint32(40)                        // export directory header
	sz += addrTableCount * 4                // export address table
	sz += uint32(namedCount) * 4            // name pointer table
	sz += uint32(namedCount) * 2            // ordinal table
	sz += uint32(len(dllName)) + 1          // DLL name string
	for _, e := range exports {
		if e.Name != "" {
			sz += uint32(len(e.Name)) + 1
		}
	}
	return align32(sz, 4)
}

// buildEDATA assembles the .edata section.
// symVA maps symbol name → virtual address.
// imageBase is subtracted to produce RVAs stored in the address table.
func buildEDATA(exports []Export, dllName string, symVA map[string]uint64, imageBase uint64) ([]byte, error) {
	if len(exports) == 0 {
		return nil, nil
	}

	le := binary.LittleEndian

	// Sort exports by ordinal.
	sorted := append([]Export(nil), exports...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ordinal < sorted[j].Ordinal })

	minOrd := sorted[0].Ordinal
	maxOrd := sorted[len(sorted)-1].Ordinal
	addrTableCount := uint32(maxOrd-minOrd) + 1
	ordinalBase := uint32(minOrd)

	// Collect named exports, sorted lexicographically (required by spec).
	type namedExport struct {
		name    string
		ordinal uint16
	}
	var named []namedExport
	for _, e := range sorted {
		if e.Name != "" {
			named = append(named, namedExport{e.Name, e.Ordinal})
		}
	}
	sort.Slice(named, func(i, j int) bool { return named[i].name < named[j].name })
	namedCount := uint32(len(named))

	// Layout (section-relative offsets):
	//   [0]         Export Directory (40 bytes)
	//   [40]        Export Address Table  addrTableCount × 4
	//   [eatEnd]    Name Pointer Table    namedCount × 4
	//   [nptEnd]    Ordinal Table         namedCount × 2
	//   [otEnd]     DLL name string       null-terminated
	//   [dllNameEnd] Export name strings  null-terminated
	eatOff  := uint32(40)
	nptOff  := eatOff + addrTableCount*4
	otOff   := nptOff + namedCount*4
	dllOff  := align32(otOff+namedCount*2, 2)
	nameOff := dllOff + uint32(len(dllName)) + 1

	totalNameBytes := uint32(0)
	for _, n := range named {
		totalNameBytes += uint32(len(n.name)) + 1
	}
	totalSize := align32(nameOff+totalNameBytes, 4)
	buf := make([]byte, totalSize)

	// Export directory header (fields that reference other structures use section-relative
	// offsets now; fixupEDATA will add the section VA to produce RVAs).
	le.PutUint32(buf[0:], 0)             // ExportFlags (reserved)
	le.PutUint32(buf[4:], 0)             // TimeDateStamp
	le.PutUint16(buf[8:], 0)             // MajorVersion
	le.PutUint16(buf[10:], 0)            // MinorVersion
	le.PutUint32(buf[12:], dllOff)       // NameRVA (fixup pending)
	le.PutUint32(buf[16:], ordinalBase)  // OrdinalBase
	le.PutUint32(buf[20:], addrTableCount)
	le.PutUint32(buf[24:], namedCount)
	le.PutUint32(buf[28:], eatOff)       // ExportAddressTableRVA (fixup pending)
	le.PutUint32(buf[32:], nptOff)       // NamePointerRVA (fixup pending)
	le.PutUint32(buf[36:], otOff)        // OrdinalTableRVA (fixup pending)

	// Export Address Table.
	for _, e := range sorted {
		va, ok := symVA[e.Symbol]
		if !ok {
			return nil, fmt.Errorf("pe: export %q references unknown symbol %q", e.Name, e.Symbol)
		}
		slot := eatOff + uint32(e.Ordinal-minOrd)*4
		le.PutUint32(buf[slot:], uint32(va-imageBase)) // store as RVA
	}

	// Name Pointer Table and Ordinal Table.
	cur := nameOff
	for i, n := range named {
		le.PutUint32(buf[nptOff+uint32(i)*4:], cur) // name RVA (fixup pending)
		le.PutUint16(buf[otOff+uint32(i)*2:], uint16(n.ordinal-minOrd))
		copy(buf[cur:], n.name)
		cur += uint32(len(n.name)) + 1
	}

	// DLL name string.
	copy(buf[dllOff:], dllName)

	// Fix up section-relative pointers by adding the section VA.
	// (Called after the section VA is known; we do it inline here by storing
	// the section VA via the caller; the caller must pass edataVA.)
	// NOTE: fixupEDATA must be called after this function with the actual section VA.
	return buf, nil
}

// fixupEDATA adds edataVA to the four directory pointers stored as section-relative
// offsets by buildEDATA, converting them to image RVAs.
func fixupEDATA(buf []byte, edataVA uint32) {
	le := binary.LittleEndian
	for _, off := range []int{12, 28, 32, 36} {
		if v := le.Uint32(buf[off:]); v != 0 {
			le.PutUint32(buf[off:], v+edataVA)
		}
	}
	// Fix up name-pointer-table entries.
	addrTableCount := le.Uint32(buf[20:])
	namedCount     := int(le.Uint32(buf[24:]))
	nptOff         := 40 + addrTableCount*4
	for i := 0; i < namedCount; i++ {
		off := nptOff + uint32(i)*4
		if v := le.Uint32(buf[off:]); v != 0 {
			le.PutUint32(buf[off:], v+edataVA)
		}
	}
}