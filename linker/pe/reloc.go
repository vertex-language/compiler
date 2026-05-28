package pe

import (
	"encoding/binary"
	"fmt"
)

// patchRelocations applies all COFF relocations in layout to the merged
// section data, resolving symbol addresses from symtab.
func patchRelocations(machine uint16, layout *Layout, symtab *SymbolTable, imageBase uint64) error {
	// Build a fast lookup: (obj, 0-based secIdx) → (mergedSection, pieceOffset).
	type locKey struct {
		obj    *ObjectFile
		secIdx int
	}
	type locVal struct {
		ms  *MergedSection
		off uint32
	}
	lut := make(map[locKey]locVal)
	for _, ms := range layout.Sections {
		for _, p := range ms.Pieces {
			lut[locKey{p.Obj, p.Sec.Index}] = locVal{ms, p.Offset}
		}
	}

	// resolveVA returns the final VA for a raw symbol in obj.
	resolveVA := func(obj *ObjectFile, rawSym *RawSymbol) (uint64, error) {
		// Named symbol: prefer global table (handles imports, thunks, etc.).
		if rawSym.Name != "" {
			if gs, ok := symtab.syms[rawSym.Name]; ok && gs.va != 0 {
				return gs.va, nil
			}
		}
		// Section symbol or local: compute from section VA + piece offset + sym value.
		if rawSym.SectionNumber > 0 {
			secIdx := int(rawSym.SectionNumber) - 1
			if v, ok := lut[locKey{obj, secIdx}]; ok {
				return imageBase + uint64(v.ms.VAddr) + uint64(v.off) + uint64(rawSym.Value), nil
			}
			return 0, fmt.Errorf("section %d not found in layout", rawSym.SectionNumber)
		}
		if rawSym.SectionNumber == -1 { // IMAGE_SYM_ABSOLUTE
			return uint64(rawSym.Value), nil
		}
		if rawSym.Name != "" {
			return 0, fmt.Errorf("unresolved symbol %q", rawSym.Name)
		}
		return 0, fmt.Errorf("unresolvable anonymous symbol (section %d)", rawSym.SectionNumber)
	}

	// sectionBaseVA returns the start VA of whichever output section contains addr.
	sectionBaseVA := func(addr uint64) uint64 {
		for _, ms := range layout.Sections {
			start := imageBase + uint64(ms.VAddr)
			end := start + uint64(ms.VirtualSize)
			if addr >= start && addr < end {
				return start
			}
		}
		return 0
	}

	for _, ms := range layout.Sections {
		if ms.Data == nil {
			continue // BSS — no data to patch
		}
		for _, p := range ms.Pieces {
			obj := p.Obj
			sec := p.Sec

			for _, rel := range sec.Relocs {
				if int(rel.SymIndex) >= len(obj.Symbols) {
					return fmt.Errorf("%s: reloc sym index %d out of range",
						obj.Path, rel.SymIndex)
				}
				rawSym := obj.Symbols[rel.SymIndex]

				symVA, err := resolveVA(obj, rawSym)
				if err != nil {
					return fmt.Errorf("%s: section %s+%#x: %w",
						obj.Path, sec.Name, rel.Offset, err)
				}

				// Byte offset within the merged section's Data slice.
				patchOff := int(p.Offset) + int(rel.Offset)
				if patchOff < 0 || patchOff > len(ms.Data) {
					continue
				}
				// VA of the patch site.
				patchVA := imageBase + uint64(ms.VAddr) + uint64(patchOff)

				var applyErr error
				switch machine {
				case 0x8664: // AMD64
					applyErr = applyAMD64(ms.Data, patchOff, rel.Type,
						symVA, patchVA, imageBase, sectionBaseVA)
				case 0xAA64, 0xA641: // ARM64 / ARM64EC
					applyErr = applyARM64(ms.Data, patchOff, rel.Type,
						symVA, patchVA, imageBase, sectionBaseVA)
				}
				if applyErr != nil {
					return fmt.Errorf("%s: %s+%#x: machine %#x reloc %#x: %w",
						obj.Path, sec.Name, rel.Offset, machine, rel.Type, applyErr)
				}
			}
		}
	}
	return nil
}

// ─── AMD64 relocations ────────────────────────────────────────────────────────

