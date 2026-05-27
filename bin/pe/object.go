package pe

import (
	"encoding/binary"
	"strings"
)

// ObjBuilder constructs a COFF object file (.obj) from sections, symbols, and
// relocations. Object files are consumed by the linker; they differ from image
// files in that they have no optional header, no DOS stub, no PE signature, and
// section VAs are all zero (relocations are resolved at link time).
//
// Usage:
//
//	ob := pe.NewObjBuilder(pe.MachineAMD64)
//	ob.AddSection(pe.COFFSection{Name: ".text", Chars: pe.ScnCode, Data: code,
//	    Relocs: []pe.COFFReloc{{Offset: 5, SymbolIndex: 1, Type: pe.IMAGE_REL_AMD64_REL32}}})
//	ob.AddSymbol(pe.COFFSymbol{Name: "main", SectionNumber: 1, StorageClass: pe.IMAGE_SYM_CLASS_EXTERNAL,
//	    Type: pe.MakeCOFFType(pe.IMAGE_SYM_TYPE_NULL, pe.IMAGE_SYM_DTYPE_FUNCTION)})
//	data, err := ob.Emit()
type ObjBuilder struct {
	machine  MachineType
	sections []COFFSection
	symbols  []COFFSymbol
}

// NewObjBuilder returns a Builder for COFF object files targeting the given machine.
func NewObjBuilder(machine MachineType) *ObjBuilder {
	return &ObjBuilder{machine: machine}
}

// AddSection appends a section to the object file.
func (ob *ObjBuilder) AddSection(s COFFSection) { ob.sections = append(ob.sections, s) }

// AddSymbol appends a symbol. Symbols must be ordered with all local (non-EXTERNAL)
// symbols before global (EXTERNAL / WEAK_EXTERNAL) symbols, as required by the spec.
func (ob *ObjBuilder) AddSymbol(s COFFSymbol) { ob.symbols = append(ob.symbols, s) }

