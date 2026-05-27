package pe

import (
	"encoding/binary"
	"fmt"
)

// emit serialises o into a valid COFF .obj byte slice.
//
// File layout:
//
//	COFF header          20 bytes
//	section headers      N × 40 bytes
//	section raw data     variable (each blob file-aligned per section)
//	relocation tables    10 bytes × relocs, one block per section
//	symbol table         18 bytes × (symbols + aux records)
//	string table         4-byte size prefix + null-terminated name strings
//
// Section symbols (StorageClass=3, one aux record each) are auto-generated
// for every section so that debug tools and COMDAT-aware linkers work
// correctly.  Symbols and relocations added by the cpu backend follow them.
func emit(o *Object) ([]byte, error) {
	le := binary.LittleEndian
	nsecs := len(o.sections)

	// ── String table ──────────────────────────────────────────────────────────
	//
	// The COFF string table begins with a 4-byte size (inclusive).  Offsets
	// embedded in name fields are from the very start of the string table, so
	// the first real string lives at offset 4.

	var strBuf []byte // bytes after the 4-byte size prefix
	strCache := make(map[string]uint32)

	internStr := func(name string) uint32 {
		if off, ok := strCache[name]; ok {
			return off
		}
		off := uint32(len(strBuf)) + 4 // +4 for the size prefix
		strBuf = append(strBuf, name...)
		strBuf = append(strBuf, 0)
		strCache[name] = off
		return off
	}

	// nameField encodes a name into the 8-byte COFF name field.
	// Names ≤ 8 bytes are stored inline (null-padded).
	// Longer names are stored in the string table: first 4 bytes = 0,
	// next 4 bytes = offset into the string table (little-endian uint32).
	nameField := func(name string) [8]byte {
		var b [8]byte
		if len(name) <= 8 {
			copy(b[:], name)
		} else {
			le.PutUint32(b[4:], internStr(name))
		}
		return b
	}

	// ── Symbol records ────────────────────────────────────────────────────────
	//
	// Flat layout in the COFF symbol table:
	//   [0 … nsecs-1]   section symbols (1 sym + 1 aux each)
	//   [nsecs … ]      user symbols (1 sym; weak externals get 1 aux)
	//   [tail]          implicit undefined symbols synthesised from relocs

	type symRec struct {
		name         string
		value        uint32
		sectionNum   int16  // 0=undef, −1=abs, >0=1-based section index
		symType      uint16 // IMAGE_SYM_TYPE_*
		storageClass uint8
		aux          []byte // nil or exactly symRecSize bytes
	}

	var recs []symRec

	// 1. Auto-generate one section symbol (+ aux) per section.
	for i, s := range o.sections {
		dataLen := uint32(s.buf.Len())
		if s.isBSS {
			dataLen = s.nobitsSize
		}
		aux := make([]byte, symRecSize)
		le.PutUint32(aux[0:], dataLen)        // section byte length
		le.PutUint16(aux[12:], uint16(i+1))   // 1-based section number (for COMDAT assoc)
		// aux[4:6]  NumberOfRelocations — filled after reloc counts are known
		// aux[14]   ComdatSelection = 0 (not COMDAT)
		recs = append(recs, symRec{
			name:         s.Name,
			value:        0,
			sectionNum:   int16(i + 1),
			storageClass: SymClassStatic,
			aux:          aux,
		})
	}

	// 2. User symbols + any implicit undefs required by relocations.
	defined := make(map[string]bool, len(o.symbols))
	for _, sym := range o.symbols {
		defined[sym.Name] = true
	}
	allSyms := make([]Symbol, len(o.symbols))
	copy(allSyms, o.symbols)
	for _, r := range o.relocs {
		if !defined[r.Symbol] {
			allSyms = append(allSyms, Symbol{Name: r.Symbol, Global: true})
			defined[r.Symbol] = true
		}
	}

	for _, sym := range allSyms {
		var sectionNum int16
		var value uint32
		var storageClass uint8
		var symType uint16
		var aux []byte

		switch {
		case sym.Abs:
			sectionNum = -1 // IMAGE_SYM_ABSOLUTE (stored as 0xFFFF as uint16)
			value = sym.AbsValue
			storageClass = SymClassExternal
		case sym.Section == "":
			sectionNum = 0 // undefined
			storageClass = SymClassExternal
		default:
			si, ok := o.secIndex[sym.Section]
			if !ok {
				return nil, fmt.Errorf("object/pe emit: symbol %q references unknown section %q",
					sym.Name, sym.Section)
			}
			sectionNum = int16(si + 1)
			value = sym.Offset
			storageClass = SymClassExternal
		}

		if sym.Weak {
			storageClass = SymClassWeakExternal
			aux = make([]byte, symRecSize)
			wc := sym.WeakChars
			if wc == 0 {
				wc = WeakSearchLibrary
			}
			le.PutUint32(aux[4:], wc)
			// aux[0:4] TagIndex is filled after flat indices are computed.
		}
		if sym.IsFunction {
			symType = SymTypeFunction
		}

		recs = append(recs, symRec{
			name:         sym.Name,
			value:        value,
			sectionNum:   sectionNum,
			symType:      symType,
			storageClass: storageClass,
			aux:          aux,
		})
	}

	// ── Flat indices ──────────────────────────────────────────────────────────
	// Each symbol rec that carries an aux occupies two consecutive flat slots.

	flatIdx := make([]int, len(recs))
	flat := 0
	for i, r := range recs {
		flatIdx[i] = flat
		flat++
		if r.aux != nil {
			flat++
		}
	}
	totalSyms := flat

	// Name → flat index map used by relocation records.
	nameFlatIdx := make(map[string]int, len(recs))
	for i, r := range recs {
		// Section symbols share their name with the section; they always
		// occupy the first nsecs entries and are registered first, so a
		// user symbol with a coincidental section name would lose to them —
		// which is the correct COFF convention.
		if _, exists := nameFlatIdx[r.name]; !exists {
			nameFlatIdx[r.name] = flatIdx[i]
		}
	}

	// ── Fix up weak-external aux TagIndex values ──────────────────────────────
	weakDefaults := make(map[string]string, 4)
	for _, sym := range o.symbols {
		if sym.Weak && sym.WeakDefault != "" {
			weakDefaults[sym.Name] = sym.WeakDefault
		}
	}
	for _, r := range recs {
		if r.storageClass != SymClassWeakExternal || r.aux == nil {
			continue
		}
		if defName, ok := weakDefaults[r.name]; ok {
			if di, ok2 := nameFlatIdx[defName]; ok2 {
				le.PutUint32(r.aux[0:], uint32(di))
			}
		}
	}

	// ── Pre-register all long names so the string table is complete before
	//    we compute totalSize. ─────────────────────────────────────────────────
	for _, r := range recs {
		if len(r.name) > 8 {
			internStr(r.name)
		}
	}
	// Section header names are covered by the section-symbol loop above,
	// but be explicit in case of zero-symbol objects.
	for _, s := range o.sections {
		if len(s.Name) > 8 {
			internStr(s.Name)
		}
	}

	// ── Section file-offset layout ────────────────────────────────────────────

	type secLayout struct {
		dataOff  uint32
		dataSize uint32
		relocOff uint32
		nRelocs  uint32
	}
	sl := make([]secLayout, nsecs)

	pos := uint32(coffHdrSize + nsecs*secHdrSize)

	// Raw data blobs (skipped for BSS or empty sections).
	for i, s := range o.sections {
		if s.isBSS {
			sl[i].dataSize = s.nobitsSize
			continue
		}
		sz := uint32(s.buf.Len())
		if sz == 0 {
			continue
		}
		if a := scnAlignBytes(s.Chars); a > 1 {
			pos = alignUp32(pos, a)
		}
		sl[i].dataOff = pos
		sl[i].dataSize = sz
		pos += sz
	}

	// Relocation tables (one block per section that has relocs).
	for i, s := range o.sections {
		var nr uint32
		for _, r := range o.relocs {
			if r.Section == s.Name {
				nr++
			}
		}
		if nr == 0 {
			continue
		}
		if nr > 0xFFFF {
			return nil, fmt.Errorf("object/pe emit: section %q: %d relocations exceeds COFF limit of 65535",
				s.Name, nr)
		}
		sl[i].relocOff = pos
		sl[i].nRelocs = nr
		pos += nr * relocRecSize
	}

	// Back-fill reloc counts into section-symbol aux records.
	for i := range o.sections {
		if sl[i].nRelocs > 0 {
			le.PutUint16(recs[i].aux[4:], uint16(sl[i].nRelocs))
		}
	}

	symTabOff := pos
	strTabSize := uint32(len(strBuf)) + 4
	totalSize := int(symTabOff) + totalSyms*symRecSize + int(strTabSize)

	buf := make([]byte, totalSize)

	// ── COFF header ───────────────────────────────────────────────────────────
	le.PutUint16(buf[0:], uint16(o.arch))
	le.PutUint16(buf[2:], uint16(nsecs))
	le.PutUint32(buf[4:], 0) // TimeDateStamp = 0 for reproducible output
	le.PutUint32(buf[8:], symTabOff)
	le.PutUint32(buf[12:], uint32(totalSyms))
	le.PutUint16(buf[16:], 0) // SizeOfOptionalHeader = 0 for .obj
	le.PutUint16(buf[18:], 0) // Characteristics

	// ── Section headers ───────────────────────────────────────────────────────
	for i, s := range o.sections {
		base := coffHdrSize + i*secHdrSize

		nb := nameField(s.Name)
		copy(buf[base:], nb[:])

		// buf[base+8]  PhysicalAddress (in images: VirtualSize): data byte count.
		// buf[base+12] VirtualAddress = 0 in relocatable objects.
		// buf[base+16] SizeOfRawData: same as PhysicalAddress for init sections,
		//              0 for BSS (linker uses the PhysicalAddress for BSS size).
		// buf[base+20] PointerToRawData: 0 for BSS or empty sections.
		le.PutUint32(buf[base+8:], sl[i].dataSize)
		if !s.isBSS && sl[i].dataOff != 0 {
			le.PutUint32(buf[base+16:], sl[i].dataSize)
			le.PutUint32(buf[base+20:], sl[i].dataOff)
		}
		le.PutUint32(buf[base+24:], sl[i].relocOff)
		// buf[base+28] PointerToLinenumbers = 0
		le.PutUint16(buf[base+32:], uint16(sl[i].nRelocs))
		// buf[base+34] NumberOfLinenumbers = 0
		le.PutUint32(buf[base+36:], s.Chars)
	}

	// ── Section raw data ──────────────────────────────────────────────────────
	for i, s := range o.sections {
		if s.isBSS || sl[i].dataOff == 0 {
			continue
		}
		copy(buf[sl[i].dataOff:], s.buf.Bytes())
	}

	// ── Relocation tables ─────────────────────────────────────────────────────
	for i, s := range o.sections {
		if sl[i].nRelocs == 0 {
			continue
		}
		roff := int(sl[i].relocOff)
		for _, r := range o.relocs {
			if r.Section != s.Name {
				continue
			}
			fi, ok := nameFlatIdx[r.Symbol]
			if !ok {
				return nil, fmt.Errorf("object/pe emit: reloc in section %q references unknown symbol %q",
					s.Name, r.Symbol)
			}
			le.PutUint32(buf[roff+0:], r.Offset)
			le.PutUint32(buf[roff+4:], uint32(fi))
			le.PutUint16(buf[roff+8:], r.Type)
			roff += relocRecSize
		}
	}

	// ── Symbol table ──────────────────────────────────────────────────────────
	soff := int(symTabOff)
	for _, r := range recs {
		nb := nameField(r.name)
		copy(buf[soff:], nb[:])
		le.PutUint32(buf[soff+8:], r.value)
		le.PutUint16(buf[soff+12:], uint16(r.sectionNum))
		le.PutUint16(buf[soff+14:], r.symType)
		buf[soff+16] = r.storageClass
		if r.aux != nil {
			buf[soff+17] = 1
		}
		soff += symRecSize
		if r.aux != nil {
			copy(buf[soff:], r.aux)
			soff += symRecSize
		}
	}

	// ── String table ──────────────────────────────────────────────────────────
	le.PutUint32(buf[soff:], strTabSize)
	copy(buf[soff+4:], strBuf)

	return buf, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// scnAlignBytes extracts the alignment in bytes from section Characteristics.
// The alignment field occupies bits 20–23; value N means 1 << (N-1) bytes.
func scnAlignBytes(chars uint32) uint32 {
	field := (chars >> 20) & 0xF
	if field == 0 {
		return 1
	}
	return 1 << (field - 1)
}

// alignUp32 rounds v up to the nearest multiple of a (must be a power of two).
func alignUp32(v, a uint32) uint32 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) &^ (a - 1)
}