package pe

import (
	"encoding/binary"
	"fmt"
)

// idataBuild is the result of assembling an .idata section.
type idataBuild struct {
	// Data is the complete, byte-accurate .idata section contents.
	Data []byte

	// ImportDirRVA and ImportDirSize describe the Import Directory Table
	// for DataDirectory[dataDirImport].
	ImportDirRVA  uint32
	ImportDirSize uint32

	// IATRVA and IATSize describe the contiguous IAT block for
	// DataDirectory[dataDirIAT]. The IAT block covers all DLLs' IAT
	// arrays end to end.
	IATRVA  uint32
	IATSize uint32

	// SlotRVA maps a lookup key to the RVA of that import's IAT slot.
	// For named imports the key is "DLLName\x00FuncName".
	// For ordinal imports the key is "DLLName\x00#N" (N = decimal ordinal).
	// Used by the relocation engine to patch __imp_* symbol references.
	SlotRVA map[string]uint32
}

// buildIDATA assembles a complete .idata section for the given set of imports.
// baseRVA is the virtual address at which the section will be loaded.
// Returns nil, nil when imports is empty.
//
// Layout within the generated section:
//
//	[0]                Import Directory Table  (N+1) × 20 bytes
//	[descEnd]          ILT arrays              one per DLL, each (M+1)×8 bytes
//	[iltEnd / iatBase] IAT arrays              one per DLL, same shape as ILTs
//	[iatEnd]           Hint/Name table         2-byte hint + null-terminated name
//	[hnEnd]            DLL name strings        null-terminated ASCII
//
// The IAT is placed as a single contiguous block so that DataDirectory[IAT]
// can describe it with a single RVA+size pair.
func buildIDATA(imports []Import, baseRVA uint32) (*idataBuild, error) {
	if len(imports) == 0 {
		return nil, nil
	}

	N := len(imports)
	le := binary.LittleEndian

	// ---------------------------------------------------------------
	// Pass 1 – compute the offset of every sub-structure.
	// None of these offsets depend on baseRVA; only the RVA values
	// written into the structures do.
	// ---------------------------------------------------------------

	// 1. Import Directory Table: (N+1) × 20 bytes, null entry at [N].
	const descSize = 20
	descEnd := uint32((N + 1) * descSize)

	// 2. ILT arrays (Import Lookup Tables / Import Name Tables).
	//    PE32+ entries are 8 bytes each; each array is null-terminated.
	iltOffsets := make([]uint32, N) // offset of DLL[i]'s ILT
	cur := descEnd
	for i, imp := range imports {
		iltOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}

	// 3. IAT arrays – on disk identical to the corresponding ILT.
	//    All IATs are grouped together so DataDirectory[IAT] is contiguous.
	iatBase := cur // start of the entire IAT block
	iatOffsets := make([]uint32, N) // offset of DLL[i]'s IAT
	for i, imp := range imports {
		iatOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	iatEnd := cur

	// 4. Hint/Name table.
	//    Each entry: [hint: u16][name: cstring][optional 0x00 pad to word boundary].
	//    Ordinal-only imports have no Hint/Name entry.
	type hnLoc struct{ offset uint32 }
	hnLocs := make([][]hnLoc, N) // [dllIdx][symIdx]
	for i, imp := range imports {
		hnLocs[i] = make([]hnLoc, len(imp.Symbols))
		for j, sym := range imp.Symbols {
			if sym.Name == "" {
				continue // ordinal import – no Hint/Name record
			}
			hnLocs[i][j] = hnLoc{offset: cur}
			size := 2 + uint32(len(sym.Name)) + 1 // hint(2) + name + NUL
			if size&1 != 0 {
				size++ // pad to even boundary
			}
			cur += size
		}
	}

	// 5. DLL name strings (null-terminated; no alignment requirement).
	dllNameOffsets := make([]uint32, N)
	for i, imp := range imports {
		dllNameOffsets[i] = cur
		cur += uint32(len(imp.DLL)) + 1
	}

	// Round total size up to 4-byte boundary.
	totalSize := (cur + 3) &^ 3

	// ---------------------------------------------------------------
	// Pass 2 – emit.
	// ---------------------------------------------------------------
	buf := make([]byte, totalSize) // zero-initialised

	// (a) Import Directory Table descriptors.
	for i, imp := range imports {
		base := i * descSize
		le.PutUint32(buf[base+0:], baseRVA+iltOffsets[i])      // OriginalFirstThunk → ILT
		le.PutUint32(buf[base+4:], 0)                           // TimeDateStamp (unbound)
		le.PutUint32(buf[base+8:], 0)                           // ForwarderChain (none)
		le.PutUint32(buf[base+12:], baseRVA+dllNameOffsets[i])  // Name → DLL name string
		le.PutUint32(buf[base+16:], baseRVA+iatOffsets[i])      // FirstThunk → IAT
		_ = imp
	}
	// Descriptor at index N is the null sentinel – already zeroed by make.

	// (b) ILT and IAT arrays (identical content on disk).
	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			var entry uint64
			if sym.Name == "" {
				// Ordinal import: MSB set, low 16 bits hold the ordinal.
				entry = 1<<63 | uint64(sym.Ordinal)
			} else {
				// Name import: RVA of the Hint/Name entry.
				entry = uint64(baseRVA + hnLocs[i][j].offset)
			}
			iltSlot := iltOffsets[i] + uint32(j)*8
			le.PutUint64(buf[iltSlot:], entry)

			iatSlot := iatOffsets[i] + uint32(j)*8
			le.PutUint64(buf[iatSlot:], entry)
		}
		// Null terminators at slot [M] are already zero.
	}

	// (c) Hint/Name entries.
	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			if sym.Name == "" {
				continue
			}
			off := hnLocs[i][j].offset
			le.PutUint16(buf[off:], sym.Hint)
			copy(buf[off+2:], sym.Name)
			// NUL terminator and pad byte are already zero.
		}
	}

	// (d) DLL name strings.
	for i, imp := range imports {
		copy(buf[dllNameOffsets[i]:], imp.DLL)
		// NUL terminator already zero.
	}

	// ---------------------------------------------------------------
	// Build the SlotRVA map for relocation patching of __imp_* refs.
	// ---------------------------------------------------------------
	slotRVA := make(map[string]uint32, N*4)
	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			var key string
			if sym.Name == "" {
				key = fmt.Sprintf("%s\x00#%d", imp.DLL, sym.Ordinal)
			} else {
				key = imp.DLL + "\x00" + sym.Name
			}
			slotRVA[key] = baseRVA + iatOffsets[i] + uint32(j)*8
		}
	}

	return &idataBuild{
		Data:          buf,
		ImportDirRVA:  baseRVA,
		ImportDirSize: uint32((N + 1) * descSize),
		IATRVA:        baseRVA + iatBase,
		IATSize:       iatEnd - iatBase,
		SlotRVA:       slotRVA,
	}, nil
}