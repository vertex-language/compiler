package elf

import (
	"encoding/binary"
	"fmt"
)

// emit serialises o into a valid ELF64 ET_REL byte slice.
//
// File layout:
//
//	Elf64_Ehdr               64 bytes
//	user section data        variable (each blob aligned to section.Align)
//	.symtab                  24 bytes × (1 null + all symbols)
//	.strtab                  NUL-terminated symbol name strings
//	.rela.<name>             24 bytes × relocs, one block per section with relocs
//	.shstrtab                NUL-terminated section name strings
//	Elf64_Shdr table         64 bytes × (1 null + nsecs + nrela + 3 meta)
//
// Section header table layout:
//
//	[0]               null section
//	[1 … nsecs]       user sections, in the order they were created
//	[nsecs+1 … +nrela] .rela.<name> sections, in section-definition order
//	[nsecs+nrela+1]   .symtab
//	[nsecs+nrela+2]   .strtab
//	[nsecs+nrela+3]   .shstrtab  ← e_shstrndx
//
// The symbol table follows ELF convention: the null symbol at index 0,
// all STB_LOCAL symbols next, then STB_GLOBAL and STB_WEAK symbols. The
// sh_info field of .symtab records the index of the first non-local entry.
// Undefined symbols synthesised from unregistered relocation targets are
// appended as STB_GLOBAL SHN_UNDEF entries automatically.
func emit(o *Object) ([]byte, error) {
	le := binary.LittleEndian
	nsecs := len(o.sections)

	// ── Step 1: gather and order all symbols ──────────────────────────────────
	//
	// Auto-synthesise undefined symbols for any relocation that names a symbol
	// that has not been explicitly registered via AddSymbol.

	defined := make(map[string]bool, len(o.symbols))
	for _, sym := range o.symbols {
		defined[sym.Name] = true
	}
	allSyms := make([]Symbol, len(o.symbols))
	copy(allSyms, o.symbols)
	for _, r := range o.relocs {
		if r.Symbol != "" && !defined[r.Symbol] {
			allSyms = append(allSyms, Symbol{Name: r.Symbol, Global: true})
			defined[r.Symbol] = true
		}
	}

	// ELF requires all STB_LOCAL entries to precede the first non-local.
	var locals, globals []Symbol
	for _, sym := range allSyms {
		if sym.Global || sym.Weak {
			globals = append(globals, sym)
		} else {
			locals = append(locals, sym)
		}
	}
	ordered := append(locals, globals...)

	// firstGlobal is the sh_info value for .symtab: index of the first
	// non-local symbol (the null symbol counts as local, hence +1).
	firstGlobal := 1 + len(locals)

	// ── Step 2: build .strtab (symbol name string table) ─────────────────────

	strtab := []byte{0} // index 0 = empty string (null symbol name)
	strtabCache := map[string]uint32{"": 0}
	internSym := func(name string) uint32 {
		if off, ok := strtabCache[name]; ok {
			return off
		}
		off := uint32(len(strtab))
		strtab = append(strtab, name...)
		strtab = append(strtab, 0)
		strtabCache[name] = off
		return off
	}

	// ── Step 3: build Elf64_Sym records ──────────────────────────────────────

	type symRec struct {
		nameOff uint32
		info    uint8  // (bind<<4) | type
		vis     uint8  // st_other low 2 bits = STV_*
		shndx   uint16 // section index or SHNUndef / SHNAbs / SHNCommon
		value   uint64 // st_value
		size    uint64 // st_size
	}

	// Slot 0: null symbol (all zeros).
	symRecs := []symRec{{}}
	symIdxMap := make(map[string]uint32, len(ordered))

	for _, sym := range ordered {
		var shndx uint16
		var value, size uint64

		switch {
		case sym.Abs:
			shndx = SHNAbs
			value = sym.AbsValue
			size = sym.Size

		case sym.Common:
			shndx = SHNCommon
			value = sym.CommonAlign // ELF encodes alignment in st_value for SHN_COMMON
			size = sym.Size

		case sym.Section == "":
			shndx = SHNUndef

		default:
			si, ok := o.secIndex[sym.Section]
			if !ok {
				return nil, fmt.Errorf("object/elf emit: symbol %q references unknown section %q",
					sym.Name, sym.Section)
			}
			shndx = uint16(si + 1) // 1-based section index
			value = sym.Offset
			size = sym.Size
		}

		var bind uint8
		switch {
		case sym.Weak:
			bind = STBWeak
		case sym.Global:
			bind = STBGlobal
		default:
			bind = STBLocal
		}

		var styp uint8
		switch {
		case sym.IsFunction:
			styp = STTFunc
		case sym.IsData:
			styp = STTObject
		case sym.Common:
			styp = STTCommon
		}

		idx := uint32(len(symRecs))
		symIdxMap[sym.Name] = idx
		symRecs = append(symRecs, symRec{
			nameOff: internSym(sym.Name),
			info:    (bind << 4) | styp,
			vis:     sym.Vis & 0x3,
			shndx:   shndx,
			value:   value,
			size:    size,
		})
	}

	// ── Step 4: group relocs by section ───────────────────────────────────────

	relocsBySec := make(map[string][]Reloc, nsecs)
	for _, r := range o.relocs {
		relocsBySec[r.Section] = append(relocsBySec[r.Section], r)
	}

	// Determine which user sections carry relocations, preserving definition order.
	var relaSecs []string
	relaSeen := make(map[string]bool)
	for _, s := range o.sections {
		if _, ok := relocsBySec[s.Name]; ok && !relaSeen[s.Name] {
			relaSecs = append(relaSecs, s.Name)
			relaSeen[s.Name] = true
		}
	}
	nrela := len(relaSecs)

	// ── Step 5: assign section header table indices ───────────────────────────

	//   [0]                 null
	//   [1 … nsecs]         user sections
	//   [nsecs+1 … +nrela]  .rela.* sections
	//   [symtabShIdx]       .symtab
	//   [strtabShIdx]       .strtab
	//   [shstrtabShIdx]     .shstrtab
	symtabShIdx  := uint32(1 + nsecs + nrela)
	strtabShIdx  := symtabShIdx + 1
	shstrtabShIdx := strtabShIdx + 1
	totalShdr    := int(shstrtabShIdx) + 1

	// ── Step 6: build .shstrtab (section name string table) ───────────────────
	//
	// Pre-register every section name so the table is fully determined before
	// the file-layout step. Later calls to internSec are pure cache lookups.

	shstrtab := []byte{0}
	shstrCache := map[string]uint32{"": 0}
	internSec := func(name string) uint32 {
		if off, ok := shstrCache[name]; ok {
			return off
		}
		off := uint32(len(shstrtab))
		shstrtab = append(shstrtab, name...)
		shstrtab = append(shstrtab, 0)
		shstrCache[name] = off
		return off
	}
	for _, s := range o.sections {
		internSec(s.Name)
	}
	for _, secName := range relaSecs {
		internSec(".rela" + secName)
	}
	internSec(".symtab")
	internSec(".strtab")
	internSec(".shstrtab")

	// ── Step 7: compute file-offset layout ────────────────────────────────────

	type secLayout struct {
		fileOff uint64 // 0 for nobits / empty sections
		size    uint64
	}
	sl := make([]secLayout, nsecs)

	pos := uint64(elfHdrSize)

	// Non-nobits user section data.
	for i, s := range o.sections {
		if s.isNobits() {
			sl[i].size = s.nobitsSize
			continue
		}
		sz := uint64(s.buf.Len())
		if sz == 0 {
			continue
		}
		if s.Align > 1 {
			pos = alignUp64(pos, s.Align)
		}
		sl[i].fileOff = pos
		sl[i].size = sz
		pos += sz
	}

	// .symtab — 8-byte aligned.
	pos = alignUp64(pos, 8)
	symtabFileOff := pos
	symtabFileSize := uint64(len(symRecs)) * symSize
	pos += symtabFileSize

	// .strtab — no alignment requirement.
	strtabFileOff := pos
	strtabFileSize := uint64(len(strtab))
	pos += strtabFileSize

	// .rela.* sections — 8-byte aligned each.
	type relaLayout struct {
		fileOff uint64
		nrelocs uint64
	}
	relaLayouts := make(map[string]relaLayout, nrela)
	for _, secName := range relaSecs {
		pos = alignUp64(pos, 8)
		rels := relocsBySec[secName]
		relaLayouts[secName] = relaLayout{
			fileOff: pos,
			nrelocs: uint64(len(rels)),
		}
		pos += uint64(len(rels)) * relaSize
	}

	// .shstrtab.
	shstrtabFileOff := pos
	shstrtabFileSize := uint64(len(shstrtab))
	pos += shstrtabFileSize

	// Section header table — 8-byte aligned.
	pos = alignUp64(pos, 8)
	shdrFileOff := pos
	totalSize := int(shdrFileOff) + totalShdr*shdrSize

	buf := make([]byte, totalSize)

	// ── ELF header ────────────────────────────────────────────────────────────
	//
	// Elf64_Ehdr offsets match ehoff_* constants in linker/elf/parse.go.

	copy(buf[0:], "\x7fELF")
	buf[4] = 2 // ELFCLASS64
	buf[5] = 1 // ELFDATA2LSB (little-endian)
	buf[6] = 1 // EV_CURRENT
	buf[7] = 0 // ELFOSABI_NONE
	// buf[8:16] = 0  (EI_ABIVERSION + padding)
	le.PutUint16(buf[16:], 1)                     // ET_REL
	le.PutUint16(buf[18:], uint16(o.arch))        // e_machine
	le.PutUint32(buf[20:], 1)                     // e_version = EV_CURRENT
	// buf[24:32] e_entry = 0
	// buf[32:40] e_phoff = 0  (no program headers in ET_REL)
	le.PutUint64(buf[40:], shdrFileOff)           // e_shoff
	le.PutUint32(buf[48:], o.eflags)              // e_flags
	le.PutUint16(buf[52:], uint16(elfHdrSize))    // e_ehsize
	// buf[54:56] e_phentsize = 0
	// buf[56:58] e_phnum     = 0
	le.PutUint16(buf[58:], uint16(shdrSize))      // e_shentsize
	le.PutUint16(buf[60:], uint16(totalShdr))     // e_shnum
	le.PutUint16(buf[62:], uint16(shstrtabShIdx)) // e_shstrndx

	// ── User section data ─────────────────────────────────────────────────────

	for i, s := range o.sections {
		if s.isNobits() || sl[i].fileOff == 0 {
			continue
		}
		copy(buf[sl[i].fileOff:], s.buf.Bytes())
	}

	// ── .symtab data ──────────────────────────────────────────────────────────
	//
	// Elf64_Sym wire layout (24 bytes):
	//   0  uint32  st_name
	//   4  uint8   st_info  (bind<<4 | type)
	//   5  uint8   st_other (visibility in low 2 bits)
	//   6  uint16  st_shndx
	//   8  uint64  st_value
	//  16  uint64  st_size

	soff := int(symtabFileOff)
	for _, sr := range symRecs {
		le.PutUint32(buf[soff+0:], sr.nameOff)
		buf[soff+4] = sr.info
		buf[soff+5] = sr.vis
		le.PutUint16(buf[soff+6:], sr.shndx)
		le.PutUint64(buf[soff+8:], sr.value)
		le.PutUint64(buf[soff+16:], sr.size)
		soff += symSize
	}

	// ── .strtab data ──────────────────────────────────────────────────────────

	copy(buf[strtabFileOff:], strtab)

	// ── .rela.* data ──────────────────────────────────────────────────────────
	//
	// Elf64_Rela wire layout (24 bytes):
	//   0  uint64  r_offset
	//   8  uint64  r_info   (symIdx<<32 | type)
	//  16  int64   r_addend

	for _, secName := range relaSecs {
		rl := relaLayouts[secName]
		rels := relocsBySec[secName]
		roff := int(rl.fileOff)
		for _, r := range rels {
			symIdx, ok := symIdxMap[r.Symbol]
			if !ok {
				return nil, fmt.Errorf("object/elf emit: reloc in %q references unknown symbol %q",
					secName, r.Symbol)
			}
			rInfo := (uint64(symIdx) << 32) | uint64(r.Type)
			le.PutUint64(buf[roff+0:], uint64(r.Offset))
			le.PutUint64(buf[roff+8:], rInfo)
			le.PutUint64(buf[roff+16:], uint64(r.Addend)) // int64 bits preserved
			roff += relaSize
		}
	}

	// ── .shstrtab data ────────────────────────────────────────────────────────

	copy(buf[shstrtabFileOff:], shstrtab)

	// ── Section header table ──────────────────────────────────────────────────
	//
	// Elf64_Shdr wire layout (64 bytes):
	//   0  uint32  sh_name       (offset into .shstrtab)
	//   4  uint32  sh_type
	//   8  uint64  sh_flags
	//  16  uint64  sh_addr       (0 in ET_REL)
	//  24  uint64  sh_offset
	//  32  uint64  sh_size
	//  40  uint32  sh_link
	//  44  uint32  sh_info
	//  48  uint64  sh_addralign
	//  56  uint64  sh_entsize

	writeShdr := func(i int, nameOff, stype uint32, flags uint64,
		fileoff, size uint64, link, info uint32, align, entsize uint64) {
		base := int(shdrFileOff) + i*shdrSize
		le.PutUint32(buf[base+0:], nameOff)
		le.PutUint32(buf[base+4:], stype)
		le.PutUint64(buf[base+8:], flags)
		// buf[base+16:24] sh_addr = 0
		le.PutUint64(buf[base+24:], fileoff)
		le.PutUint64(buf[base+32:], size)
		le.PutUint32(buf[base+40:], link)
		le.PutUint32(buf[base+44:], info)
		le.PutUint64(buf[base+48:], align)
		le.PutUint64(buf[base+56:], entsize)
	}

	// [0] null section — all zeros; writeShdr default is already zero, but be explicit.
	writeShdr(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)

	// [1 … nsecs] user sections.
	for i, s := range o.sections {
		var foff uint64
		var stype = s.Type
		if !s.isNobits() {
			foff = sl[i].fileOff
		}
		writeShdr(i+1,
			internSec(s.Name), stype, s.Flags,
			foff, sl[i].size,
			0, 0, s.Align, 0)
	}

	// [nsecs+1 … nsecs+nrela] .rela.<name> sections.
	//   sh_link  = .symtab section index
	//   sh_info  = index of the user section being patched (1-based)
	//   sh_flags = SHF_INFO_LINK (sh_info holds a section header index)
	for j, secName := range relaSecs {
		si := o.secIndex[secName]
		rl := relaLayouts[secName]
		writeShdr(nsecs+1+j,
			internSec(".rela"+secName), SHTRela, SHFInfoLink,
			rl.fileOff, rl.nrelocs*relaSize,
			symtabShIdx, uint32(si+1), 8, relaSize)
	}

	// .symtab
	//   sh_link  = .strtab section index
	//   sh_info  = index of first non-local symbol
	writeShdr(int(symtabShIdx),
		internSec(".symtab"), SHTSymtab, 0,
		symtabFileOff, symtabFileSize,
		strtabShIdx, uint32(firstGlobal), 8, symSize)

	// .strtab
	writeShdr(int(strtabShIdx),
		internSec(".strtab"), SHTStrtab, 0,
		strtabFileOff, strtabFileSize,
		0, 0, 1, 0)

	// .shstrtab
	writeShdr(int(shstrtabShIdx),
		internSec(".shstrtab"), SHTStrtab, 0,
		shstrtabFileOff, shstrtabFileSize,
		0, 0, 1, 0)

	return buf, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// alignUp64 rounds v up to the nearest multiple of align (must be a power of two).
func alignUp64(v, align uint64) uint64 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}