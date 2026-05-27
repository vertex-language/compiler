package macho

import (
	"encoding/binary"
	"fmt"
)

// AMD64 relocation types (X86_64_RELOC_*).
const (
	x86RelocUnsigned  = 0
	x86RelocSigned    = 1
	x86RelocBranch    = 2
	x86RelocGotLoad   = 3
	x86RelocGot       = 4
	x86RelocSubtractor = 5
	x86RelocSigned1   = 6
	x86RelocSigned2   = 7
	x86RelocSigned4   = 8
	x86RelocTLV       = 9
)

// ARM64 relocation types (ARM64_RELOC_*).
const (
	arm64RelocUnsigned        = 0
	arm64RelocSubtractor      = 1
	arm64RelocBranch26        = 2
	arm64RelocPage21          = 3
	arm64RelocPageoff12       = 4
	arm64RelocGotLoadPage21   = 5
	arm64RelocGotLoadPageoff12 = 6
	arm64RelocPointerToGot    = 7
	arm64RelocTlvpLoadPage21  = 8
	arm64RelocTlvpLoadPageoff12 = 9
	arm64RelocAddend          = 10
)

// PatchRelocations applies all RELA-style relocations from every input object
// to the merged section data in-place.
// Must be called after ResolveSymbolAddresses and FinalizeStubs.
func PatchRelocations(
	arch uint32,
	layout *Layout,
	symtab *SymbolTable,
	stubs *StubTable,
	objects []*ObjectFile,
) error {
	for _, obj := range objects {
		if err := patchObject(arch, layout, symtab, stubs, obj); err != nil {
			return err
		}
	}
	return nil
}