// Emit serializes the COFF object file and returns the raw bytes.
func (ob *ObjBuilder) Emit() ([]byte, error) {
	le := binary.LittleEndian

	// ── Build string table ────────────────────────────────────────────────────
	// The string table starts with a 4-byte size field (including the size field itself).
	// Names > 8 bytes are stored here; the symbol record holds {0, offset}.
	strTab := []byte{0, 0, 0, 0} // placeholder for size
	strOffset := func(name string) uint32 {
		off := uint32(len(strTab))
		strTab = append(strTab, []byte(name)...)
		strTab = append(strTab, 0)
		return off
	}

	// ── Count total symbol records (including auxiliary records) ─────────────
	totalSymbols := 0
	for _, sym := range ob.symbols {
		totalSymbols++ // primary record
		for _, aux := range sym.Aux {
			_ = aux
			totalSymbols++ // each aux is one 18-byte record
		}
	}
	for _, sym := range ob.symbols {
		for _, aux := range sym.Aux {
			if aux.File != nil && len(aux.File.FileName) > 18 {
				// Extra aux records for long filenames.
				extra := (len(aux.File.FileName) - 18 + 17) / 18
				totalSymbols += extra
			}
		}
	}

	// ── Layout: compute file offsets ─────────────────────────────────────────
	// COFF header: 20 bytes
	// Section headers: N × 40 bytes
	// For each section: raw data, then relocation records.
	const coffHdrSize = 20
	const secHdrSize  = 40
	const relocSize   = 10

	numSections  := len(ob.sections)
	headerEnd    := coffHdrSize + numSections*secHdrSize

	type secLayout struct {
		dataOff   uint32
		relocOff  uint32
		dataSize  uint32
		relocCount uint32
	}
	layouts := make([]secLayout, numSections)
	cur := uint32(headerEnd)
	for i, sec := range ob.sections {
		layouts[i].dataOff = cur
		layouts[i].dataSize = uint32(len(sec.Data))
		cur += uint32(len(sec.Data))
	}
	for i, sec := range ob.sections {
		if len(sec.Relocs) > 0 {
			layouts[i].relocOff = cur
			layouts[i].relocCount = uint32(len(sec.Relocs))
			cur += uint32(len(sec.Relocs)) * relocSize
		}
	}
	symTabOff := cur

	// ── Serialize string table entries for long symbol names ─────────────────
	// We pre-pass to build the string table so Name fields are known.
	type symName struct {
		inline [8]byte
		isLong bool
		stOff  uint32
	}
	symNames := make([]symName, len(ob.symbols))
	for i, sym := range ob.symbols {
		name := sym.Name
		if len(name) <= 8 {
			var sn symName
			copy(sn.inline[:], name)
			symNames[i] = sn
		} else {
			off := strOffset(name)
			symNames[i] = symName{isLong: true, stOff: off}
		}
	}
	// Section names > 8 bytes (rare; use "/N" format in section header).

	// Finalize string table size.
	binary.LittleEndian.PutUint32(strTab[:4], uint32(len(strTab)))

	// ── Allocate output buffer ────────────────────────────────────────────────
	symTabSize := uint32(totalSymbols * 18)
	totalSize  := int(symTabOff) + int(symTabSize) + len(strTab)
	out := make([]byte, totalSize)

	// ── COFF file header (20 bytes) ───────────────────────────────────────────
	le.PutUint16(out[0:], uint16(ob.machine))
	le.PutUint16(out[2:], uint16(numSections))
	le.PutUint32(out[4:], 0) // TimeDateStamp: 0 for reproducible builds
	le.PutUint32(out[8:], symTabOff)
	le.PutUint32(out[12:], uint32(totalSymbols))
	le.PutUint16(out[16:], 0) // SizeOfOptionalHeader: 0 for objects
	le.PutUint16(out[18:], 0) // Characteristics

	// ── Section headers (40 bytes each) ──────────────────────────────────────
	for i, sec := range ob.sections {
		hdr := coffHdrSize + i*secHdrSize
		name := sec.Name
		if len(name) <= 8 {
			copy(out[hdr:hdr+8], name)
		} else {
			// Long name: store "/offset" into string table.
			off := strOffset(name)
			sname := make([]byte, 8)
			copy(sname, "/"+itoa(off))
			copy(out[hdr:hdr+8], sname)
		}
		le.PutUint32(out[hdr+8:], 0)             // PhysicalAddress/VirtualSize: 0
		le.PutUint32(out[hdr+12:], 0)            // VirtualAddress: 0
		le.PutUint32(out[hdr+16:], layouts[i].dataSize)
		le.PutUint32(out[hdr+20:], layouts[i].dataOff)
		le.PutUint32(out[hdr+24:], layouts[i].relocOff)
		le.PutUint32(out[hdr+28:], 0) // PointerToLinenumbers: 0
		nReloc := layouts[i].relocCount
		if nReloc > 0xFFFF {
			// Set overflow flag and write 0xFFFF in the header; actual count
			// is stored in the first relocation record's VirtualAddress field.
			le.PutUint16(out[hdr+32:], 0xFFFF)
			out[hdr+36] |= byte(IMAGE_SCN_LNK_NRELOC_OVFL >> 24) // set high bits
		} else {
			le.PutUint16(out[hdr+32:], uint16(nReloc))
		}
		le.PutUint16(out[hdr+34:], 0) // NumberOfLinenumbers: 0
		le.PutUint32(out[hdr+36:], sec.Chars)
	}

	// ── Section raw data ─────────────────────────────────────────────────────
	for i, sec := range ob.sections {
		copy(out[layouts[i].dataOff:], sec.Data)
	}

	// ── COFF relocations ─────────────────────────────────────────────────────
	for i, sec := range ob.sections {
		if len(sec.Relocs) == 0 {
			continue
		}
		off := layouts[i].relocOff
		start := 0
		if layouts[i].relocCount > 0xFFFF {
			// Extended relocation: first record's VirtualAddress = actual count.
			le.PutUint32(out[off:], layouts[i].relocCount)
			le.PutUint32(out[off+4:], 0)
			le.PutUint16(out[off+8:], IMAGE_REL_AMD64_ABSOLUTE)
			off += relocSize
			start = 0
		}
		for j := start; j < len(sec.Relocs); j++ {
			r := sec.Relocs[j]
			le.PutUint32(out[off:], r.Offset)
			le.PutUint32(out[off+4:], r.SymbolIndex)
			le.PutUint16(out[off+8:], r.Type)
			off += relocSize
		}
	}

	// ── Symbol table ─────────────────────────────────────────────────────────
	symOff := int(symTabOff)
	for i, sym := range ob.symbols {
		sn := symNames[i]
		if sn.isLong {
			le.PutUint32(out[symOff+0:], 0)
			le.PutUint32(out[symOff+4:], sn.stOff)
		} else {
			copy(out[symOff:symOff+8], sn.inline[:])
		}
		le.PutUint32(out[symOff+8:], sym.Value)
		le.PutUint16(out[symOff+12:], uint16(sym.SectionNumber))
		le.PutUint16(out[symOff+14:], sym.Type)
		out[symOff+16] = sym.StorageClass
		out[symOff+17] = uint8(countAuxRecords(sym))
		symOff += 18

		// Auxiliary records.
		for _, aux := range sym.Aux {
			var auxBuf [18]byte
			switch {
			case aux.FunctionDef != nil:
				a := aux.FunctionDef
				le.PutUint32(auxBuf[0:], a.TagIndex)
				le.PutUint32(auxBuf[4:], a.TotalSize)
				le.PutUint32(auxBuf[8:], 0) // PointerToLinenumber: 0
				le.PutUint32(auxBuf[12:], a.PointerToNextFunction)
			case aux.WeakExternal != nil:
				a := aux.WeakExternal
				le.PutUint32(auxBuf[0:], a.TagIndex)
				le.PutUint32(auxBuf[4:], a.Characteristics)
			case aux.SectionDef != nil:
				a := aux.SectionDef
				le.PutUint32(auxBuf[0:], a.Length)
				le.PutUint16(auxBuf[4:], a.NumberOfRelocations)
				le.PutUint16(auxBuf[6:], 0)
				le.PutUint32(auxBuf[8:], a.Checksum)
				le.PutUint16(auxBuf[12:], a.Number)
				auxBuf[14] = a.Selection
			case aux.File != nil:
				name := aux.File.FileName
				// First 18 bytes of filename.
				n := name
				if len(n) > 18 { n = name[:18] }
				copy(auxBuf[:], n)
				copy(out[symOff:symOff+18], auxBuf[:])
				symOff += 18
				// Overflow aux records for filenames > 18 bytes.
				for len(name) > 18 {
					name = name[18:]
					var extra [18]byte
					n = name
					if len(n) > 18 { n = name[:18] }
					copy(extra[:], n)
					copy(out[symOff:symOff+18], extra[:])
					symOff += 18
				}
				continue
			}
			copy(out[symOff:symOff+18], auxBuf[:])
			symOff += 18
		}
	}

	// ── String table ─────────────────────────────────────────────────────────
	// Update size in case strOffset was called for section long-names above.
	binary.LittleEndian.PutUint32(strTab[:4], uint32(len(strTab)))
	copy(out[symOff:], strTab)

	return out, nil
}

// countAuxRecords returns the total number of 18-byte auxiliary records for sym.
func countAuxRecords(sym COFFSymbol) int {
	total := 0
	for _, aux := range sym.Aux {
		if aux.File != nil {
			// One aux per 18 bytes of filename (rounded up).
			n := (len(aux.File.FileName) + 17) / 18
			if n == 0 { n = 1 }
			total += n
		} else {
			total++
		}
	}
	return total
}

// itoa converts a uint32 to a decimal ASCII string without importing strconv.
func itoa(n uint32) string {
	if n == 0 { return "0" }
	var buf [10]byte
	pos := 10
	for n > 0 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[pos:])
}

var _ = strings.TrimSpace // keep import if needed