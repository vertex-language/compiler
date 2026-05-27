package pe

import (
	"encoding/binary"
	"fmt"
)

// delayBuild holds the sub-buffers assembled by buildDelayIDATA.
type delayBuild struct {
	descriptorData []byte // array of IMAGE_DELAYLOAD_DESCRIPTOR (null-terminated)
	moduleHandles  []byte // one QWORD per DLL, zeroed (filled by helper at runtime)
	iatData        []byte // delay IAT arrays
	intData        []byte // delay INT arrays (hint/name or ordinal entries)
	hnData         []byte // hint/name records
	nameData       []byte // DLL name strings
	descriptorRVA  uint32
	descriptorSize uint32
}

// flat concatenates all sub-buffers into a single contiguous .didat blob.
func (d *delayBuild) flat() []byte {
	var out []byte
	out = append(out, d.descriptorData...)
	out = append(out, d.moduleHandles...)
	out = append(out, d.iatData...)
	out = append(out, d.intData...)
	out = append(out, d.hnData...)
	out = append(out, d.nameData...)
	return padToAlignment(out, 4)
}

// buildDelayIDATA assembles a .didat (delay-load import) section.
//
// The v2 (RVA-based) IMAGE_DELAYLOAD_DESCRIPTOR format is used; all fields
// are image-relative addresses. Layout within the returned flat blob:
//
//	Descriptor array   (N+1) × 32 bytes  [null sentinel at end]
//	Module handles     N × 8 bytes       (zeroed; loader writes TLS index here)
//	IAT arrays         one per DLL, null-terminated, 8 bytes/entry
//	INT arrays         identical in shape to IAT (initial values)
//	Hint/Name table    2-byte hint + null-terminated name, word-aligned
//	DLL name strings   null-terminated ASCII
func buildDelayIDATA(imports []DelayImport, baseRVA uint32, imageBase uint64) (*delayBuild, error) {
	if len(imports) == 0 {
		return &delayBuild{}, nil
	}
	N := len(imports)
	le := binary.LittleEndian

	const descSize = 32
	descBlockSize := uint32((N + 1) * descSize) // null sentinel

	// Module handle array: one 8-byte slot per DLL.
	modHandleBase := descBlockSize
	modHandleSize := uint32(N * 8)

	// IAT / INT arrays.
	iatBase := modHandleBase + modHandleSize
	iatOffsets := make([]uint32, N)
	cur := iatBase
	for i, imp := range imports {
		iatOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	iatEnd := cur

	intBase := iatEnd
	intOffsets := make([]uint32, N)
	for i, imp := range imports {
		intOffsets[i] = cur
		cur += uint32((len(imp.Symbols) + 1) * 8)
	}
	intEnd := cur

	// Hint/Name table.
	type hnLoc struct{ offset uint32 }
	hnLocs := make([][]hnLoc, N)
	hnBase := intEnd
	cur = hnBase
	for i, imp := range imports {
		hnLocs[i] = make([]hnLoc, len(imp.Symbols))
		for j, sym := range imp.Symbols {
			if sym.Name == "" {
				continue
			}
			hnLocs[i][j] = hnLoc{offset: cur}
			sz := 2 + uint32(len(sym.Name)) + 1
			if sz&1 != 0 {
				sz++
			}
			cur += sz
		}
	}
	hnEnd := cur

	// DLL name strings.
	dllNameOffsets := make([]uint32, N)
	for i, imp := range imports {
		dllNameOffsets[i] = cur
		cur += uint32(len(imp.DLL)) + 1
	}

	total := align32(cur, 4)
	buf := make([]byte, total)

	// Emit descriptor array.
	for i, imp := range imports {
		base := i * descSize
		const dlattrRva = uint32(1) // v2 format: all fields are RVAs
		le.PutUint32(buf[base+0:], dlattrRva)
		le.PutUint32(buf[base+4:], baseRVA+dllNameOffsets[i])  // DLL name RVA
		le.PutUint32(buf[base+8:], baseRVA+modHandleBase+uint32(i)*8) // module handle RVA
		le.PutUint32(buf[base+12:], baseRVA+iatOffsets[i])     // delay IAT RVA
		le.PutUint32(buf[base+16:], baseRVA+intOffsets[i])     // delay INT RVA
		le.PutUint32(buf[base+20:], 0) // BoundIAT RVA (unbound)
		le.PutUint32(buf[base+24:], 0) // UnloadIAT RVA (none)
		le.PutUint32(buf[base+28:], 0) // TimeDateStamp (unbound)
		_ = imp
	}
	// Null sentinel at [N] is already zero.

	// IAT and INT arrays (same content).
	for _, offsets := range [][]uint32{iatOffsets, intOffsets} {
		for i, imp := range imports {
			for j, sym := range imp.Symbols {
				var entry uint64
				if sym.Name == "" {
					entry = 1<<63 | uint64(sym.Ordinal)
				} else {
					entry = uint64(baseRVA + hnLocs[i][j].offset)
				}
				le.PutUint64(buf[offsets[i]+uint32(j)*8:], entry)
			}
		}
	}

	// Hint/Name records.
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

	// DLL name strings.
	for i, imp := range imports {
		copy(buf[dllNameOffsets[i]:], imp.DLL)
	}

	_ = fmt.Sprintf // keep import
	_ = iatEnd
	_ = hnEnd
	_ = imageBase

	return &delayBuild{
		descriptorData: buf[:descBlockSize],
		moduleHandles:  buf[modHandleBase : modHandleBase+modHandleSize],
		iatData:        buf[iatBase:iatEnd],
		intData:        buf[intBase:intEnd],
		hnData:         buf[hnBase:hnEnd],
		nameData:       buf[dllNameOffsets[0] : total],
		descriptorRVA:  baseRVA,
		descriptorSize: uint32((N + 1) * descSize),
	}, nil
}