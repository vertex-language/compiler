package pe

import (
	"encoding/binary"
	"sort"
)

type baseRelocEntry struct {
	rva uint32
	typ uint8
}

// buildBaseReloc emits the .reloc section from a list of base-relocation entries.
// Entries are grouped by 4 KiB page, with each block's header followed by
// 16-bit (type<<12 | pageOffset) words, padded to a DWORD boundary.
func buildBaseReloc(entries []baseRelocEntry) []byte {
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rva < entries[j].rva })

	le := binary.LittleEndian
	var buf []byte

	for i := 0; i < len(entries); {
		pageRVA    := entries[i].rva &^ 0xFFF
		blockStart := i
		for i < len(entries) && (entries[i].rva&^0xFFF) == pageRVA {
			i++
		}
		words := make([]uint16, len(entries[blockStart:i]))
		for j, e := range entries[blockStart:i] {
			words[j] = uint16(e.typ)<<12 | uint16(e.rva&0xFFF)
		}
		if len(words)&1 != 0 {
			words = append(words, 0) // pad to 4-byte DWORD boundary
		}
		blockSize := uint32(8 + len(words)*2)
		hdr := make([]byte, 8)
		le.PutUint32(hdr[0:], pageRVA)
		le.PutUint32(hdr[4:], blockSize)
		buf = append(buf, hdr...)
		for _, w := range words {
			buf = append(buf, byte(w), byte(w>>8))
		}
	}
	for len(buf)&3 != 0 {
		buf = append(buf, 0)
	}
	return buf
}