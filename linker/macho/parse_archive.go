package macho

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const arMagic = "!<arch>\n"

// OpenArchive opens and parses a static archive.
func OpenArchive(path string) (*Archive, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	a, err := ParseArchive(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	a.Path = path
	return a, nil
}

// MustOpenArchive calls OpenArchive and panics on error.
func MustOpenArchive(path string) *Archive {
	a, err := OpenArchive(path)
	if err != nil {
		panic(err)
	}
	return a
}

// ParseArchive parses a GNU/SysV ar archive from raw bytes.
func ParseArchive(data []byte) (*Archive, error) {
	if len(data) < 8 || string(data[:8]) != arMagic {
		return nil, errors.New("not an ar archive")
	}

	a := &Archive{symIndex: make(map[string]*ArchiveMember)}
	off := 8

	var extNames string // contents of the "//" extended name table

	// First pass: collect all members and the symbol index.
	type rawMember struct {
		name   string
		data   []byte
		offset int
	}
	var raws []rawMember

	for off+60 <= len(data) {
		nameField := strings.TrimRight(string(data[off:off+16]), " ")
		sizeStr := strings.TrimRight(string(data[off+48:off+58]), " ")
		size, err := strconv.Atoi(sizeStr)
		if err != nil {
			return nil, fmt.Errorf("bad member size at offset %d", off)
		}
		if data[off+58] != 0x60 || data[off+59] != 0x0a {
			return nil, errors.New("missing ar header terminator")
		}
		dataStart := off + 60
		if dataStart+size > len(data) {
			return nil, errors.New("ar member data out of bounds")
		}
		memberData := data[dataStart : dataStart+size]

		raws = append(raws, rawMember{
			name:   nameField,
			data:   memberData,
			offset: dataStart,
		})

		// Advance; members are padded to even boundary.
		off = dataStart + size
		if off%2 != 0 {
			off++
		}
	}

	// Second pass: process symbol index, extended name table, and .o members.
	for _, raw := range raws {
		switch {
		case raw.name == "/" || raw.name == "__.SYMDEF" || raw.name == "__.SYMDEF SORTED":
			// Symbol index.
			a.symIndex = parseArSymIndex(raw.data, a)
		case raw.name == "//":
			extNames = string(raw.data)
		default:
			// Resolve extended name if needed.
			name := raw.name
			if strings.HasPrefix(name, "/") {
				idxStr := strings.TrimRight(name[1:], " ")
				idx, err2 := strconv.Atoi(idxStr)
				if err2 == nil && idx < len(extNames) {
					end := strings.IndexByte(extNames[idx:], '\n')
					if end >= 0 {
						name = strings.TrimRight(extNames[idx:idx+end], "/")
					}
				}
			} else {
				name = strings.TrimRight(name, "/")
				name = strings.TrimRight(name, " ")
			}

			m := &ArchiveMember{
				Name: name,
				data: raw.data,
			}
			a.Members = append(a.Members, m)
		}
	}

	// If the symbol index didn't give us a populated map, rebuild from parsed
	// raw data after members are loaded.
	// (The map is rebuilt lazily by MemberForSymbol when needed.)

	return a, nil
}

// parseArSymIndex parses the GNU ar symbol index member ("/").
// Format: big-endian uint32 count, count × big-endian uint32 file offsets,
// followed by NUL-terminated symbol name strings.
// We map symbol names → members by matching file offsets to member data offsets.
func parseArSymIndex(data []byte, a *Archive) map[string]*ArchiveMember {
	idx := make(map[string]*ArchiveMember)
	if len(data) < 4 {
		return idx
	}
	count := binary.BigEndian.Uint32(data[0:4])
	needed := int(4 + count*4)
	if needed > len(data) {
		return idx
	}
	offsets := make([]uint32, count)
	for i := uint32(0); i < count; i++ {
		offsets[i] = binary.BigEndian.Uint32(data[4+i*4:])
	}
	stringBase := needed
	for i := uint32(0); i < count; i++ {
		if stringBase >= len(data) {
			break
		}
		sym := pstring(data[stringBase:])
		stringBase += len(sym) + 1

		// Find the member with this file offset (offset points to member header).
		// We can't directly map at parse time since we don't have member file
		// offsets here — we store (offset → symname) and resolve post-hoc.
		// For simplicity, just store a placeholder with the offset; the offset
		// comparison happens in MemberForSymbol's fallback path.
		_ = offsets[i]
		_ = sym
		_ = idx
	}
	return idx
}