func patchObject(
	arch uint32,
	layout *Layout,
	symtab *SymbolTable,
	stubs *StubTable,
	obj *ObjectFile,
) error {
	// Build a map from 0-based section index → MergedSection piece offset.
	// We need to know: for each input section, what is the base VAddr in the
	// merged output?
	type secInfo struct {
		ms          *MergedSection
		pieceOffset uint64 // offset of this contribution within ms
	}
	secInfos := make([]secInfo, len(obj.Sections))
	for i, rawSec := range obj.Sections {
		if rawSec == nil || i == 0 {
			continue
		}
		ms := layout.SectionByKey(rawSec.SegName, rawSec.SectName)
		if ms == nil {
			continue
		}
		// Find the piece.
		for _, p := range ms.Pieces {
			if p.Obj == obj && p.Sec == rawSec {
				secInfos[i] = secInfo{ms: ms, pieceOffset: p.Offset}
				break
			}
		}
	}

	// Pre-scan for ARM64_RELOC_ADDEND pairs.
	addends := make(map[int]int64) // reloc index → explicit addend
	if arch == ArchARM64 {
		for ri, rel := range obj.Relocs {
			if rel.Type == arm64RelocAddend {
				// r_symbolnum encodes the 24-bit signed addend
				addend := int64(int32(rel.SymIdx << 8) >> 8)
				addends[ri+1] = addend
			}
		}
	}

	for ri, rel := range obj.Relocs {
		if rel.Type == arm64RelocAddend {
			continue // consumed by the next reloc
		}

		secIdx := rel.SectionIdx + 1 // back to 1-based
		if secIdx >= len(secInfos) || secInfos[secIdx].ms == nil {
			continue
		}
		si := secInfos[secIdx]
		ms := si.ms

		// Patch offset within the merged section.
		patchOff := si.pieceOffset + uint64(rel.Offset)
		if int(patchOff) >= len(ms.Data) {
			return fmt.Errorf("reloc offset %d out of bounds in %s,%s",
				patchOff, ms.SegName, ms.SectName)
		}

		// Patch site virtual address.
		patchVA := ms.VAddr + patchOff

		// Resolve symbol address.
		var symVA uint64
		var symName string
		if rel.Extern {
			if int(rel.SymIdx) >= len(obj.Symbols) {
				return fmt.Errorf("reloc symbol index %d out of bounds", rel.SymIdx)
			}
			raw := obj.Symbols[rel.SymIdx]
			symName = raw.Name
			rs := symtab.Lookup(symName)
			if rs == nil {
				return fmt.Errorf("unresolved symbol %q in relocation", symName)
			}
			symVA = rs.VAddr
		} else {
			// Local reloc: r_symbolnum is 1-based section number.
			targSecIdx := int(rel.SectNum)
			if targSecIdx > 0 && targSecIdx < len(secInfos) && secInfos[targSecIdx].ms != nil {
				ts := secInfos[targSecIdx]
				symVA = ts.ms.VAddr + ts.pieceOffset
			}
		}

		// Explicit addend for ARM64_RELOC_ADDEND.
		explicitAddend, hasExplicit := addends[ri]

		var err error
		switch arch {
		case ArchAMD64:
			err = applyAMD64Reloc(ms.Data, patchOff, patchVA, symVA, symName, rel, stubs)
		case ArchARM64:
			addend := int64(0)
			if hasExplicit {
				addend = explicitAddend
			}
			err = applyARM64Reloc(ms.Data, patchOff, patchVA, symVA, symName, rel, stubs, addend)
		default:
			return fmt.Errorf("unsupported architecture 0x%x", arch)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", obj.Path, err)
		}
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// AMD64
// ──────────────────────────────────────────────────────────────────────────────

func applyAMD64Reloc(
	data []byte,
	patchOff, patchVA, symVA uint64,
	symName string,
	rel *RawReloc,
	stubs *StubTable,
) error {
	// Embedded addend: read from the current bytes at the patch site.
	var addend int64
	switch rel.Length {
	case 2:
		addend = int64(int32(binary.LittleEndian.Uint32(data[patchOff:])))
	case 3:
		addend = int64(int64(binary.LittleEndian.Uint64(data[patchOff:])))
	}

	switch rel.Type {
	case x86RelocUnsigned:
		result := symVA + uint64(addend)
		if rel.Length == 3 {
			binary.LittleEndian.PutUint64(data[patchOff:], result)
		} else {
			binary.LittleEndian.PutUint32(data[patchOff:], uint32(result))
		}

	case x86RelocSigned, x86RelocSigned1, x86RelocSigned2, x86RelocSigned4:
		implicit := int64(0)
		switch rel.Type {
		case x86RelocSigned1:
			implicit = -1
		case x86RelocSigned2:
			implicit = -2
		case x86RelocSigned4:
			implicit = -4
		}
		result := int64(symVA) + addend + implicit - int64(patchVA)
		v := int32(result)
		if int64(v) != result {
			return fmt.Errorf("AMD64 SIGNED reloc: value 0x%x overflows int32", result)
		}
		binary.LittleEndian.PutUint32(data[patchOff:], uint32(v))

	case x86RelocBranch:
		// For dylib symbols, redirect through stub.
		target := symVA
		if symName != "" {
			if stubVA, ok := stubs.StubVAddrFor(symName); ok {
				target = stubVA
			}
		}
		result := int64(target) + addend - int64(patchVA)
		v := int32(result)
		if int64(v) != result {
			return fmt.Errorf("AMD64 BRANCH reloc to %q: value 0x%x overflows int32", symName, result)
		}
		binary.LittleEndian.PutUint32(data[patchOff:], uint32(v))

	case x86RelocGotLoad, x86RelocGot:
		// Reference to GOT slot.
		gotVA := symVA
		if symName != "" {
			if gv, ok := stubs.GotVAddrFor(symName); ok {
				gotVA = gv
			}
		}
		result := int64(gotVA) + addend - int64(patchVA)
		v := int32(result)
		if int64(v) != result {
			return fmt.Errorf("AMD64 GOT reloc to %q: value 0x%x overflows int32", symName, result)
		}
		binary.LittleEndian.PutUint32(data[patchOff:], uint32(v))

	case x86RelocSubtractor:
		// SUBTRACTOR must be followed by UNSIGNED; handled in pair — skip here.
		// (The pair logic would be in the outer loop; simplification: subtract only.)
		binary.LittleEndian.PutUint64(data[patchOff:], uint64(-int64(symVA)))

	case x86RelocTLV:
		// TLV descriptor: treat like GOT_LOAD for now.
		gotVA := symVA
		result := int64(gotVA) + addend - int64(patchVA)
		v := int32(result)
		binary.LittleEndian.PutUint32(data[patchOff:], uint32(v))
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// ARM64
// ──────────────────────────────────────────────────────────────────────────────

func applyARM64Reloc(
	data []byte,
	patchOff, patchVA, symVA uint64,
	symName string,
	rel *RawReloc,
	stubs *StubTable,
	explicitAddend int64,
) error {
	insn := binary.LittleEndian.Uint32(data[patchOff:])

	switch rel.Type {
	case arm64RelocUnsigned:
		result := symVA + uint64(explicitAddend)
		if rel.Length == 3 {
			binary.LittleEndian.PutUint64(data[patchOff:], result)
		} else {
			binary.LittleEndian.PutUint32(data[patchOff:], uint32(result))
		}

	case arm64RelocBranch26:
		// BL / B: 26-bit signed PC-relative, in units of 4 bytes.
		target := symVA
		if symName != "" {
			if stubVA, ok := stubs.StubVAddrFor(symName); ok {
				target = stubVA
			}
		}
		delta := (int64(target) + explicitAddend - int64(patchVA)) / 4
		if delta < -(1<<25) || delta > (1<<25)-1 {
			return fmt.Errorf("ARM64 BRANCH26 to %q: target out of range ±128 MiB", symName)
		}
		insn = (insn & 0xFC000000) | uint32(delta&0x3FFFFFF)
		binary.LittleEndian.PutUint32(data[patchOff:], insn)

	case arm64RelocPage21, arm64RelocTlvpLoadPage21:
		// ADRP: patch the 21-bit page-relative immediate.
		target := symVA
		if rel.Type == arm64RelocGotLoadPage21 {
			if symName != "" {
				if gv, ok := stubs.GotVAddrFor(symName); ok {
					target = gv
				}
			}
		}
		pageDelta := int64(target>>12) + (explicitAddend >> 12) - int64(patchVA>>12)
		immlo := uint32(pageDelta & 0x3)
		immhi := uint32((pageDelta >> 2) & 0x7FFFF)
		insn = (insn & 0x9F00001F) | (immlo << 29) | (immhi << 5)
		binary.LittleEndian.PutUint32(data[patchOff:], insn)

	case arm64RelocGotLoadPage21:
		// ADRP to GOT slot page.
		gotVA := symVA
		if symName != "" {
			if gv, ok := stubs.GotVAddrFor(symName); ok {
				gotVA = gv
			}
		}
		pageDelta := int64(gotVA>>12) - int64(patchVA>>12)
		immlo := uint32(pageDelta & 0x3)
		immhi := uint32((pageDelta >> 2) & 0x7FFFF)
		insn = (insn & 0x9F00001F) | (immlo << 29) | (immhi << 5)
		binary.LittleEndian.PutUint32(data[patchOff:], insn)

	case arm64RelocPageoff12, arm64RelocTlvpLoadPageoff12:
		// ADD / LDR / STR: 12-bit page offset in bits [21:10].
		pageOff := (symVA + uint64(explicitAddend)) & 0xFFF
		// Detect LDR/STR (bit 27=1 for load/store) vs ADD.
		if insn&0x3B000000 == 0x39000000 {
			// Load/store: scale the offset by the access size (bits 31:30).
			size := insn >> 30
			pageOff >>= size
		}
		insn = (insn & 0xFFC003FF) | (uint32(pageOff) << 10)
		binary.LittleEndian.PutUint32(data[patchOff:], insn)

	case arm64RelocGotLoadPageoff12:
		// LDR x<n>, [x<m>, #pageoff]: 12-bit GOT slot offset, scaled by 8.
		gotVA := symVA
		if symName != "" {
			if gv, ok := stubs.GotVAddrFor(symName); ok {
				gotVA = gv
			}
		}
		pageOff := (gotVA & 0xFFF) >> 3 // ÷8 for 64-bit LDR
		insn = (insn & 0xFFC003FF) | (uint32(pageOff) << 10)
		binary.LittleEndian.PutUint32(data[patchOff:], insn)

	case arm64RelocPointerToGot:
		// 32-bit PC-relative to GOT slot.
		gotVA := symVA
		if symName != "" {
			if gv, ok := stubs.GotVAddrFor(symName); ok {
				gotVA = gv
			}
		}
		result := int32(int64(gotVA) - int64(patchVA))
		binary.LittleEndian.PutUint32(data[patchOff:], uint32(result))

	case arm64RelocSubtractor:
		// SUBTRACTOR + UNSIGNED pair: the SUBTRACTOR stores -symVA.
		binary.LittleEndian.PutUint64(data[patchOff:], uint64(-int64(symVA)))

	case arm64RelocAddend:
		// Consumed by the caller before dispatching; should not arrive here.
	}

	return nil
}