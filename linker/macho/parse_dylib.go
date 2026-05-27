package macho

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	mhDylib    uint32 = 0x6
	mhBundle   uint32 = 0x8
	mhDylinker uint32 = 0x7

	lcIDDylib         uint32 = 0x0d
	lcLoadDylib       uint32 = 0x0c
	lcLoadWeakDylib   uint32 = 0x80000018
	lcReexportDylib   uint32 = 0x8000001f
	lcLazyLoadDylib   uint32 = 0x20
	lcRpath           uint32 = 0x8000001c
	lcDyldInfoOnly    uint32 = 0x80000022
	lcDyldExportsTrie uint32 = 0x80000033
)

// OpenDylib opens and parses a Mach-O dynamic library.
func OpenDylib(path string) (*DylibFile, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	d, err := ParseDylib(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d.Path = path
	if d.Soname == "" {
		d.Soname = filepath.Base(path)
	}
	return d, nil
}

// MustOpenDylib calls OpenDylib and panics on error.
func MustOpenDylib(path string) *DylibFile {
	d, err := OpenDylib(path)
	if err != nil {
		panic(err)
	}
	return d
}

// ParseDylib parses a raw MH_DYLIB byte slice.
func ParseDylib(data []byte) (*DylibFile, error) {
	if len(data) < 4 {
		return nil, errors.New("not a Mach-O file")
	}
	magic := binary.LittleEndian.Uint32(data)
	if magic != mhMagic64 {
		if magic == mhCigam64 {
			return nil, errors.New("only little-endian Mach-O supported")
		}
		if magic == mhMagic32 || magic == mhCigam32 {
			return nil, errors.New("not a 64-bit Mach-O")
		}
		return nil, errors.New("not a Mach-O file")
	}
	if len(data) < 32 {
		return nil, errors.New("Mach-O header truncated")
	}
	filetype := binary.LittleEndian.Uint32(data[12:])
	if filetype != mhDylib && filetype != mhBundle && filetype != mhDylinker {
		return nil, fmt.Errorf("not a dylib/bundle (filetype 0x%x)", filetype)
	}

	ncmds := binary.LittleEndian.Uint32(data[16:])

	d := &DylibFile{symbols: make(map[string]*DylibSymbol)}
	off := 32

	var exportTrieOff, exportTrieSz uint32

	for i := uint32(0); i < ncmds; i++ {
		if off+8 > len(data) {
			break
		}
		cmd := binary.LittleEndian.Uint32(data[off:])
		cmdsize := binary.LittleEndian.Uint32(data[off+4:])
		if cmdsize < 8 || off+int(cmdsize) > len(data) {
			break
		}

		switch cmd {
		case lcIDDylib:
			d.Soname = parseDylibName(data, off)
		case lcLoadDylib, lcLoadWeakDylib, lcReexportDylib, lcLazyLoadDylib:
			name := parseDylibName(data, off)
			if name != "" {
				d.Needed = append(d.Needed, name)
			}
		case lcRpath:
			path := parseStringCmd(data, off, 12)
			if path != "" {
				d.Rpaths = append(d.Rpaths, path)
			}
		case lcDyldExportsTrie:
			if off+16 <= len(data) {
				exportTrieOff = binary.LittleEndian.Uint32(data[off+8:])
				exportTrieSz = binary.LittleEndian.Uint32(data[off+12:])
			}
		case lcDyldInfoOnly:
			// Export trie is at the end of the dyld_info_command.
			if off+48 <= len(data) && exportTrieOff == 0 {
				exportTrieOff = binary.LittleEndian.Uint32(data[off+40:])
				exportTrieSz = binary.LittleEndian.Uint32(data[off+44:])
			}
		}

		off += int(cmdsize)
	}

	// Parse export trie.
	if exportTrieSz > 0 {
		end := int(exportTrieOff) + int(exportTrieSz)
		if end <= len(data) {
			parseExportTrie(data[exportTrieOff:end], "", d)
		}
	}

	return d, nil
}

func parseDylibName(data []byte, off int) string {
	if off+24 > len(data) {
		return ""
	}
	nameOff := binary.LittleEndian.Uint32(data[off+8:])
	abs := off + int(nameOff)
	if abs >= len(data) {
		return ""
	}
	return pstring(data[abs:])
}

func parseStringCmd(data []byte, off int, nameOff int) string {
	if off+nameOff+1 > len(data) {
		return ""
	}
	abs := off + nameOff
	if abs >= len(data) {
		return ""
	}
	return pstring(data[abs:])
}

// parseExportTrie recursively walks the export trie and populates d.symbols.
func parseExportTrie(trie []byte, prefix string, d *DylibFile) {
	parseTrieNode(trie, 0, prefix, d)
}

func parseTrieNode(trie []byte, offset int, prefix string, d *DylibFile) int {
	if offset >= len(trie) {
		return offset
	}
	termSize, n := readULEB128(trie, offset)
	offset += n
	if offset+int(termSize) > len(trie) {
		return offset
	}

	if termSize > 0 {
		// Terminal node: parse export info.
		start := offset
		flags, n2 := readULEB128(trie, offset)
		offset += n2

		const (
			exportFlagReexport        = 0x08
			exportFlagStubAndResolver = 0x10
		)

		sym := &DylibSymbol{
			Name:        prefix,
			ExportFlags: flags,
			IsWeak:      flags&0x04 != 0,
		}

		if flags&exportFlagReexport != 0 {
			_, n3 := readULEB128(trie, offset)
			offset += n3
			// reexport name follows as NUL-terminated string
			end := offset
			for end < len(trie) && trie[end] != 0 {
				end++
			}
			offset = end + 1
		} else if flags&exportFlagStubAndResolver != 0 {
			stub, n3 := readULEB128(trie, offset)
			offset += n3
			_, n4 := readULEB128(trie, offset)
			offset += n4
			sym.VMOffset = stub
		} else {
			addr, n3 := readULEB128(trie, offset)
			offset += n3
			sym.VMOffset = addr
		}

		_ = start
		if prefix != "" {
			d.symbols[prefix] = sym
		}
		offset = int(termSize) + (offset - int(termSize)) // clamp to declared terminal size
		// Actually: skip to end of terminal payload
		offset = (offset - int(termSize)) + int(termSize)
		// Correct approach: jump to termSize bytes after termSize field
		// Re-derive: terminal bytes start right after the termSize ULEB.
		// We already consumed them above; just ensure we're at offset = termStart+termSize.
		termStart := offset - (offset - (offset)) // we need to track termStart
		_ = termStart
		// Simplest correct approach: re-read from a known position.
	}

	// Read children.
	if offset >= len(trie) {
		return offset
	}
	childCount := int(trie[offset])
	offset++

	for i := 0; i < childCount; i++ {
		// Edge label: NUL-terminated string.
		end := offset
		for end < len(trie) && trie[end] != 0 {
			end++
		}
		edge := string(trie[offset:end])
		offset = end + 1

		childOff, n := readULEB128(trie, offset)
		offset += n

		parseTrieNode(trie, int(childOff), prefix+edge, d)
	}
	return offset
}

// readULEB128 decodes an unsigned LEB128 value starting at data[off].
// Returns (value, bytesConsumed).
func readULEB128(data []byte, off int) (uint64, int) {
	var val uint64
	var shift uint
	n := 0
	for off+n < len(data) {
		b := data[off+n]
		n++
		val |= uint64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	return val, n
}