func applyAMD64(data []byte, off int, typ uint16,
	symVA, patchVA, imageBase uint64,
	secBaseVA func(uint64) uint64) error {

	switch typ {
	case 0x0000: // IMAGE_REL_AMD64_ABSOLUTE — no-op / padding
		return nil

	case 0x0001: // IMAGE_REL_AMD64_ADDR64 — 8-byte VA
		if off+8 > len(data) {
			return nil
		}
		addend := int64(binary.LittleEndian.Uint64(data[off:]))
		binary.LittleEndian.PutUint64(data[off:], uint64(int64(symVA)+addend))

	case 0x0002: // IMAGE_REL_AMD64_ADDR32 — 4-byte VA (truncated)
		if off+4 > len(data) {
			return nil
		}
		addend := int32(binary.LittleEndian.Uint32(data[off:]))
		binary.LittleEndian.PutUint32(data[off:], uint32(int64(symVA)+int64(addend)))

	case 0x0003: // IMAGE_REL_AMD64_ADDR32NB — 4-byte RVA (image-relative)
		if off+4 > len(data) {
			return nil
		}
		addend := int32(binary.LittleEndian.Uint32(data[off:]))
		rva := int64(symVA) - int64(imageBase) + int64(addend)
		binary.LittleEndian.PutUint32(data[off:], uint32(rva))

	case 0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009:
		// IMAGE_REL_AMD64_REL32 and REL32_1 … REL32_5.
		// PC after the displacement field = patchVA + 4 + (typ - 0x0004).
		if off+4 > len(data) {
			return nil
		}
		extra := int64(typ - 0x0004)
		addend := int32(binary.LittleEndian.Uint32(data[off:]))
		rel := int64(symVA) - int64(patchVA) - 4 - extra + int64(addend)
		binary.LittleEndian.PutUint32(data[off:], uint32(int32(rel)))

	case 0x000A: // IMAGE_REL_AMD64_SECTION — 2-byte 1-based section index (debug only)
		return nil // leave as-is; section indices are stable

	case 0x000B: // IMAGE_REL_AMD64_SECREL — 4-byte section-relative offset
		if off+4 > len(data) {
			return nil
		}
		base := secBaseVA(symVA)
		addend := int32(binary.LittleEndian.Uint32(data[off:]))
		secRel := int64(symVA) - int64(base) + int64(addend)
		binary.LittleEndian.PutUint32(data[off:], uint32(int32(secRel)))

	case 0x000C: // IMAGE_REL_AMD64_SECREL7 — 7-bit section-relative (debug)
		return nil

	case 0x000D: // IMAGE_REL_AMD64_TOKEN — CLR metadata token
		return nil

	default:
		// Unknown — skip silently so new object formats don't hard-fail.
		return nil
	}
	return nil
}

// ─── ARM64 relocations ────────────────────────────────────────────────────────

