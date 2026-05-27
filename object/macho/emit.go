// object/macho/emit.go

package macho

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// emit serialises o into a valid MH_OBJECT Mach-O byte slice.
//
// File layout:
//
//	[ mach_header_64            ]  32 bytes
//	[ LC_SEGMENT_64             ]  72 + nsects×80 bytes
//	  [ section_64 × nsects     ]  one per user section
//	[ LC_SYMTAB                 ]  24 bytes
//	[ non-zerofill section data ]  each blob aligned to section.Align
//	[ relocation entries        ]  8 bytes each, one block per section with relocs
//	[ nlist_64 symbol entries   ]  16 bytes each; locals precede globals
//	[ string table              ]  NUL-terminated symbol name strings
//
// All section addresses in the unnamed MH_OBJECT segment are set to zero.
// Symbol n_value for N_SECT symbols encodes the byte offset within the
// section, consistent with how linker/macho.ResolveSymbolAddresses consumes it.
func emit(o *Object) ([]byte, error) {
	le := binary.LittleEndian
	nsects := len(o.sections)

	// ── Step 1: section name → 1-based ordinal ───────────────────────────────

	sectOrdinal := make(map[string]uint32, nsects)
	for i, s := range o.sections {
		sectOrdinal[s.SectName] = uint32(i + 1)
	}

	// ── Step 2: assemble full symbol list ────────────────────────────────────
	//
	// Any symbol referenced by an extern reloc but not explicitly added via
	// AddSymbol is implicitly undefined (resolved by the linker).

	definedNames := make(map[string]bool, len(o.symbols))
	for _, sym := range o.symbols {
		definedNames[sym.Name] = true
	}
	allSyms := make([]Symbol, len(o.symbols))
	copy(allSyms, o.symbols)
	for _, r := range o.relocs {
		if r.Extern && !definedNames[r.Symbol] {
			allSyms = append(allSyms, Symbol{Name: r.Symbol, Global: true})
			definedNames[r.Symbol] = true
		}
	}

	// nlist_64 requires: all local (STB_LOCAL) entries precede the first global.
	var locals, globals []Symbol
	for _, sym := range allSyms {
		if sym.Global || sym.Weak {
			globals = append(globals, sym)
		} else {
			locals = append(locals, sym)
		}
	}
	ordered := append(locals, globals...) // nlist order: locals first

	// ── Step 3: build symbol string table ────────────────────────────────────

	strtab := []byte{0} // index 0 = null byte (empty-string sentinel)
	strtabCache := map[string]uint32{"": 0}
	addStr := func(name string) uint32 {
		if off, ok := strtabCache[name]; ok {
			return off
		}
		off := uint32(len(strtab))
		strtab = append(strtab, name...)
		strtab = append(strtab, 0)
		strtabCache[name] = off
		return off
	}

	type symEntry struct {
		sym    Symbol
		strIdx uint32
		sectNo uint8 // 1-based section number; 0 for undef / abs
	}
	entries := make([]symEntry, len(ordered))
	symIdxMap := make(map[string]uint32, len(ordered))
	for i, sym := range ordered {
		sectNo := uint8(0)
		if !sym.Abs && sym.SectionName != "" {
			if n, ok := sectOrdinal[sym.SectionName]; ok {
				sectNo = uint8(n)
			}
		}
		entries[i] = symEntry{sym: sym, strIdx: addStr(sym.Name), sectNo: sectNo}
		symIdxMap[sym.Name] = uint32(i)
	}

	// ── Step 4: group relocations by section ─────────────────────────────────

	relocsBySect := make(map[string][]Reloc, nsects)
	for _, r := range o.relocs {
		relocsBySect[r.SectionName] = append(relocsBySect[r.SectionName], r)
	}

	// ── Step 5: compute file layout ──────────────────────────────────────────

	// Load-command block size.
	segCmdSize := uint32(segCmd64Size + nsects*section64Size)
	sizeofcmds := segCmdSize + symtabCmdSize

	// Running file position; starts right after the header + load commands.
	filePos := uint32(machHeader64Size) + sizeofcmds

	type secLayout struct {
		fileOff  uint32 // file offset of section data (0 for zerofill)
		size     uint32 // byte count of section data (logical size for zerofill)
		relocOff uint32 // file offset of this section's reloc entries (0 if none)
		nreloc   uint32
	}
	sl := make([]secLayout, nsects)

	// Non-zerofill section data.
	for i, s := range o.sections {
		if isZerofill(s) {
			sl[i].size = uint32(s.nobits)
			continue
		}
		filePos = alignUp32(filePos, uint32(s.Align))
		sl[i].fileOff = filePos
		sl[i].size = uint32(s.buf.Len())
		filePos += sl[i].size
	}

	// Relocation entries (4-byte aligned, one block per section).
	filePos = alignUp32(filePos, 4)
	for i, s := range o.sections {
		rels := relocsBySect[s.SectName]
		if len(rels) == 0 {
			continue
		}
		sl[i].relocOff = filePos
		sl[i].nreloc = uint32(len(rels))
		filePos += sl[i].nreloc * relocEntrySize
	}

	// Symbol table.
	filePos = alignUp32(filePos, 4)
	symoff := filePos
	nsyms := uint32(len(entries))
	filePos += nsyms * nlist64Size

	// String table.
	stroff := filePos
	strsize := uint32(len(strtab))

	totalSize := int(stroff) + int(strsize)

	// ── Step 6: compute segment vmsize and file extents ──────────────────────

	// vmsize: sum of aligned section sizes in address order (addr = 0 for all).
	var vmsize uint64
	for _, s := range o.sections {
		a := s.Align
		if a < 1 {
			a = 1
		}
		vmsize = alignUp64(vmsize, a)
		if isZerofill(s) {
			vmsize += s.nobits
		} else {
			vmsize += uint64(s.buf.Len())
		}
	}

	// Segment fileoff / filesize: span of all non-zerofill section data.
	segFileOff := uint32(0)
	segFileEnd := uint32(0)
	for i, s := range o.sections {
		if isZerofill(s) || sl[i].size == 0 {
			continue
		}
		if segFileOff == 0 {
			segFileOff = sl[i].fileOff
		}
		if end := sl[i].fileOff + sl[i].size; end > segFileEnd {
			segFileEnd = end
		}
	}
	segFileSize := uint32(0)
	if segFileEnd > segFileOff {
		segFileSize = segFileEnd - segFileOff
	}

	// ── Step 7: fill buffer ───────────────────────────────────────────────────

	buf := make([]byte, totalSize)

	// ── mach_header_64 ────────────────────────────────────────────────────────

	le.PutUint32(buf[0:], mhMagic64)
	le.PutUint32(buf[4:], uint32(o.arch))
	le.PutUint32(buf[8:], o.arch.cpuSubtype())
	le.PutUint32(buf[12:], mhObject)
	le.PutUint32(buf[16:], 2) // ncmds: LC_SEGMENT_64 + LC_SYMTAB
	le.PutUint32(buf[20:], sizeofcmds)
	le.PutUint32(buf[24:], MHSubsectionsViaSym)
	// buf[28:32] reserved = 0

	// ── LC_SEGMENT_64 ─────────────────────────────────────────────────────────

	off := machHeader64Size
	le.PutUint32(buf[off+0:], lcSegment64)
	le.PutUint32(buf[off+4:], segCmdSize)
	// segname[16] at off+8: all zeros — single unnamed segment in MH_OBJECT
	le.PutUint64(buf[off+24:], 0)                    // vmaddr
	le.PutUint64(buf[off+32:], vmsize)               // vmsize
	le.PutUint64(buf[off+40:], uint64(segFileOff))   // fileoff
	le.PutUint64(buf[off+48:], uint64(segFileSize))  // filesize
	le.PutUint32(buf[off+56:], 7)                    // maxprot  VM_PROT_ALL
	le.PutUint32(buf[off+60:], 7)                    // initprot VM_PROT_ALL
	le.PutUint32(buf[off+64:], uint32(nsects))       // nsects
	// flags = 0
	off += segCmd64Size

	// ── section_64 entries ────────────────────────────────────────────────────

	for i, s := range o.sections {
		base := off
		// sectname[16] and segname[16]: copy, leaving remainder as zeros.
		copy(buf[base+0:base+16], s.SectName)
		copy(buf[base+16:base+32], s.SegName)
		// addr (8) at base+32: 0 — MH_OBJECT sections have no fixed address
		le.PutUint64(buf[base+40:], uint64(sl[i].size)) // size
		le.PutUint32(buf[base+48:], sl[i].fileOff)      // offset (0 for zerofill)
		// align as log₂; s.Align is always ≥ 1 after getOrAdd.
		alignLog := uint32(0)
		if s.Align > 1 {
			alignLog = uint32(bits.Len64(s.Align) - 1)
		}
		le.PutUint32(buf[base+52:], alignLog)
		le.PutUint32(buf[base+56:], sl[i].relocOff) // reloff
		le.PutUint32(buf[base+60:], sl[i].nreloc)   // nreloc
		le.PutUint32(buf[base+64:], s.Attrs|s.Type) // flags
		// reserved1/2/3 at base+68..79: already zero
		off += section64Size
	}

	// ── LC_SYMTAB ─────────────────────────────────────────────────────────────

	le.PutUint32(buf[off+0:], lcSymtab)
	le.PutUint32(buf[off+4:], symtabCmdSize)
	le.PutUint32(buf[off+8:], symoff)
	le.PutUint32(buf[off+12:], nsyms)
	le.PutUint32(buf[off+16:], stroff)
	le.PutUint32(buf[off+20:], strsize)

	// ── Non-zerofill section data ─────────────────────────────────────────────

	for i, s := range o.sections {
		if isZerofill(s) {
			continue
		}
		copy(buf[sl[i].fileOff:], s.buf.Bytes())
	}

	// ── Relocation entries ────────────────────────────────────────────────────
	//
	// relocation_info wire format (8 bytes):
	//   r_address  uint32  — byte offset within the section
	//   packed     uint32  — r_symbolnum[23:0] | r_pcrel[24] |
	//                        r_length[26:25] | r_extern[27] | r_type[31:28]

	for i, s := range o.sections {
		rels := relocsBySect[s.SectName]
		if len(rels) == 0 {
			continue
		}
		roff := int(sl[i].relocOff)
		for _, r := range rels {
			var symnum uint32
			if r.Extern {
				idx, ok := symIdxMap[r.Symbol]
				if !ok {
					return nil, fmt.Errorf(
						"object/macho emit: reloc in %q references unknown symbol %q",
						s.SectName, r.Symbol,
					)
				}
				symnum = idx
			} else {
				symnum = r.SectOrdinal
			}

			pcrelBit := uint32(0)
			if r.PCRel {
				pcrelBit = 1
			}
			externBit := uint32(0)
			if r.Extern {
				externBit = 1
			}
			packed := (symnum & 0x00FFFFFF) |
				(pcrelBit << 24) |
				(uint32(r.Length) << 25) |
				(externBit << 27) |
				(uint32(r.Type) << 28)

			le.PutUint32(buf[roff+0:], r.Offset)
			le.PutUint32(buf[roff+4:], packed)
			roff += relocEntrySize
		}
	}

	// ── nlist_64 symbol entries ───────────────────────────────────────────────
	//
	// nlist_64 wire format (16 bytes):
	//   n_strx  uint32  — byte offset into string table
	//   n_type  uint8   — type flags (N_STAB | N_PEXT | N_TYPE | N_EXT)
	//   n_sect  uint8   — 1-based section number; 0 = N_UNDF / N_ABS
	//   n_desc  uint16  — symbol descriptor flags
	//   n_value uint64  — for N_SECT: byte offset within section; for N_ABS: value

	soff := int(symoff)
	for _, e := range entries {
		sym := e.sym

		var ntype uint8
		var nvalue uint64
		switch {
		case sym.Abs:
			ntype = nAbs
			nvalue = sym.AbsValue
		case e.sectNo != 0:
			ntype = nSect
			nvalue = sym.Offset // byte offset within the section
		default:
			ntype = nUndf
		}
		if sym.Global {
			ntype |= nExt
		}

		var ndesc uint16
		if sym.Weak {
			if e.sectNo != 0 {
				ndesc |= nWeakDef // defined weak
			} else {
				ndesc |= nWeakRef // weak reference
			}
		}

		le.PutUint32(buf[soff+0:], e.strIdx) // n_strx
		buf[soff+4] = ntype                  // n_type
		buf[soff+5] = e.sectNo              // n_sect
		le.PutUint16(buf[soff+6:], ndesc)   // n_desc
		le.PutUint64(buf[soff+8:], nvalue)  // n_value
		soff += nlist64Size
	}

	// ── String table ──────────────────────────────────────────────────────────

	copy(buf[stroff:], strtab)

	return buf, nil
}

// ── Alignment helpers ─────────────────────────────────────────────────────────

// alignUp32 rounds v up to the nearest multiple of align (power of two).
func alignUp32(v, align uint32) uint32 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}

// alignUp64 rounds v up to the nearest multiple of align (power of two).
func alignUp64(v, align uint64) uint64 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}