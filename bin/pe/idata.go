package pe

import (
	"encoding/binary"
	"fmt"
)

// idataBuild is the result of assembling a .idata section.
type idataBuild struct {
	Data          []byte
	ImportDirRVA  uint32
	ImportDirSize uint32
	IATRVA        uint32
	IATSize       uint32
	// SlotRVA maps "DLLName\x00FuncName" or "DLLName\x00#N" to the IAT slot RVA.
	// The linker uses this to patch __imp_* symbol references.
	SlotRVA map[string]uint32
}

// buildIDATA assembles a complete .idata section for the given imports.
// baseRVA is the virtual address at which the section will be loaded.
// Returns nil, nil when imports is empty.
//
// Layout (all offsets from section start):
//
//	Import Directory Table   (N+1) × 20 bytes  [null-terminated]
//	ILT arrays               one per DLL, each (M+1) × 8 bytes
//	IAT arrays               same shape as ILTs, grouped for DataDirectory[IAT]
//	Hint/Name table          2-byte hint + null-terminated name, word-aligned
//	DLL name strings         null-terminated ASCII
func buildIDATA(imports []Import, baseRVA uint32) (*idataBuild, error) {
	if len(imports) == 0 {
		return nil, nil
	}

	N := len(imports)
	le := binary.LittleEndian

	// Pass 1 – compute sub-structure offsets (none depend on baseRVA).
	const descSize = 20
	descEnd := uint32((N + 1) * descSize)

	iltOffsets := make([]uint32, N)
	cur := descEnd
	for i, imp := range imports {
		iltOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}

	iatBase := cur
	iatOffsets := make([]uint32, N)
	for i, imp := range imports {
		iatOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	iatEnd := cur

	type hnLoc struct{ offset uint32 }
	hnLocs := make([][]hnLoc, N)
	for i, imp := range imports {
		hnLocs[i] = make([]hnLoc, len(imp.Symbols))
		for j, sym := range imp.Symbols {
			if sym.Name == "" {
				continue
			}
			hnLocs[i][j] = hnLoc{offset: cur}
			size := 2 + uint32(len(sym.Name)) + 1
			if size&1 != 0 {
				size++
			}
			cur += size
		}
	}

	dllNameOffsets := make([]uint32, N)
	for i, imp := range imports {
		dllNameOffsets[i] = cur
		cur += uint32(len(imp.DLL)) + 1
	}

	totalSize := (cur + 3) &^ 3

	// Pass 2 – emit.
	buf := make([]byte, totalSize)

	for i, imp := range imports {
		base := i * descSize
		le.PutUint32(buf[base+0:], baseRVA+iltOffsets[i])
		le.PutUint32(buf[base+4:], 0) // TimeDateStamp (unbound)
		le.PutUint32(buf[base+8:], 0) // ForwarderChain (none)
		le.PutUint32(buf[base+12:], baseRVA+dllNameOffsets[i])
		le.PutUint32(buf[base+16:], baseRVA+iatOffsets[i])
		_ = imp
	}
	// Null descriptor at [N] is already zero.

	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			var entry uint64
			if sym.Name == "" {
				entry = 1<<63 | uint64(sym.Ordinal)
			} else {
				entry = uint64(baseRVA + hnLocs[i][j].offset)
			}
			le.PutUint64(buf[iltOffsets[i]+uint32(j)*8:], entry)
			le.PutUint64(buf[iatOffsets[i]+uint32(j)*8:], entry)
		}
	}

	for i, imp := range imports {
		for j, sym := range imp.Symbols {
			if sym.Name == "" {
				continue
			}
			off := hnLocs[i][j].offset
			le.PutUint16(buf[off:], sym.Hint)
			copy(buf[off+2:], sym.Name)
		}
	}

	for i, imp := range imports {
		copy(buf[dllNameOffsets[i]:], imp.DLL)
	}

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