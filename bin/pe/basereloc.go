package pe

import (
	"encoding/binary"
	"sort"
)

type baseRelocEntry struct {
	rva uint32
	typ uint8
}

func buildBaseReloc(entries []baseRelocEntry) []byte {
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rva < entries[j].rva })

	var buf []byte
	le := binary.LittleEndian

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
			words = append(words, 0)
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
	for len(buf)&3 != 0 { buf = append(buf, 0) }
	return buf
}