func applyARM64(data []byte, off int, typ uint16,
	symVA, patchVA, imageBase uint64,
	secBaseVA func(uint64) uint64) error {

	read32 := func() uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	write32 := func(v uint32) { binary.LittleEndian.PutUint32(data[off:], v) }

	switch typ {
	case 0x0000: // IMAGE_REL_ARM64_ABSOLUTE — no-op
		return nil

	case 0x0001: // IMAGE_REL_ARM64_ADDR32 — 4-byte VA
		if off+4 > len(data) {
			return nil
		}
		addend := int32(read32())
		write32(uint32(int64(symVA) + int64(addend)))

	case 0x0002: // IMAGE_REL_ARM64_ADDR32NB — 4-byte RVA
		if off+4 > len(data) {
			return nil
		}
		addend := int32(read32())
		write32(uint32(int64(symVA) - int64(imageBase) + int64(addend)))

	case 0x0003: // IMAGE_REL_ARM64_BRANCH26 — B / BL 26-bit displacement
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		diff := int64(symVA) - int64(patchVA)
		imm26 := uint32(int32(diff>>2)) & 0x03FFFFFF
		instr = (instr &^ 0x03FFFFFF) | imm26
		write32(instr)

	case 0x0004: // IMAGE_REL_ARM64_PAGEBASE_REL21 — ADRP 21-bit page-relative
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		// imm21 = number of pages from PC page to target page.
		pageDiff := int64(symVA>>12) - int64(patchVA>>12)
		imm21 := int32(pageDiff)
		immlo := uint32(imm21) & 0x3
		immhi := (uint32(imm21) >> 2) & 0x7FFFF
		// Clear old immediate: bits [30:29]=immlo, bits [23:5]=immhi.
		instr &^= (0x3 << 29) | (0x7FFFF << 5)
		instr |= (immlo << 29) | (immhi << 5)
		write32(instr)

	case 0x0005: // IMAGE_REL_ARM64_REL21 — ADR 21-bit PC-relative
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		diff := int64(symVA) - int64(patchVA)
		imm21 := int32(diff)
		immlo := uint32(imm21) & 0x3
		immhi := (uint32(imm21) >> 2) & 0x7FFFF
		instr &^= (0x3 << 29) | (0x7FFFF << 5)
		instr |= (immlo << 29) | (immhi << 5)
		write32(instr)

	case 0x0006: // IMAGE_REL_ARM64_PAGEOFFSET_12A — ADD imm12 = page offset
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		pageOff := uint32(symVA & 0xFFF)
		instr = (instr &^ (0xFFF << 10)) | (pageOff << 10)
		write32(instr)

	case 0x0007: // IMAGE_REL_ARM64_PAGEOFFSET_12L — LDR/STR scaled imm12
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		// Scale encoded in bits [31:30] of the instruction.
		scale := (instr >> 30) & 0x3
		pageOff := uint32(symVA&0xFFF) >> scale
		instr = (instr &^ (0xFFF << 10)) | (pageOff << 10)
		write32(instr)

	case 0x0008: // IMAGE_REL_ARM64_SECREL — 4-byte section-relative offset
		if off+4 > len(data) {
			return nil
		}
		base := secBaseVA(symVA)
		addend := int32(read32())
		write32(uint32(int64(symVA) - int64(base) + int64(addend)))

	case 0x0009: // IMAGE_REL_ARM64_SECREL_LOW12A — ADD low 12 bits of secrel
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		base := secBaseVA(symVA)
		secRel := uint32(symVA - base)
		instr = (instr &^ (0xFFF << 10)) | ((secRel & 0xFFF) << 10)
		write32(instr)

	case 0x000A: // IMAGE_REL_ARM64_SECREL_HIGH12A — ADD bits [23:12] of secrel
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		base := secBaseVA(symVA)
		secRel := uint32(symVA - base)
		instr = (instr &^ (0xFFF << 10)) | (((secRel >> 12) & 0xFFF) << 10)
		write32(instr)

	case 0x000B: // IMAGE_REL_ARM64_SECREL_LOW12L — LDR/STR low 12 bits of secrel
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		base := secBaseVA(symVA)
		secRel := uint32(symVA - base)
		scale := (instr >> 30) & 0x3
		instr = (instr &^ (0xFFF << 10)) | (((secRel & 0xFFF) >> scale) << 10)
		write32(instr)

	case 0x000D: // IMAGE_REL_ARM64_SECTION — 2-byte section index (debug)
		return nil

	case 0x000E: // IMAGE_REL_ARM64_ADDR64 — 8-byte VA
		if off+8 > len(data) {
			return nil
		}
		addend := int64(binary.LittleEndian.Uint64(data[off:]))
		binary.LittleEndian.PutUint64(data[off:], uint64(int64(symVA)+addend))

	case 0x000F: // IMAGE_REL_ARM64_BRANCH19 — CBZ/CBNZ/B.cond 19-bit displacement
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		diff := int64(symVA) - int64(patchVA)
		imm19 := (uint32(int32(diff>>2))) & 0x7FFFF
		instr = (instr &^ (0x7FFFF << 5)) | (imm19 << 5)
		write32(instr)

	case 0x0010: // IMAGE_REL_ARM64_BRANCH14 — TBZ/TBNZ 14-bit displacement
		if off+4 > len(data) {
			return nil
		}
		instr := read32()
		diff := int64(symVA) - int64(patchVA)
		imm14 := (uint32(int32(diff>>2))) & 0x3FFF
		instr = (instr &^ (0x3FFF << 5)) | (imm14 << 5)
		write32(instr)

	case 0x0011: // IMAGE_REL_ARM64_REL32 — 4-byte PC-relative (like AMD64 REL32)
		if off+4 > len(data) {
			return nil
		}
		addend := int32(read32())
		rel := int32(int64(symVA) - int64(patchVA) - 4 + int64(addend))
		write32(uint32(rel))

	default:
		return nil
	}
	return nil
}