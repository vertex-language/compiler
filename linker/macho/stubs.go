package macho

import (
	"encoding/binary"
)

// ──────────────────────────────────────────────────────────────────────────────
// StubTable tracks the synthesised __stubs and __got sections.
// ──────────────────────────────────────────────────────────────────────────────

// StubEntry describes one stub/GOT pair for a dylib symbol.
type StubEntry struct {
	SymName    string
	LibOrdinal int     // 1-based dylib ordinal
	GotOffset  uint64 // byte offset within __got section
	StubOffset uint64 // byte offset within __stubs section
	GotVAddr   uint64 // filled by FinalizeStubs
	StubVAddr  uint64 // filled by FinalizeStubs
}

// StubTable manages stub and GOT synthesis for dylib symbols.
type StubTable struct {
	arch     uint32
	entries  []*StubEntry
	byName   map[string]*StubEntry

	gotData   []byte // __DATA_CONST,__got section contents
	stubsData []byte // __TEXT,__stubs section contents
}

// NewStubTable returns an empty StubTable for the given Mach-O arch.
func NewStubTable(arch uint32) *StubTable {
	return &StubTable{
		arch:   arch,
		byName: make(map[string]*StubEntry),
	}
}

// stubEntrySize returns the byte size of one stub for the given arch.
func stubEntrySize(arch uint32) uint64 {
	if arch == ArchARM64 {
		return 12
	}
	return 6 // AMD64
}

// GetOrAdd returns an existing stub entry for sym, or allocates a new one.
func (st *StubTable) GetOrAdd(symName string, libOrdinal int) *StubEntry {
	if e, ok := st.byName[symName]; ok {
		return e
	}
	sz := stubEntrySize(st.arch)
	e := &StubEntry{
		SymName:    symName,
		LibOrdinal: libOrdinal,
		GotOffset:  uint64(len(st.gotData)),
		StubOffset: uint64(len(st.stubsData)),
	}
	st.entries = append(st.entries, e)
	st.byName[symName] = e

	// Allocate GOT slot (8 bytes, initially zero).
	st.gotData = append(st.gotData, make([]byte, 8)...)

	// Allocate stub placeholder (filled in by FinalizeStubs).
	st.stubsData = append(st.stubsData, make([]byte, sz)...)

	return e
}

// GotVAddrFor returns the virtual address of the GOT slot for symName,
// or 0 if not present.
func (st *StubTable) GotVAddrFor(symName string) (uint64, bool) {
	e, ok := st.byName[symName]
	if !ok {
		return 0, false
	}
	return e.GotVAddr, true
}

// StubVAddrFor returns the virtual address of the stub for symName, or 0.
func (st *StubTable) StubVAddrFor(symName string) (uint64, bool) {
	e, ok := st.byName[symName]
	if !ok {
		return 0, false
	}
	return e.StubVAddr, true
}

// Entries returns all stub entries in allocation order.
func (st *StubTable) Entries() []*StubEntry { return st.entries }

// BuildStubs walks all relocations across all objects to find references to
// dylib symbols, allocates GOT slots and stubs, and appends the resulting
// synthetic sections to the layout.
func BuildStubs(
	arch uint32,
	objects []*ObjectFile,
	symtab *SymbolTable,
	layout *Layout,
) (*StubTable, error) {
	st := NewStubTable(arch)

	for _, obj := range objects {
		for _, rel := range obj.Relocs {
			if !rel.Extern {
				continue
			}
			if int(rel.SymIdx) >= len(obj.Symbols) {
				continue
			}
			raw := obj.Symbols[rel.SymIdx]
			if !raw.IsGlobal && !raw.IsPrivExt {
				continue
			}
			rs := symtab.Lookup(raw.Name)
			if rs == nil || rs.Kind != kindDylib {
				continue
			}
			// Allocate a stub/GOT entry for this dylib symbol.
			st.GetOrAdd(raw.Name, rs.LibOrdinal)
		}
	}

	if len(st.entries) == 0 {
		return st, nil
	}

	// Append __got section to __DATA_CONST (or __DATA if no __DATA_CONST).
	gotSec := &MergedSection{
		SegName:  "__DATA_CONST",
		SectName: "__got",
		Type:     0x06, // S_NON_LAZY_SYMBOL_POINTERS
		Attrs:    0,
		Align:    8,
		Data:     st.gotData,
		Size:     uint64(len(st.gotData)),
	}

	// Append __stubs section to __TEXT.
	stubFlags := uint32(0x80000408) // S_SYMBOL_STUBS | S_ATTR_PURE_INSTRUCTIONS | S_ATTR_SOME_INSTRUCTIONS
	if arch == ArchARM64 {
		stubFlags = 0x80000408
	}
	stubSec := &MergedSection{
		SegName:  "__TEXT",
		SectName: "__stubs",
		Type:     0x08, // S_SYMBOL_STUBS
		Attrs:    stubFlags &^ 0xff,
		Align:    uint32(stubEntrySize(arch)),
		Data:     st.stubsData,
		Size:     uint64(len(st.stubsData)),
	}

	// Insert __stubs at the end of __TEXT, __got at the end of __DATA_CONST.
	insertSection(layout, stubSec)
	insertSection(layout, gotSec)

	return st, nil
}

