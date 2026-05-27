// shared.go
package elf

import (
	"fmt"
	"os"
)

// DynSymbol is one exported symbol from a shared library's .dynsym.
type DynSymbol struct {
	Name    string
	Value   uint64
	Size    uint64
	Bind    uint8
	Type    uint8
	Version string // e.g. "GLIBC_2.17"; empty if no versioning
}

// SharedLib is a parsed ELF64 ET_DYN shared object.
// Only the information needed by the static linker is extracted —
// the dynamic symbol table, DT_NEEDED deps, soname, and rpath/runpath.
type SharedLib struct {
	Path    string
	soname  string
	needed  []string   // DT_NEEDED entries, in order
	rpaths  []string   // DT_RPATH + DT_RUNPATH, in order
	symbols map[string]*DynSymbol
}

// Soname returns the library's DT_SONAME, or its path basename if absent.
func (s *SharedLib) Soname() string {
	if s.soname != "" {
		return s.soname
	}
	// fallback: use Path basename
	for i := len(s.Path) - 1; i >= 0; i-- {
		if s.Path[i] == '/' {
			return s.Path[i+1:]
		}
	}
	return s.Path
}

// Needed returns the DT_NEEDED entries (direct shared-library dependencies).
func (s *SharedLib) Needed() []string { return s.needed }

// Rpaths returns the DT_RPATH and DT_RUNPATH entries, used during
// transitive dependency resolution.
func (s *SharedLib) Rpaths() []string { return s.rpaths }

// Symbol looks up a symbol by name in this library's dynamic symbol table.
func (s *SharedLib) Symbol(name string) (*DynSymbol, bool) {
	sym, ok := s.symbols[name]
	return sym, ok
}

// OpenShared reads path from disk and parses it.
func OpenShared(path string) (*SharedLib, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open shared %q: %w", path, err)
	}
	lib, err := ParseShared(data)
	if err != nil {
		return nil, fmt.Errorf("parse shared %q: %w", path, err)
	}
	lib.Path = path
	return lib, nil
}

// MustOpenShared panics on error.
func MustOpenShared(path string) *SharedLib {
	s, err := OpenShared(path)
	if err != nil {
		panic(err)
	}
	return s
}

