package macho

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Magic numbers.
const (
	mhMagic64 uint32 = 0xFEEDFACF
	mhCigam64 uint32 = 0xCFFAEDFE // big-endian 64-bit (unsupported)
	mhMagic32 uint32 = 0xFEEDFACE
	mhCigam32 uint32 = 0xCEFAEDFE

	mhObject uint32 = 0x1

	lcSegment64 uint32 = 0x19
	lcSymtab    uint32 = 0x02
)

// nlist_64 n_type masks and values.
const (
	nStab uint8 = 0xe0
	nPExt uint8 = 0x10
	nType uint8 = 0x0e
	nExt  uint8 = 0x01

	nUndf uint8 = 0x00
	nAbs  uint8 = 0x02
	nSect uint8 = 0x0e

	nWeakRef uint16 = 0x0040
	nWeakDef uint16 = 0x0080
)

// OpenObject opens and parses an MH_OBJECT Mach-O file.
func OpenObject(path string) (*ObjectFile, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	obj.Path = path
	return obj, nil
}

// MustOpenObject calls OpenObject and panics on error.
func MustOpenObject(path string) *ObjectFile {
	o, err := OpenObject(path)
	if err != nil {
		panic(err)
	}
	return o
}

// ParseObject parses a raw MH_OBJECT byte slice.
func ParseObject(data []byte) (*ObjectFile, error) {
	if len(data) < 4 {
		return nil, errors.New("not a Mach-O file")
	}
	magic := binary.LittleEndian.Uint32(data)
	switch magic {
	case mhMagic64:
		// little-endian 64-bit — correct
	case mhCigam64:
		return nil, errors.New("only little-endian Mach-O supported")
	case mhMagic32, mhCigam32:
		return nil, errors.New("not a 64-bit Mach-O")
	default:
		return nil, errors.New("not a Mach-O file")
	}

	if len(data) < 32 {
		return nil, errors.New("Mach-O header truncated")
	}

	arch := binary.LittleEndian.Uint32(data[4:])
	filetype := binary.LittleEndian.Uint32(data[12:])
	if filetype != mhObject {
		return nil, fmt.Errorf("not a relocatable object (MH_OBJECT), got filetype 0x%x", filetype)
	}
	ncmds := binary.LittleEndian.Uint32(data[16:])
	mhFlags := binary.LittleEndian.Uint32(data[24:])

	obj := &ObjectFile{
		Arch:    arch,
		MHFlags: mhFlags,
		Sections: []*RawSection{nil}, // index 0 is nil (sections are 1-based)
	}

	// Walk load commands.
	off := 32
	var symoff, nsyms, stroff, strsize uint32

	for i := uint32(0); i < ncmds; i++ {
		if off+8 > len(data) {
			return nil, errors.New("load command truncated")
		}
		cmd := binary.LittleEndian.Uint32(data[off:])
		cmdsize := binary.LittleEndian.Uint32(data[off+4:])
		if cmdsize < 8 || off+int(cmdsize) > len(data) {
			return nil, fmt.Errorf("load command %d has invalid size %d", i, cmdsize)
		}

		switch cmd {
		case lcSegment64:
			if err := parseSegment64(data, off, obj); err != nil {
				return nil, err
			}
		case lcSymtab:
			if cmdsize < 24 {
				return nil, errors.New("LC_SYMTAB too small")
			}
			symoff = binary.LittleEndian.Uint32(data[off+8:])
			nsyms = binary.LittleEndian.Uint32(data[off+12:])
			stroff = binary.LittleEndian.Uint32(data[off+16:])
			strsize = binary.LittleEndian.Uint32(data[off+20:])
		}

		off += int(cmdsize)
	}

	// Parse symbol table.
	if nsyms > 0 {
		if err := parseSymtab(data, symoff, nsyms, stroff, strsize, obj); err != nil {
			return nil, err
		}
	}

	return obj, nil
}

func parseSegment64(data []byte, off int, obj *ObjectFile) error {
	// segment_command_64 is 64 bytes; each section_64 is 80 bytes.
	if off+64 > len(data) {
		return errors.New("LC_SEGMENT_64 truncated")
	}
	nsects := binary.LittleEndian.Uint32(data[off+48:])
	sectBase := off + 64

	for i := uint32(0); i < nsects; i++ {
		soff := sectBase + int(i)*80
		if soff+80 > len(data) {
			return errors.New("section_64 truncated")
		}
		s, err := parseSection64(data, soff)
		if err != nil {
			return err
		}
		// Assign 1-based index.
		s.Index = len(obj.Sections)
		obj.Sections = append(obj.Sections, s)

		// Parse relocations for this section.
		reloff := binary.LittleEndian.Uint32(data[soff+48:])
		nreloc := binary.LittleEndian.Uint32(data[soff+52:])
		secIdx := s.Index - 1 // 0-based index in Sections[1:]
		for r := uint32(0); r < nreloc; r++ {
			roff := int(reloff) + int(r)*8
			if roff+8 > len(data) {
				return errors.New("relocation entry truncated")
			}
			rel, err := parseReloc(data, roff, secIdx)
			if err != nil {
				return err
			}
			obj.Relocs = append(obj.Relocs, rel)
		}
	}
	return nil
}

