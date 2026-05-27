// archive.go
package elf

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// GNU/SysV ar format constants.
const (
	arMagic   = "!<arch>\n" // 8-byte global header
	arHdrSize = 60          // struct ar_hdr
	arFmag    = "`\n"       // ar_hdr terminator (bytes 58–59)
)

// ── Public types ──────────────────────────────────────────────────────────────

// ArchiveMember is one relocatable object inside a .a file, loaded but lazily parsed.
type ArchiveMember struct {
	Name string
	data []byte
	obj  *ObjectFile // nil until Object() is called
}

// Object parses and returns the ELF object for this member. Cached.
func (m *ArchiveMember) Object() (*ObjectFile, error) {
	if m.obj != nil {
		return m.obj, nil
	}
	obj, err := ParseObject(m.data)
	if err != nil {
		return nil, fmt.Errorf("archive member %q: %w", m.Name, err)
	}
	obj.Path = m.Name
	m.obj = obj
	return obj, nil
}

// Archive is a parsed .a static library.
type Archive struct {
	Path    string
	Members []*ArchiveMember

	// symIndex maps a defined symbol name → index in Members.
	// Populated from the "/" symbol-index member; falls back to lazy scan
	// if that member is absent or malformed.
	symIndex map[string]int
}

// MemberForSymbol returns the archive member that provides a definition for
// sym, or nil if none exists. Only STB_GLOBAL definitions are in the index.
func (a *Archive) MemberForSymbol(sym string) *ArchiveMember {
	if idx, ok := a.symIndex[sym]; ok {
		return a.Members[idx]
	}
	return nil
}

// OpenArchive reads path from disk and parses it.
func OpenArchive(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open archive %q: %w", path, err)
	}
	ar, err := ParseArchive(data)
	if err != nil {
		return nil, fmt.Errorf("parse archive %q: %w", path, err)
	}
	ar.Path = path
	return ar, nil
}

// MustOpenArchive panics on error.
func MustOpenArchive(path string) *Archive {
	a, err := OpenArchive(path)
	if err != nil {
		panic(err)
	}
	return a
}

// ParseArchive parses a GNU/SysV ar archive from raw bytes.
//
// Wire format:
//   "!<arch>\n"           8-byte magic
//   For each member:
//     [0:16]  ar_name     member name (right-padded with spaces; "/" suffix for SysV short names)
//     [16:28] ar_date     decimal mtime
//     [28:34] ar_uid      decimal uid
//     [34:40] ar_gid      decimal gid
//     [40:48] ar_mode     octal mode
//     [48:58] ar_size     decimal data size (not including padding)
//     [58:60] ar_fmag     "`\n" sentinel
//     [60:60+size] data   padded to even file offset (padding byte not counted in ar_size)
//
// Special GNU/SysV members (always appear before regular members):
//   "/"     SysV symbol index  — big-endian uint32 nsyms, uint32[nsyms] archive offsets,
//                                 then null-terminated symbol names
//   "/SYM64/" 64-bit variant  — same but uint64 counts and offsets
//   "//"    GNU long-name table — concatenated names, "/" delimited
func ParseArchive(data []byte) (*Archive, error) {
	if len(data) < len(arMagic) || string(data[:len(arMagic)]) != arMagic {
		return nil, fmt.Errorf("not a GNU/SysV archive (bad magic)")
	}

	ar := &Archive{symIndex: make(map[string]int)}

	// ── Pass 1: scan all member headers ───────────────────────────────────────

	// rawEntry captures the decoded header and a slice view of the member data.
	type rawEntry struct {
		hdrOffset int    // file offset of the ar_hdr
		rawName   string // raw ar_name field (trimmed of trailing spaces)
		memberData []byte
	}

	var entries []rawEntry
	var longNameTable []byte // "//" member content

	pos := len(arMagic)
	for pos+arHdrSize <= len(data) {
		hdrOffset := pos
		hdr := data[pos : pos+arHdrSize]

		if string(hdr[58:60]) != arFmag {
			return nil, fmt.Errorf("bad ar_fmag at file offset 0x%x", pos)
		}

		rawName := strings.TrimRight(string(hdr[0:16]), " ")
		sizeStr := strings.TrimRight(string(hdr[48:58]), " ")

		size, err := strconv.Atoi(sizeStr)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid ar_size %q at 0x%x", sizeStr, pos)
		}

		dataStart := pos + arHdrSize
		dataEnd   := dataStart + size
		if dataEnd > len(data) {
			return nil, fmt.Errorf("member data out of bounds at 0x%x (need %d, have %d)",
				pos, dataEnd, len(data))
		}

		memberData := data[dataStart:dataEnd]

		if rawName == "//" {
			longNameTable = make([]byte, size)
			copy(longNameTable, memberData)
		}

		entries = append(entries, rawEntry{hdrOffset, rawName, memberData})

		pos = dataEnd
		if pos%2 != 0 {
			pos++ // even-byte padding between members
		}
	}

	// ── Pass 2: build Members and hdrOffset→index map ─────────────────────────

	// We need hdrOffset→memberIndex to resolve symbol-index offsets.
	offsetToMemberIdx := make(map[int]int)

	for _, e := range entries {
		switch e.rawName {
		case "/", "/SYM64/", "__.SYMDEF", "__.SYMDEF_64", "//":
			continue // special — handled separately
		}

		name := decodeMemberName(e.rawName, longNameTable)
		if name == "" {
			continue
		}

		idx := len(ar.Members)
		offsetToMemberIdx[e.hdrOffset] = idx
		ar.Members = append(ar.Members, &ArchiveMember{
			Name: name,
			data: e.memberData,
		})
	}

	// ── Pass 3: parse symbol index ────────────────────────────────────────────

	for _, e := range entries {
		switch e.rawName {
		case "/", "__.SYMDEF":
			if err := parseSysVSymIndex32(e.memberData, ar, offsetToMemberIdx); err != nil {
				// non-fatal: fall back to exhaustive scan in MemberForSymbol
				_ = err
			}
		case "/SYM64/", "__.SYMDEF_64":
			if err := parseSysVSymIndex64(e.memberData, ar, offsetToMemberIdx); err != nil {
				_ = err
			}
		}
	}

	// ── Fallback: if symIndex is empty, scan every member ─────────────────────
	// This handles archives created without ranlib / without a symbol table.

	if len(ar.symIndex) == 0 {
		if err := ar.buildSymIndexByScan(); err != nil {
			return nil, fmt.Errorf("building archive symbol index by scan: %w", err)
		}
	}

	return ar, nil
}

