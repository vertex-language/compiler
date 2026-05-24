package uefi

import (
	"encoding/binary"
	"sort"
)

// Base relocation types for the .reloc section (IMAGE_REL_BASED_*).
// These are the upper 4 bits of each 16-bit entry in a relocation block.
// The lower 12 bits are the byte offset within the block's 4 KB page.
const (
	// IMAGE_REL_BASED_ABSOLUTE is a no-op used to pad a block to a
	// 4-byte boundary when the entry count is odd. The builder inserts
	// these automatically.
	IMAGE_REL_BASED_ABSOLUTE uint8 = 0

	// IMAGE_REL_BASED_DIR64 applies the full 64-bit load-delta to the
	// 64-bit field at the given offset. Use this for every absolute
	// pointer in AMD64 and ARM64 UEFI images.
	IMAGE_REL_BASED_DIR64 uint8 = 10
)

// buildReloc generates the raw bytes of the .reloc section from the Reloc
// annotations on each section. sectionRVAs[i] is the image RVA of sections[i].
//
// The .reloc section is always emitted by the Builder, even when there are no
// annotated relocations, because UEFI firmware requires the base-relocation
// data directory entry to be present and valid.
//
// Format reference: PE/COFF spec §6.6 "The .reloc Section (Image Only)".
func buildReloc(sections []Section, sectionRVAs []uint32) []byte {
	// Collect all (absoluteRVA, type) pairs across every section.
	type entry struct {
		rva      uint32
		relocType uint8
	}
	var entries []entry
	for i, sec := range sections {
		for _, r := range sec.Relocs {
			entries = append(entries, entry{
				rva:      sectionRVAs[i] + r.Offset,
				relocType: r.Type,
			})
		}
	}
	if len(entries) == 0 {
		return nil
	}

	// Sort by RVA so we can group into 4 KB pages in one pass.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rva < entries[j].rva
	})

	// Build blocks. Each block covers one 4 KB page.
	//
	//   IMAGE_BASE_RELOCATION {
	//       VirtualAddress uint32  // page base RVA (entry.rva &^ 0xFFF)
	//       SizeOfBlock    uint32  // 8 + 2*numEntries (after padding)
	//   }
	//   entries[]  uint16  // (type<<12) | (rva & 0xFFF)
	//
	// Blocks must be DWORD-aligned: if the entry count is odd, append one
	// IMAGE_REL_BASED_ABSOLUTE (type=0, offset=0) padding entry.
	var buf []byte

	i := 0
	for i < len(entries) {
		pageBase := entries[i].rva &^ 0xFFF

		// Collect all entries that fall on this page.
		j := i
		for j < len(entries) && entries[j].rva&^0xFFF == pageBase {
			j++
		}
		pageEntries := entries[i:j]
		i = j

		// Pad to even count for DWORD alignment.
		needPad := len(pageEntries)%2 != 0

		blockSize := uint32(8 + 2*len(pageEntries))
		if needPad {
			blockSize += 2
		}

		block := make([]byte, blockSize)
		binary.LittleEndian.PutUint32(block[0:], pageBase)
		binary.LittleEndian.PutUint32(block[4:], blockSize)
		for k, e := range pageEntries {
			offset := e.rva & 0xFFF
			word := uint16(e.relocType)<<12 | uint16(offset)
			binary.LittleEndian.PutUint16(block[8+k*2:], word)
		}
		// Padding entry is already zero (IMAGE_REL_BASED_ABSOLUTE, offset 0).

		buf = append(buf, block...)
	}
	return buf
}