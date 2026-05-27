// object.go
package elf

import (
	"fmt"
	"os"
)

// ── Public types ──────────────────────────────────────────────────────────────

// RawSection is one section from an input .o file, fully loaded into memory.
type RawSection struct {
	Name    string
	Type    uint32
	Flags   uint64
	Data    []byte  // nil for SHT_NOBITS
	Size    uint64  // len(Data) for data sections; bss-size for SHT_NOBITS
	Align   uint64
	Link    uint32
	Info    uint32
	EntSize uint64
	Index   int     // position in the input file's section header table
}

// RawSymbol is one Elf64_Sym entry from a .symtab or .dynsym section.
type RawSymbol struct {
	Name    string
	Value   uint64
	Size    uint64
	Bind    uint8  // stbLocal / stbGlobal / stbWeak
	Type    uint8  // sttFunc / sttObject / …
	Vis     uint8  // STV_DEFAULT etc.
	ShndxRaw uint16 // raw st_shndx value

	// SectionName is the decoded section name:
	//   ""         for SHN_UNDEF  (undefined/imported)
	//   "*ABS*"    for SHN_ABS
	//   "*COMMON*" for SHN_COMMON
	//   ".text" …  for normal section references
	SectionName string
}

// RawReloc is one Elf64_Rela entry, with its target section index decoded.
type RawReloc struct {
	TargetSecIdx int    // sh_info of the .rela.* section → index of section being patched
	Offset       uint64
	SymIdx       uint32
	Type         uint32
	Addend       int64
}

// ObjectFile is a fully-parsed ELF64 ET_REL relocatable object.
type ObjectFile struct {
	Path     string
	Machine  uint16
	EFlags   uint32
	Sections []*RawSection // index 0 is the null section
	Symbols  []*RawSymbol  // index 0 is the null symbol
	Relocs   []*RawReloc
}

// OpenObject reads path from disk and parses it.
func OpenObject(path string) (*ObjectFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open object %q: %w", path, err)
	}
	obj, err := ParseObject(data)
	if err != nil {
		return nil, fmt.Errorf("parse object %q: %w", path, err)
	}
	obj.Path = path
	return obj, nil
}

// MustOpenObject panics if OpenObject fails.
func MustOpenObject(path string) *ObjectFile {
	o, err := OpenObject(path)
	if err != nil {
		panic(err)
	}
	return o
}