// decodeMemberName converts a raw ar_name field to a plain file name.
//
//   "foo/"        → "foo"          (SysV short name, "/" is terminator)
//   "/42"         → long name at offset 42 in the "//" table (GNU)
//   "/42/"        → same
func decodeMemberName(rawName string, longNames []byte) string {
	if strings.HasPrefix(rawName, "/") && len(rawName) > 1 && rawName[1] != '/' {
		// GNU long-name reference: "/NNN" where NNN is the byte offset into "//".
		offStr := strings.TrimRight(rawName[1:], "/ ")
		n, err := strconv.Atoi(offStr)
		if err != nil || longNames == nil || n >= len(longNames) {
			return ""
		}
		// Names in the "//" table are '/' terminated.
		end := n
		for end < len(longNames) && longNames[end] != '/' {
			end++
		}
		return string(longNames[n:end])
	}

	// SysV: name ends with "/" (short names only, ≤15 chars).
	return strings.TrimRight(rawName, "/ ")
}

// parseSysVSymIndex32 parses the SysV 32-bit "/" symbol-index member.
//
// Wire format:
//   uint32be  nsyms
//   uint32be[nsyms]  archive file offsets of member headers for each symbol
//   null-terminated strings: nsyms symbol names in the same order
func parseSysVSymIndex32(data []byte, ar *Archive, offsetToMember map[int]int) error {
	r := newReader(data)
	if len(data) < 4 {
		return fmt.Errorf("sym index too small")
	}
	nsyms, _ := r.u32be(0)
	tableSize := 4 + int(nsyms)*4
	if len(data) < tableSize {
		return fmt.Errorf("sym index truncated at offsets table (nsyms=%d)", nsyms)
	}

	// Read offsets.
	offsets := make([]uint32, nsyms)
	for i := range offsets {
		offsets[i], _ = r.u32be(4 + i*4)
	}

	// Read names from the string table that follows.
	strtab := data[tableSize:]
	pos := 0
	for i := 0; i < int(nsyms); i++ {
		end := pos
		for end < len(strtab) && strtab[end] != 0 {
			end++
		}
		name := string(strtab[pos:end])
		pos = end + 1

		if idx, ok := offsetToMember[int(offsets[i])]; ok {
			ar.symIndex[name] = idx
		}
	}
	return nil
}

// parseSysVSymIndex64 parses the GNU "/SYM64/" symbol-index member.
// Same as the 32-bit variant but with uint64 counts and offsets.
func parseSysVSymIndex64(data []byte, ar *Archive, offsetToMember map[int]int) error {
	r := newReader(data)
	if len(data) < 8 {
		return fmt.Errorf("sym index64 too small")
	}
	nsyms, _ := r.u64be(0)
	tableSize := 8 + int(nsyms)*8
	if len(data) < tableSize {
		return fmt.Errorf("sym index64 truncated")
	}

	offsets := make([]uint64, nsyms)
	for i := range offsets {
		offsets[i], _ = r.u64be(8 + i*8)
	}

	strtab := data[tableSize:]
	pos := 0
	for i := 0; i < int(nsyms); i++ {
		end := pos
		for end < len(strtab) && strtab[end] != 0 {
			end++
		}
		name := string(strtab[pos:end])
		pos = end + 1

		if idx, ok := offsetToMember[int(offsets[i])]; ok {
			ar.symIndex[name] = idx
		}
	}
	return nil
}

// buildSymIndexByScan exhaustively parses every member to build symIndex.
// Used only when the archive has no pre-built symbol table.
func (a *Archive) buildSymIndexByScan() error {
	for idx, m := range a.Members {
		obj, err := m.Object()
		if err != nil {
			continue // skip unparseable members
		}
		for _, sym := range obj.Symbols {
			if sym.Bind != stbGlobal && sym.Bind != stbWeak {
				continue
			}
			if sym.ShndxRaw == shnUndef || sym.Name == "" {
				continue
			}
			if _, exists := a.symIndex[sym.Name]; !exists {
				a.symIndex[sym.Name] = idx
			}
		}
	}
	return nil
}