package pe

import (
	"encoding/binary"
)

// ── Public helpers to build common debug payloads ────────────────────────────

// BuildCodeViewPDB creates a CodeView PDB 7.0 (RSDS) debug entry for a
// PDB file, linking the image to its symbol information.
//
//	guid     – 16-byte GUID uniquely identifying the PDB build.
//	age      – incremented each time the PDB is updated without a new GUID.
//	pdbPath  – absolute or relative path to the .pdb file (null-terminated in output).
func BuildCodeViewPDB(guid [16]byte, age uint32, pdbPath string) DebugEntry {
	le := binary.LittleEndian
	// RSDS: signature(4) + GUID(16) + age(4) + null-terminated path
	payload := make([]byte, 4+16+4+len(pdbPath)+1)
	copy(payload[0:], "RSDS")
	copy(payload[4:], guid[:])
	le.PutUint32(payload[20:], age)
	copy(payload[24:], pdbPath)
	return DebugEntry{Type: IMAGE_DEBUG_TYPE_CODEVIEW, Data: payload}
}

// BuildReproEntry creates an IMAGE_DEBUG_TYPE_REPRO entry. When this entry is
// present, the TimeDateStamp in the COFF header and optional header must be set
// to the hash value embedded here (rather than a real timestamp), enabling
// reproducible (deterministic) builds.
//
// hash is typically a SHA-256 digest of the image content (32 bytes), but the
// spec only requires it be non-empty.
func BuildReproEntry(hash []byte) DebugEntry {
	le := binary.LittleEndian
	payload := make([]byte, 4+len(hash))
	le.PutUint32(payload[0:], uint32(len(hash)))
	copy(payload[4:], hash)
	return DebugEntry{Type: IMAGE_DEBUG_TYPE_REPRO, Data: payload}
}

// BuildVCFeatureEntry creates an IMAGE_DEBUG_TYPE_VC_FEATURE entry with the
// counts of pre-VC++11, C++11, /GS, /sdl, and guardN features used by the
// compiler. Pass all zeros for a minimal compliant entry.
func BuildVCFeatureEntry(preVC11, cppPlusPlus11, gs, sdl, guardN uint32) DebugEntry {
	le := binary.LittleEndian
	payload := make([]byte, 20)
	le.PutUint32(payload[0:], preVC11)
	le.PutUint32(payload[4:], cppPlusPlus11)
	le.PutUint32(payload[8:], gs)
	le.PutUint32(payload[12:], sdl)
	le.PutUint32(payload[16:], guardN)
	return DebugEntry{Type: IMAGE_DEBUG_TYPE_VC_FEATURE, Data: payload}
}

// ── Internal section builder ─────────────────────────────────────────────────

// buildDebugSectionSize returns the byte size of the .debug section that
// buildDebugSection would produce.
func buildDebugSectionSize(entries []DebugEntry) uint32 {
	sz := uint32(len(entries) * 28) // N × IMAGE_DEBUG_DIRECTORY entries
	for _, e := range entries {
		sz += align32(uint32(len(e.Data)), 4)
	}
	return sz
}

// buildDebugSection serializes all debug entries into a .debug section blob.
//
// Layout:
//
//	[0]        IMAGE_DEBUG_DIRECTORY[0]   28 bytes
//	[28]       IMAGE_DEBUG_DIRECTORY[1]   28 bytes
//	...
//	[N×28]     raw payload for entry 0
//	[N×28+sz0] raw payload for entry 1
//	...
//
// Each IMAGE_DEBUG_DIRECTORY.PointerToRawData is a section-relative offset;
// the linker must add the section file offset when computing the file pointer.
// AddressOfRawData is the section RVA + payload offset.
func buildDebugSection(entries []DebugEntry, sectionVA uint32) []byte {
	if len(entries) == 0 {
		return nil
	}
	le := binary.LittleEndian

	headerBlock := uint32(len(entries) * 28)
	totalSize := buildDebugSectionSize(entries)
	buf := make([]byte, totalSize)

	payloadOff := headerBlock
	for i, e := range entries {
		dirOff := uint32(i * 28)
		payloadSz := uint32(len(e.Data))
		paddedSz  := align32(payloadSz, 4)

		// IMAGE_DEBUG_DIRECTORY layout (28 bytes):
		//  [0]  Characteristics    uint32 – reserved, 0
		//  [4]  TimeDateStamp      uint32 – 0 for reproducible builds; otherwise build time
		//  [8]  MajorVersion       uint16
		//  [10] MinorVersion       uint16
		//  [12] Type               uint32
		//  [16] SizeOfData         uint32
		//  [20] AddressOfRawData   uint32 – RVA
		//  [24] PointerToRawData   uint32 – file offset (filled by serializer)
		le.PutUint32(buf[dirOff+0:], 0)
		le.PutUint32(buf[dirOff+4:], 0)
		le.PutUint16(buf[dirOff+8:], 0)
		le.PutUint16(buf[dirOff+10:], 0)
		le.PutUint32(buf[dirOff+12:], e.Type)
		le.PutUint32(buf[dirOff+16:], payloadSz)
		le.PutUint32(buf[dirOff+20:], sectionVA+payloadOff) // AddressOfRawData
		le.PutUint32(buf[dirOff+24:], payloadOff)           // PointerToRawData (section-relative; adjusted by serialize)

		copy(buf[payloadOff:], e.Data)
		payloadOff += paddedSz
	}
	return buf
}