// Package linker — archive.go
// Reads GNU/SysV ar archives (.a static libraries).
//
// Archive layout (per the SysV / GNU spec):
//
//   "!<arch>\n"          ← 8-byte magic
//   member*              ← zero or more members, each:
//     [60-byte header]
//       ar_name[16]      right-padded with spaces; ends with '/' for GNU
//       ar_date[12]      mtime, ASCII decimal
//       ar_uid[6]        owner UID, ASCII decimal
//       ar_gid[6]        owner GID, ASCII decimal
//       ar_mode[8]       file mode, ASCII octal
//       ar_size[10]      data size in bytes, ASCII decimal
//       ar_fmag[2]       "`\n" (0x60 0x0A)
//     [data bytes]       ar_size bytes, padded to even boundary with '\n'
//
// Special members:
//   "/"    GNU symbol index (big-endian uint32 count, offsets, names)
//   "//"   GNU long-name string table
package linker

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const arMagic = "!<arch>\n"
const arHdrSize = 60

// ArMember is one regular (non-special) member extracted from a .a archive.
type ArMember struct {
	Name string // decoded file name
	Data []byte // raw member bytes (e.g. an ELF .o file)
}

// ReadArchive parses a GNU/SysV ar archive and returns its regular members
// in order.  The symbol index ("/") and long-name table ("//") are consumed
// internally.
func ReadArchive(data []byte) ([]ArMember, error) {
	if len(data) < len(arMagic) || string(data[:len(arMagic)]) != arMagic {
		return nil, fmt.Errorf("archive: not a valid .a file (bad magic)")
	}

	var longNames []byte
	var members []ArMember
	pos := len(arMagic)

	for pos < len(data) {
		if len(data)-pos < arHdrSize {
			break // trailing padding bytes; not an error
		}

		hdr := data[pos : pos+arHdrSize]
		pos += arHdrSize

		// Verify the header terminator.
		if hdr[58] != '`' || hdr[59] != '\n' {
			return nil, fmt.Errorf("archive: bad member header at offset %d", pos-arHdrSize)
		}

		rawName := strings.TrimRight(string(hdr[0:16]), " ")
		sizeStr := strings.TrimRight(string(hdr[48:58]), " ")
		size, err := strconv.Atoi(sizeStr)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("archive: invalid member size %q", sizeStr)
		}
		if len(data)-pos < size {
			return nil, fmt.Errorf("archive: member %q data truncated", rawName)
		}

		memberData := data[pos : pos+size]
		pos += size
		// Members are always at even byte boundaries.
		if size%2 != 0 && pos < len(data) {
			pos++ // skip '\n' padding byte
		}

		switch {
		case rawName == "/" || rawName == "/SYM64/":
			// Symbol index — used only by ArchiveSymbolIndex, skip here.
			continue

		case rawName == "//":
			// Long-name string table.
			cp := make([]byte, len(memberData))
			copy(cp, memberData)
			longNames = cp
			continue

		case strings.HasPrefix(rawName, "/"):
			// GNU long name: "/offset" indexes into the "//" member.
			offStr := strings.TrimRight(strings.TrimPrefix(rawName, "/"), " ")
			off, err := strconv.Atoi(offStr)
			if err != nil {
				return nil, fmt.Errorf("archive: invalid long-name ref %q", rawName)
			}
			rawName = readArLongName(longNames, off)
		}

		// GNU appends '/' to short names; strip it.
		name := strings.TrimRight(rawName, "/")

		cp := make([]byte, len(memberData))
		copy(cp, memberData)
		members = append(members, ArMember{Name: name, Data: cp})
	}
	return members, nil
}

// ArchiveSymbolIndex reads the GNU/SysV symbol index from the first "/"
// member and returns a map from symbol name to archive-file byte offset.
// Used to quickly determine which archive member defines a given symbol
// without scanning every member.
func ArchiveSymbolIndex(data []byte) (map[string]uint32, error) {
	if len(data) < len(arMagic) || string(data[:len(arMagic)]) != arMagic {
		return nil, fmt.Errorf("archive: invalid magic")
	}
	pos := len(arMagic)
	if len(data)-pos < arHdrSize {
		return nil, fmt.Errorf("archive: file too short for a member header")
	}

	hdr := data[pos : pos+arHdrSize]
	rawName := strings.TrimRight(string(hdr[0:16]), " ")
	if rawName != "/" && rawName != "/SYM64/" {
		return nil, fmt.Errorf("archive: first member is %q, not the symbol index", rawName)
	}
	sizeStr := strings.TrimRight(string(hdr[48:58]), " ")
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return nil, fmt.Errorf("archive: symbol index size: %w", err)
	}
	pos += arHdrSize
	if len(data)-pos < size {
		return nil, fmt.Errorf("archive: symbol index truncated")
	}
	idx := data[pos : pos+size]

	// GNU format:
	//   [4 bytes] count of symbols (big-endian uint32)
	//   [4*count] archive-file offsets per symbol (big-endian uint32)
	//   [remainder] null-terminated symbol names, one per symbol
	if len(idx) < 4 {
		return nil, fmt.Errorf("archive: symbol index body too short")
	}
	count := int(binary.BigEndian.Uint32(idx[0:4]))
	if len(idx) < 4+count*4 {
		return nil, fmt.Errorf("archive: symbol index offset table truncated")
	}

	offsets := make([]uint32, count)
	for i := range offsets {
		offsets[i] = binary.BigEndian.Uint32(idx[4+i*4:])
	}
	names := idx[4+count*4:]

	result := make(map[string]uint32, count)
	namePos := 0
	for i, arOff := range offsets {
		if namePos >= len(names) {
			return nil, fmt.Errorf("archive: symbol name %d out of range", i)
		}
		end := namePos
		for end < len(names) && names[end] != 0 {
			end++
		}
		result[string(names[namePos:end])] = arOff
		namePos = end + 1
	}
	return result, nil
}

// readArLongName extracts a null-or-slash-or-newline terminated name from the
// "//" long-name table at byte offset off.
func readArLongName(table []byte, off int) string {
	if off < 0 || off >= len(table) {
		return ""
	}
	end := off
	for end < len(table) && table[end] != '/' && table[end] != '\n' && table[end] != 0 {
		end++
	}
	return string(table[off:end])
}

// readMemberAt reads the single archive member whose 60-byte header starts
// at byte offset off within data.  Used by ingestArchive to fetch the specific
// members identified by the GNU symbol index without re-scanning everything.
func readMemberAt(data []byte, off uint32) (ArMember, error) {
	pos := int(off)
	if len(data)-pos < arHdrSize {
		return ArMember{}, fmt.Errorf("archive: offset %d out of file bounds", off)
	}

	hdr := data[pos : pos+arHdrSize]
	if hdr[58] != '`' || hdr[59] != '\n' {
		return ArMember{}, fmt.Errorf("archive: bad member header terminator at offset %d", off)
	}

	sizeStr := strings.TrimRight(string(hdr[48:58]), " ")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 {
		return ArMember{}, fmt.Errorf("archive: invalid member size %q at offset %d", sizeStr, off)
	}

	dataStart := pos + arHdrSize
	if len(data)-dataStart < size {
		return ArMember{}, fmt.Errorf("archive: member data at offset %d is truncated", off)
	}

	rawName := strings.TrimRight(string(hdr[0:16]), " /")
	cp := make([]byte, size)
	copy(cp, data[dataStart:dataStart+size])
	return ArMember{Name: rawName, Data: cp}, nil
}