// ParseShared parses an ELF64 ET_DYN shared object from raw bytes.
//
// Strategy:
//   1. Read the ELF header → locate section header table.
//   2. Find .dynamic (SHT_DYNAMIC) → extract DT_STRTAB, DT_STRSZ, DT_SYMTAB,
//      DT_SONAME, DT_NEEDED, DT_RPATH, DT_RUNPATH via d_tag walk.
//   3. Walk .dynsym (SHT_DYNSYM) using section headers to read exported symbols.
//   4. Decode symbol versions from .gnu.version / .gnu.version_r (optional).
//
// Note: DT_STRTAB / DT_SYMTAB carry virtual addresses, not file offsets.
// We resolve them to file offsets via the section header table (sh_addr → sh_offset).
func ParseShared(data []byte) (*SharedLib, error) {
	r := newReader(data)

	if len(data) < ehdrSize {
		return nil, fmt.Errorf("file too small")
	}
	if string(data[0:4]) != "\x7fELF" {
		return nil, fmt.Errorf("not an ELF file")
	}
	if data[eiClass] != elfClass64 {
		return nil, fmt.Errorf("not ELF64")
	}
	if data[eiData] != elfData2LSB {
		return nil, fmt.Errorf("only little-endian supported")
	}
	eType, _ := r.u16(ehoff_type)
	if eType != etDyn && eType != etExec {
		return nil, fmt.Errorf("not a shared object or executable (e_type=%d)", eType)
	}

	shoff, _    := r.u64(ehoff_shoff)
	shentsize, _ := r.u16(ehoff_shentsize)
	shnum, _   := r.u16(ehoff_shnum)
	shstrndx, _ := r.u16(ehoff_shstrndx)

	if shentsize < shdrSize || shnum == 0 {
		return nil, fmt.Errorf("no section headers")
	}

	// ── Read section headers ───────────────────────────────────────────────────

	type secInfo struct {
		name    string
		stype   uint32
		addr    uint64
		offset  uint64
		size    uint64
		link    uint32
		entsize uint64
	}

	secs := make([]secInfo, shnum)
	readSec := func(i int) (secInfo, error) {
		base := int(shoff) + i*int(shentsize)
		var s secInfo
		var err error
		if s.stype, err   = r.u32(base + shoff_type);   err != nil { return s, err }
		if s.addr, err    = r.u64(base + shoff_addr);    err != nil { return s, err }
		if s.offset, err  = r.u64(base + shoff_offset);  err != nil { return s, err }
		if s.size, err    = r.u64(base + shoff_size);    err != nil { return s, err }
		if s.link, err    = r.u32(base + shoff_link);    err != nil { return s, err }
		if s.entsize, err = r.u64(base + shoff_entsize); err != nil { return s, err }
		return s, nil
	}

	for i := range secs {
		s, err := readSec(i)
		if err != nil {
			return nil, fmt.Errorf("section header %d: %w", i, err)
		}
		secs[i] = s
	}

	// Read shstrtab for section names.
	if int(shstrndx) < len(secs) {
		sh := secs[shstrndx]
		shstrtab, err := r.view(int(sh.offset), int(sh.size))
		if err == nil {
			for i := range secs {
				base := int(shoff) + i*int(shentsize)
				nameOff, _ := r.u32(base + shoff_name)
				secs[i].name, _ = cstr(shstrtab, nameOff)
			}
		}
	}

	// Helper: resolve a virtual address to a file offset via section headers.
	vaddrToFileOffset := func(vaddr uint64) (uint64, bool) {
		for _, s := range secs {
			if s.addr != 0 && vaddr >= s.addr && vaddr < s.addr+s.size {
				return s.offset + (vaddr - s.addr), true
			}
		}
		return 0, false
	}

	// ── Walk .dynamic section ─────────────────────────────────────────────────

	var (
		dynStrtabVA uint64
		dynStrtabSz uint64
		dynSymtabVA uint64
		soname      uint64
		lib         = &SharedLib{symbols: make(map[string]*DynSymbol)}
	)

	for _, sec := range secs {
		if sec.stype != shtDynamic {
			continue
		}
		n := int(sec.size) / dynEntSize
		dr := newReader(data)
		for i := 0; i < n; i++ {
			base := int(sec.offset) + i*dynEntSize
			tag, _ := dr.i64(base + dynoff_tag)
			val, _ := dr.u64(base + dynoff_val)
			switch int64(tag) {
			case dtNull:
				goto doneDynamic
			case dtNeeded:
				// val is a string-table offset — resolve after we have dynStrtab
				lib.needed = append(lib.needed, fmt.Sprintf("@strtab:%d", val))
			case dtStrtab:
				dynStrtabVA = val
			case dtStrsz:
				dynStrtabSz = val
			case dtSymtab:
				dynSymtabVA = val
			case dtSoname:
				soname = val
			case dtRpath, dtRunpath:
				lib.rpaths = append(lib.rpaths, fmt.Sprintf("@strtab:%d", val))
			}
		}
	doneDynamic:
		break
	}

	// ── Resolve .dynamic string table ─────────────────────────────────────────

	var dynstrtab []byte
	if dynStrtabVA != 0 {
		if foff, ok := vaddrToFileOffset(dynStrtabVA); ok {
			sz := dynStrtabSz
			if sz == 0 {
				sz = 4096 // fallback
			}
			if d, err := r.view(int(foff), int(sz)); err == nil {
				dynstrtab = d
			}
		}
		// Also try finding .dynstr by section type/name.
		if dynstrtab == nil {
			for _, sec := range secs {
				if sec.name == ".dynstr" && sec.stype == shtStrtab {
					dynstrtab, _ = r.view(int(sec.offset), int(sec.size))
					break
				}
			}
		}
	}

	// Resolve deferred string-table references.
	resolveStrtab := func(placeholder string) string {
		if dynstrtab == nil {
			return placeholder
		}
		var off uint64
		if _, err := fmt.Sscanf(placeholder, "@strtab:%d", &off); err == nil {
			s, _ := cstr(dynstrtab, uint32(off))
			return s
		}
		return placeholder
	}

	for i, n := range lib.needed {
		lib.needed[i] = resolveStrtab(n)
	}
	for i, rp := range lib.rpaths {
		lib.rpaths[i] = resolveStrtab(rp)
	}
	if soname != 0 {
		lib.soname, _ = cstr(dynstrtab, uint32(soname))
	}

	// ── Parse .dynsym ─────────────────────────────────────────────────────────

	// Locate .dynsym: either via DT_SYMTAB vaddr or by section name/type.
	var dynsymOffset uint64
	var dynsymSize   uint64

	if dynSymtabVA != 0 {
		for _, sec := range secs {
			if sec.stype == shtDynsym {
				dynsymOffset = sec.offset
				dynsymSize   = sec.size
				break
			}
		}
	}
	if dynsymOffset == 0 {
		for _, sec := range secs {
			if sec.stype == shtDynsym {
				dynsymOffset = sec.offset
				dynsymSize   = sec.size
				break
			}
		}
	}

	if dynsymOffset != 0 && dynsymSize > 0 && dynstrtab != nil {
		n := int(dynsymSize) / symEntSize
		sr := newReader(data)
		for i := 0; i < n; i++ {
			base := int(dynsymOffset) + i*symEntSize
			nameOff, _ := sr.u32(base + symoff_name)
			info        := data[base+symoff_info]
			shndx, _   := sr.u16(base + symoff_shndx)
			value, _   := sr.u64(base + symoff_value)
			size, _    := sr.u64(base + symoff_size)

			name, _ := cstr(dynstrtab, nameOff)
			if name == "" {
				continue
			}
			bind := stBind(info)
			stype := stType(info)

			// Only record defined symbols (shndx != SHN_UNDEF) — those are
			// what the static linker can satisfy references against.
			if shndx != shnUndef {
				lib.symbols[name] = &DynSymbol{
					Name:  name,
					Value: value,
					Size:  size,
					Bind:  bind,
					Type:  stype,
				}
			}
		}
	}

	// ── Optional: decode .gnu.version_r for symbol versions ───────────────────
	// This is best-effort — version strings are attached to DynSymbol.Version.
	// Implementation omitted for brevity; see bin/elf BuildVersionNeed for structure.

	return lib, nil
}