// ParseObject parses an ELF64 ET_REL object from raw bytes.
//
// Wire layout (all little-endian):
//
//   offset 0:  Elf64_Ehdr (64 bytes)
//   e_shoff:   Elf64_Shdr[e_shnum] (64 bytes each)
//   sections:  raw section data at sh_offset[i]
func ParseObject(data []byte) (*ObjectFile, error) {
	r := newReader(data)

	// ── ELF header ────────────────────────────────────────────────────────────

	if len(data) < ehdrSize {
		return nil, fmt.Errorf("file too small for ELF header (%d < %d)", len(data), ehdrSize)
	}
	if string(data[0:4]) != "\x7fELF" {
		return nil, fmt.Errorf("not an ELF file (bad magic)")
	}
	if data[eiClass] != elfClass64 {
		return nil, fmt.Errorf("not ELF64 (EI_CLASS=%d)", data[eiClass])
	}
	if data[eiData] != elfData2LSB {
		return nil, fmt.Errorf("only little-endian ELF supported (EI_DATA=%d)", data[eiData])
	}

	eType, _    := r.u16(ehoff_type)
	machine, _  := r.u16(ehoff_machine)
	eflags, _   := r.u32(ehoff_flags)
	shoff, _    := r.u64(ehoff_shoff)
	shentsize, _ := r.u16(ehoff_shentsize)
	shnum, _    := r.u16(ehoff_shnum)
	shstrndx, _ := r.u16(ehoff_shstrndx)

	if eType != etRel {
		return nil, fmt.Errorf("not a relocatable object (e_type=%d, want ET_REL=%d)", eType, etRel)
	}
	if shentsize < shdrSize {
		return nil, fmt.Errorf("e_shentsize=%d < required %d", shentsize, shdrSize)
	}

	// ── Section headers ───────────────────────────────────────────────────────

	type shdrRaw struct {
		nameOff, stype uint32
		flags          uint64
		addr, offset   uint64
		size           uint64
		link, info     uint32
		align, entsize uint64
	}

	readShdr := func(i int) (shdrRaw, error) {
		base := int(shoff) + i*int(shentsize)
		var s shdrRaw
		var err error
		if s.nameOff, err = r.u32(base + shoff_name);      err != nil { return s, err }
		if s.stype, err   = r.u32(base + shoff_type);      err != nil { return s, err }
		if s.flags, err   = r.u64(base + shoff_flags);     err != nil { return s, err }
		if s.addr, err    = r.u64(base + shoff_addr);      err != nil { return s, err }
		if s.offset, err  = r.u64(base + shoff_offset);    err != nil { return s, err }
		if s.size, err    = r.u64(base + shoff_size);      err != nil { return s, err }
		if s.link, err    = r.u32(base + shoff_link);      err != nil { return s, err }
		if s.info, err    = r.u32(base + shoff_info);      err != nil { return s, err }
		if s.align, err   = r.u64(base + shoff_addralign); err != nil { return s, err }
		if s.entsize, err = r.u64(base + shoff_entsize);   err != nil { return s, err }
		return s, nil
	}

	shdrs := make([]shdrRaw, shnum)
	for i := range shdrs {
		sh, err := readShdr(i)
		if err != nil {
			return nil, fmt.Errorf("section header %d: %w", i, err)
		}
		shdrs[i] = sh
	}

	// ── .shstrtab ─────────────────────────────────────────────────────────────

	if int(shstrndx) >= len(shdrs) {
		return nil, fmt.Errorf("e_shstrndx=%d out of range (%d sections)", shstrndx, len(shdrs))
	}
	shstrSh := shdrs[shstrndx]
	shstrtab, err := r.view(int(shstrSh.offset), int(shstrSh.size))
	if err != nil {
		return nil, fmt.Errorf("reading shstrtab: %w", err)
	}

	// ── Build RawSection slice ─────────────────────────────────────────────────

	sections := make([]*RawSection, len(shdrs))
	for i, sh := range shdrs {
		name, err := cstr(shstrtab, sh.nameOff)
		if err != nil {
			return nil, fmt.Errorf("section %d name: %w", i, err)
		}
		sec := &RawSection{
			Name:    name,
			Type:    sh.stype,
			Flags:   sh.flags,
			Align:   sh.align,
			Size:    sh.size,
			Link:    sh.link,
			Info:    sh.info,
			EntSize: sh.entsize,
			Index:   i,
		}
		if sh.stype != shtNobits && sh.size > 0 {
			sec.Data, err = r.slice(int(sh.offset), int(sh.size))
			if err != nil {
				return nil, fmt.Errorf("section %q data: %w", name, err)
			}
		}
		sections[i] = sec
	}

	// ── Parse .symtab ─────────────────────────────────────────────────────────

	var symbols []*RawSymbol

	for _, sec := range sections {
		if sec.Type != shtSymtab {
			continue
		}
		if int(sec.Link) >= len(sections) {
			return nil, fmt.Errorf(".symtab sh_link=%d out of range", sec.Link)
		}
		strtab := sections[sec.Link].Data
		if strtab == nil {
			return nil, fmt.Errorf(".symtab string table (section %d) has no data", sec.Link)
		}

		n := len(sec.Data) / symEntSize
		symbols = make([]*RawSymbol, n)
		sr := newReader(sec.Data)

		for i := range symbols {
			base := i * symEntSize
			nameOff, _ := sr.u32(base + symoff_name)
			info        := sec.Data[base+symoff_info]
			other       := sec.Data[base+symoff_other]
			shndx, _   := sr.u16(base + symoff_shndx)
			value, _   := sr.u64(base + symoff_value)
			size, _    := sr.u64(base + symoff_size)

			name, _ := cstr(strtab, nameOff)

			secName := ""
			switch shndx {
			case shnUndef:
				secName = ""
			case shnAbs:
				secName = "*ABS*"
			case shnCommon:
				secName = "*COMMON*"
			case shnXindex:
				// extended index — would need SHT_SYMTAB_SHNDX; not common in .o files
				secName = ""
			default:
				if int(shndx) < len(sections) {
					secName = sections[shndx].Name
				}
			}

			symbols[i] = &RawSymbol{
				Name:        name,
				Value:       value,
				Size:        size,
				Bind:        stBind(info),
				Type:        stType(info),
				Vis:         other & 0x3,
				ShndxRaw:    shndx,
				SectionName: secName,
			}
		}
		break // there is at most one SHT_SYMTAB
	}

	// ── Parse RELA sections ────────────────────────────────────────────────────

	var relocs []*RawReloc

	for _, sec := range sections {
		if sec.Type != shtRela {
			continue
		}
		// sh_info = index of section being patched
		// sh_link = index of symbol table
		n := len(sec.Data) / relaEntSize
		sr := newReader(sec.Data)
		for i := 0; i < n; i++ {
			base := i * relaEntSize
			offset, _ := sr.u64(base + relaoff_offset)
			info, _   := sr.u64(base + relaoff_info)
			addend, _ := sr.i64(base + relaoff_addend)
			relocs = append(relocs, &RawReloc{
				TargetSecIdx: int(sec.Info),
				Offset:       offset,
				SymIdx:       relaSymIdx(info),
				Type:         relaType(info),
				Addend:       addend,
			})
		}
	}

	return &ObjectFile{
		Machine:  machine,
		EFlags:   eflags,
		Sections: sections,
		Symbols:  symbols,
		Relocs:   relocs,
	}, nil
}