func parseSection64(data []byte, off int) (*RawSection, error) {
	sectName := cstring16(data[off:])
	segName := cstring16(data[off+16:])
	addr := binary.LittleEndian.Uint64(data[off+32:])
	size := binary.LittleEndian.Uint64(data[off+40:])
	fileoff := binary.LittleEndian.Uint32(data[off+48:])
	alignLog := binary.LittleEndian.Uint32(data[off+52:])
	flags := binary.LittleEndian.Uint32(data[off+64:])
	reserved1 := binary.LittleEndian.Uint32(data[off+68:])
	reserved2 := binary.LittleEndian.Uint32(data[off+72:])

	align := uint32(1)
	if alignLog < 32 {
		align = 1 << alignLog
	}

	s := &RawSection{
		SegName:   segName,
		SectName:  sectName,
		Addr:      addr,
		Size:      size,
		Align:     align,
		Flags:     flags,
		Reserved1: reserved1,
		Reserved2: reserved2,
	}

	if !s.IsZerofill() && size > 0 {
		end := int(fileoff) + int(size)
		if int(fileoff) > len(data) || end > len(data) {
			return nil, fmt.Errorf("section %s,%s data out of bounds", segName, sectName)
		}
		s.Data = make([]byte, size)
		copy(s.Data, data[fileoff:end])
	}

	return s, nil
}

func parseReloc(data []byte, off int, sectionIdx int) (*RawReloc, error) {
	addr := binary.LittleEndian.Uint32(data[off:])
	packed := binary.LittleEndian.Uint32(data[off+4:])

	// Check for scattered relocation (high bit of r_address set).
	// Scattered relocs are only used in 32-bit Mach-O; we reject them.
	if addr&0x80000000 != 0 {
		return nil, errors.New("scattered relocation encountered in 64-bit object (unsupported)")
	}

	symbolnum := packed & 0x00FFFFFF
	pcrel := (packed>>24)&1 != 0
	length := uint8((packed >> 25) & 0x3)
	extern := (packed>>27)&1 != 0
	rtype := uint8((packed >> 28) & 0xF)

	r := &RawReloc{
		SectionIdx: sectionIdx,
		Offset:     addr,
		PCRel:      pcrel,
		Length:     length,
		Extern:     extern,
		Type:       rtype,
	}
	if extern {
		r.SymIdx = symbolnum
	} else {
		r.SectNum = symbolnum
	}
	return r, nil
}

func parseSymtab(data []byte, symoff, nsyms, stroff, strsize uint32, obj *ObjectFile) error {
	strtab := data[stroff : stroff+strsize]

	for i := uint32(0); i < nsyms; i++ {
		noff := int(symoff) + int(i)*16
		if noff+16 > len(data) {
			return errors.New("nlist_64 entry truncated")
		}
		strx := binary.LittleEndian.Uint32(data[noff:])
		ntype := data[noff+4]
		nsect := data[noff+5]
		desc := binary.LittleEndian.Uint16(data[noff+6:])
		value := binary.LittleEndian.Uint64(data[noff+8:])

		name := ""
		if strx < strsize {
			name = pstring(strtab[strx:])
		}

		sym := &RawSymbol{
			Name:  name,
			Type:  ntype,
			Sect:  nsect,
			Desc:  desc,
			Value: value,
		}

		// Decode type bits.
		if ntype&nStab != 0 {
			sym.IsDebug = true
			obj.Symbols = append(obj.Symbols, sym)
			continue
		}

		sym.IsGlobal = ntype&nExt != 0
		sym.IsPrivExt = ntype&nPExt != 0
		sym.IsWeak = desc&nWeakDef != 0 || desc&nWeakRef != 0

		typeField := ntype & nType
		switch typeField {
		case nAbs:
			sym.IsAbs = true
		case nUndf:
			if value > 0 {
				sym.IsCommon = true
			} else {
				sym.IsUndef = true
			}
		case nSect:
			// Defined in a section; decode section name.
			if int(nsect) > 0 && int(nsect) < len(obj.Sections) {
				sec := obj.Sections[nsect]
				sym.SectionName = sec.SectName
				sym.SegmentName = sec.SegName
			}
		}

		obj.Symbols = append(obj.Symbols, sym)
	}
	return nil
}

// cstring16 reads a NUL-padded 16-byte field as a Go string.
func cstring16(b []byte) string {
	for i := 0; i < 16; i++ {
		if b[i] == 0 {
			return string(b[:i])
		}
	}
	return string(b[:16])
}

// pstring reads a NUL-terminated string from a byte slice.
func pstring(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}