// FinalizeStubs patches the stub byte sequences with the correct GOT-relative
// addresses.  Must be called after AssignLayout so that VAddrs are known.
func FinalizeStubs(st *StubTable, layout *Layout) {
	gotMs := layout.SectionByKey("__DATA_CONST", "__got")
	stubMs := layout.SectionByKey("__TEXT", "__stubs")
	if gotMs == nil || stubMs == nil {
		return
	}

	for _, e := range st.entries {
		e.GotVAddr = gotMs.VAddr + e.GotOffset
		e.StubVAddr = stubMs.VAddr + e.StubOffset

		switch st.arch {
		case ArchAMD64:
			patchAMD64Stub(stubMs.Data, int(e.StubOffset), e.StubVAddr, e.GotVAddr)
		case ArchARM64:
			patchARM64Stub(stubMs.Data, int(e.StubOffset), e.StubVAddr, e.GotVAddr)
		}
	}
}

// patchAMD64Stub writes a 6-byte JMP QWORD PTR [RIP+rel32] stub.
// stubAddr is the VA of this stub, gotAddr is the VA of its GOT slot.
//
//	FF 25 <rel32>    (JMP QWORD PTR [RIP + rel32])
func patchAMD64Stub(data []byte, off int, stubAddr, gotAddr uint64) {
	data[off] = 0xFF
	data[off+1] = 0x25
	// rel32 = gotAddr - (stubAddr + 6)
	rel := int32(int64(gotAddr) - int64(stubAddr+6))
	binary.LittleEndian.PutUint32(data[off+2:], uint32(rel))
}

// patchARM64Stub writes a 12-byte ADRP+LDR+BR stub.
//
//	ADRP x16, got_page
//	LDR  x16, [x16, #got_pageoff]
//	BR   x16
func patchARM64Stub(data []byte, off int, stubAddr, gotAddr uint64) {
	// ADRP x16: computes page(gotAddr) relative to page(stubAddr).
	// pageDelta = (gotAddr>>12) - (stubAddr>>12)  (signed)
	pageDelta := int64(gotAddr>>12) - int64(stubAddr>>12)
	immlo := uint32(pageDelta & 0x3)
	immhi := uint32((pageDelta >> 2) & 0x7FFFF)
	adrp := uint32(0x90000010) | (immlo << 29) | (immhi << 5)
	binary.LittleEndian.PutUint32(data[off:], adrp)

	// LDR x16, [x16, #offset] — offset = gotAddr & 0xFFF, scaled by 8
	pageOff := (gotAddr & 0xFFF) >> 3 // divided by 8 for 64-bit LDR
	ldr := uint32(0xF9400210) | (uint32(pageOff) << 10)
	binary.LittleEndian.PutUint32(data[off+4:], ldr)

	// BR x16
	binary.LittleEndian.PutUint32(data[off+8:], 0xD61F0200)
}

// insertSection adds ms to the appropriate segment in layout, appending to
// the existing section list for that segment.  If no matching segment exists,
// one is created.
func insertSection(layout *Layout, ms *MergedSection) {
	// Check for existing segment.
	for _, seg := range layout.Segments {
		if seg.Name == ms.SegName {
			seg.Sections = append(seg.Sections, ms)
			layout.Sections = append(layout.Sections, ms)
			return
		}
	}
	// Create new segment.
	seg := &OutputSegment{Name: ms.SegName, Sections: []*MergedSection{ms}}
	layout.Segments = append(layout.Segments, seg)
	layout.Sections = append(layout.Sections, ms)
}