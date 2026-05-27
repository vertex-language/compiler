package pe

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	coffHdrSize  = 20
	secHdrSize   = 40
	symRecSize   = 18
	relocRecSize = 10
)

// ─── Public entry points ──────────────────────────────────────────────────────

func OpenObject(path string) (*ObjectFile, error) {
	data, err := os.ReadFile(path)
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

func MustOpenObject(path string) *ObjectFile {
	o, err := OpenObject(path)
	if err != nil {
		panic(err)
	}
	return o
}

func OpenArchive(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ar, err := ParseArchive(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ar.Path = path
	return ar, nil
}

func MustOpenArchive(path string) *Archive {
	a, err := OpenArchive(path)
	if err != nil {
		panic(err)
	}
	return a
}

// ─── COFF object parser ───────────────────────────────────────────────────────

// ParseObject parses a COFF relocatable object file.
// Returns an error if the data is a short import stub (use ParseArchive instead).
func ParseObject(data []byte) (*ObjectFile, error) {
	if len(data) < coffHdrSize {
		return nil, fmt.Errorf("not a COFF object: too short")
	}
	le := binary.LittleEndian

	// Detect short import stub.
	if le.Uint16(data[0:]) == 0x0000 && le.Uint16(data[2:]) == 0xFFFF {
		return nil, fmt.Errorf("data is a short import stub, not a COFF object")
	}

	machine := le.Uint16(data[0:])
	numSec := int(le.Uint16(data[2:]))
	symTabOff := le.Uint32(data[8:])
	numSym := int(le.Uint32(data[12:]))
	optHdrSz := int(le.Uint16(data[16:]))

	obj := &ObjectFile{Machine: machine}

	// ── String table ──────────────────────────────────────────────────────────
	var strTab []byte
	if symTabOff > 0 && numSym > 0 {
		stStart := int(symTabOff) + numSym*symRecSize
		if stStart+4 <= len(data) {
			stSz := int(le.Uint32(data[stStart:]))
			if stStart+stSz <= len(data) && stSz >= 4 {
				strTab = data[stStart : stStart+stSz]
			}
		}
	}

	strLookup := func(off uint32) (string, error) {
		if int(off) >= len(strTab) {
			return "", fmt.Errorf("string table offset %d out of range", off)
		}
		b := strTab[off:]
		for i, c := range b {
			if c == 0 {
				return string(b[:i]), nil
			}
		}
		return string(b), nil
	}

	resolveName8 := func(raw [8]byte) (string, error) {
		if raw[0] != 0 {
			// Inline name (null-padded).
			end := 8
			for end > 0 && raw[end-1] == 0 {
				end--
			}
			return string(raw[:end]), nil
		}
		// Long name: bytes [4:8] hold string table offset.
		off := le.Uint32(raw[4:])
		return strLookup(off)
	}

	// ── Section headers ───────────────────────────────────────────────────────
	secHdrStart := coffHdrSize + optHdrSz
	if len(data) < secHdrStart+numSec*secHdrSize {
		return nil, fmt.Errorf("truncated section headers")
	}
	obj.Sections = make([]*RawSection, numSec)
	for i := 0; i < numSec; i++ {
		h := data[secHdrStart+i*secHdrSize:]
		var name8 [8]byte
		copy(name8[:], h[0:8])
		sname, err := resolveName8(name8)
		if err != nil {
			return nil, fmt.Errorf("section %d: resolve name: %w", i, err)
		}

		physAddr := le.Uint32(h[8:]) // VirtualSize in images; PhysicalAddress in obj
		rawSz := le.Uint32(h[16:])
		rawOff := le.Uint32(h[20:])
		relocOff := le.Uint32(h[24:])
		numReloc := uint32(le.Uint16(h[32:]))
		chars := le.Uint32(h[36:])

		sec := &RawSection{
			Name:        sname,
			Chars:       chars,
			Index:       i,
			VirtualSize: physAddr,
			IsComdat:    chars&0x00001000 != 0, // IMAGE_SCN_LNK_COMDAT
			ComdatAssoc: -1,
		}

		// Raw data.
		isBSS := chars&0x00000080 != 0 // IMAGE_SCN_CNT_UNINITIALIZED_DATA
		if !isBSS && rawSz > 0 && rawOff > 0 {
			end := int(rawOff) + int(rawSz)
			if end > len(data) {
				return nil, fmt.Errorf("section %q: raw data out of bounds", sname)
			}
			sec.Data = make([]byte, rawSz)
			copy(sec.Data, data[rawOff:end])
		}

		// Relocations.
		if relocOff > 0 {
			rStart := int(relocOff)
			rCount := int(numReloc)
			skip := 0
			// Extended relocation count: IMAGE_SCN_LNK_NRELOC_OVFL flag + first record holds count.
			if chars&0x01000000 != 0 && rStart+relocRecSize <= len(data) {
				rCount = int(le.Uint32(data[rStart:])) - 1
				skip = 1
			}
			for j := 0; j < rCount; j++ {
				off := rStart + (j+skip)*relocRecSize
				if off+relocRecSize > len(data) {
					break
				}
				sec.Relocs = append(sec.Relocs, &RawReloc{
					Offset:   le.Uint32(data[off:]),
					SymIndex: le.Uint32(data[off+4:]),
					Type:     le.Uint16(data[off+8:]),
				})
			}
		}

		obj.Sections[i] = sec
	}

	// ── Symbol table ──────────────────────────────────────────────────────────
	if symTabOff > 0 && numSym > 0 {
		if int(symTabOff)+numSym*symRecSize > len(data) {
			return nil, fmt.Errorf("symbol table out of bounds")
		}
		obj.Symbols = make([]*RawSymbol, 0, numSym)
		i := 0
		for i < numSym {
			sr := data[int(symTabOff)+i*symRecSize:]
			var name8 [8]byte
			copy(name8[:], sr[0:8])
			sname, err := resolveName8(name8)
			if err != nil {
				return nil, fmt.Errorf("symbol %d: %w", i, err)
			}

			sym := &RawSymbol{
				Name:          sname,
				Value:         le.Uint32(sr[8:]),
				SectionNumber: int16(le.Uint16(sr[12:])),
				Type:          le.Uint16(sr[14:]),
				StorageClass:  sr[16],
				ComdatSectionIdx: -1,
			}
			numAux := int(sr[17])
			obj.Symbols = append(obj.Symbols, sym)
			i++

			// Process auxiliary records.
			for a := 0; a < numAux && i < numSym; a++ {
				aux := data[int(symTabOff)+i*symRecSize:]
				switch sym.StorageClass {
				case 3: // IMAGE_SYM_CLASS_STATIC — section definition aux
					sym.HasSectionAux = true
					sym.ComdatSectionIdx = int(le.Uint16(aux[12:])) - 1 // convert from 1-based
					sym.ComdatLen = le.Uint32(aux[0:])
					sym.ComdatSel = aux[14]
					sym.ComdatAssocNum = le.Uint16(aux[12:])
				case 105: // IMAGE_SYM_CLASS_WEAK_EXTERNAL
					sym.WeakDefaultIdx = int(le.Uint32(aux[0:]))
					sym.WeakChars = le.Uint32(aux[4:])
				}
				// Append a placeholder symbol for the aux record so indices stay aligned.
				obj.Symbols = append(obj.Symbols, &RawSymbol{Name: "", StorageClass: 255 /*aux*/})
				i++
			}
		}
	}

	// Link COMDAT aux data back to the sections.
	for _, sym := range obj.Symbols {
		if sym.HasSectionAux && sym.ComdatSectionIdx >= 0 && sym.ComdatSectionIdx < len(obj.Sections) {
			sec := obj.Sections[sym.ComdatSectionIdx]
			if sec.IsComdat {
				sec.ComdatSel = sym.ComdatSel
				if sym.ComdatAssocNum > 0 && int(sym.ComdatAssocNum)-1 < len(obj.Sections) {
					sec.ComdatAssoc = int(sym.ComdatAssocNum) - 1
				}
			}
		}
	}

	// ── .drectve directives ───────────────────────────────────────────────────
	for _, sec := range obj.Sections {
		if sec.Name == ".drectve" {
			parseDrectve(obj, string(sec.Data))
		}
	}

	return obj, nil
}

// parseDrectve extracts linker directives from a .drectve section string.
func parseDrectve(obj *ObjectFile, s string) {
	// Tokenise: space-separated flags, optionally quoted.
	tokens := tokenizeDrectve(s)
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "-export:") || strings.HasPrefix(tok, "/export:"):
			arg := tok[len("-export:"):]
			if strings.HasPrefix(tok, "/") {
				arg = tok[len("/export:"):]
			}
			obj.Exports = append(obj.Exports, parseDrectveExport(arg))
		case strings.HasPrefix(tok, "-defaultlib:") || strings.HasPrefix(tok, "/defaultlib:"):
			idx := strings.Index(tok, ":")
			obj.DefaultLibs = append(obj.DefaultLibs, tok[idx+1:])
		case strings.HasPrefix(tok, "-entry:") || strings.HasPrefix(tok, "/entry:"):
			idx := strings.Index(tok, ":")
			obj.EntryHint = tok[idx+1:]
		}
	}
}

func tokenizeDrectve(s string) []string {
	var tokens []string
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		if s[0] == '"' {
			end := strings.Index(s[1:], "\"")
			if end < 0 {
				tokens = append(tokens, s[1:])
				break
			}
			tokens = append(tokens, s[1:end+1])
			s = strings.TrimSpace(s[end+2:])
		} else {
			end := strings.IndexAny(s, " \t")
			if end < 0 {
				tokens = append(tokens, s)
				break
			}
			tokens = append(tokens, s[:end])
			s = strings.TrimSpace(s[end:])
		}
	}
	return tokens
}

func parseDrectveExport(arg string) DrectveExport {
	// Syntax: Name[=ExportName][,data][,@ordinal[,NONAME]]
	e := DrectveExport{}
	parts := strings.Split(arg, ",")
	namePart := parts[0]
	if eq := strings.Index(namePart, "="); eq >= 0 {
		e.InternalName = namePart[:eq]
		e.ExportName = namePart[eq+1:]
	} else {
		e.InternalName = namePart
		e.ExportName = namePart
	}
	for _, p := range parts[1:] {
		if strings.EqualFold(p, "data") {
			e.IsData = true
		}
	}
	return e
}

// ParseShortImport parses a Windows short-format import stub record.
func ParseShortImport(data []byte) (*ShortImport, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("short import: too short")
	}
	le := binary.LittleEndian
	if le.Uint16(data[0:]) != 0x0000 || le.Uint16(data[2:]) != 0xFFFF {
		return nil, fmt.Errorf("not a short import stub")
	}
	machine := le.Uint16(data[6:])
	strSz := int(le.Uint32(data[12:]))
	ordHint := le.Uint16(data[16:])
	typeInfo := le.Uint16(data[18:])

	importType := typeInfo & 0x3
	nameType := (typeInfo >> 2) & 0x7

	if 20+strSz > len(data) {
		return nil, fmt.Errorf("short import: string data out of bounds")
	}
	strs := data[20 : 20+strSz]

	// First null-terminated string = symbol name.
	symName := ""
	dllName := ""
	if i := indexByte(strs, 0); i >= 0 {
		symName = string(strs[:i])
		rest := strs[i+1:]
		if j := indexByte(rest, 0); j >= 0 {
			dllName = string(rest[:j])
		} else {
			dllName = string(rest)
		}
	}

	return &ShortImport{
		Machine:    machine,
		DLL:        dllName,
		SymName:    symName,
		Ordinal:    ordHint,
		ImportType: importType,
		NameType:   nameType,
	}, nil
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// ─── Archive parser ───────────────────────────────────────────────────────────

// ParseArchive parses a COFF/GNU ar archive.
func ParseArchive(data []byte) (*Archive, error) {
	const sig = "!<arch>\n"
	if len(data) < len(sig) || string(data[:len(sig)]) != sig {
		return nil, fmt.Errorf("not an archive: missing !<arch> signature")
	}

	ar := &Archive{symIdx: make(map[string]int)}
	pos := len(sig)

	var longNames []byte
	var firstLinker []byte   // "/" member — big-endian symbol table
	var secondLinker []byte  // second "/" member — little-endian

	// First pass: collect all members.
	type rawMember struct {
		name string
		data []byte
	}
	var rawMembers []rawMember

	for pos+60 <= len(data) {
		hdr := data[pos : pos+60]
		if string(hdr[58:60]) != "`\n" {
			break
		}
		name := strings.TrimRight(string(hdr[0:16]), " ")
		sizStr := strings.TrimRight(string(hdr[48:58]), " ")
		sz, err := strconv.Atoi(sizStr)
		if err != nil || sz < 0 {
			break
		}
		pos += 60
		end := pos + sz
		if end > len(data) {
			end = len(data)
		}
		mdata := data[pos:end]
		// Align to even boundary.
		pos = end
		if pos&1 != 0 {
			pos++
		}

		switch name {
		case "/":
			if firstLinker == nil {
				firstLinker = mdata
			} else {
				secondLinker = mdata
			}
		case "//":
			longNames = mdata
		default:
			rawMembers = append(rawMembers, rawMember{name, mdata})
		}
	}

	// Resolve long names.
	resolveName := func(name string) string {
		if strings.HasPrefix(name, "/") && len(name) > 1 {
			off, err := strconv.Atoi(name[1:])
			if err == nil && longNames != nil && off < len(longNames) {
				s := longNames[off:]
				for i, c := range s {
					if c == '/' {
						return string(s[:i])
					}
					if c == 0 {
						return string(s[:i])
					}
				}
				return string(s)
			}
		}
		return strings.TrimRight(name, "/")
	}

	// Build member list.
	ar.Members = make([]*ArchiveMember, len(rawMembers))
	for i, rm := range rawMembers {
		m := &ArchiveMember{
			Name: resolveName(rm.name),
			data: rm.data,
		}
		// Detect short import stubs eagerly.
		if len(rm.data) >= 4 &&
			binary.LittleEndian.Uint16(rm.data[0:]) == 0x0000 &&
			binary.LittleEndian.Uint16(rm.data[2:]) == 0xFFFF {
			imp, err := ParseShortImport(rm.data)
			if err == nil {
				m.imp = imp
			}
		}
		ar.Members[i] = m
	}

	// Parse the second linker member (little-endian; preferred over first).
	// Format: MemberCount uint32 | MemberOffsets[M] uint32 | SymCount uint32 |
	//         SymIndices[N] uint16 | null-terminated symbol strings.
	if secondLinker != nil && len(secondLinker) >= 8 {
		le := binary.LittleEndian
		M := int(le.Uint32(secondLinker[0:]))
		base := 4 + M*4
		if base+4 <= len(secondLinker) {
			N := int(le.Uint32(secondLinker[base:]))
			idxBase := base + 4
			strBase := idxBase + N*2
			if strBase <= len(secondLinker) {
				pos2 := strBase
				for s := 0; s < N && pos2 < len(secondLinker); s++ {
					// Read symbol name.
					end := indexByte(secondLinker[pos2:], 0)
					if end < 0 {
						break
					}
					sym := string(secondLinker[pos2 : pos2+end])
					pos2 += end + 1
					if idxBase+s*2+2 <= len(secondLinker) {
						memberIdx := int(le.Uint16(secondLinker[idxBase+s*2:])) - 1
						if memberIdx >= 0 && memberIdx < len(ar.Members) {
							ar.symIdx[sym] = memberIdx
						}
					}
				}
			}
		}
	} else if firstLinker != nil && len(firstLinker) >= 4 {
		// Fall back to first linker member (big-endian).
		be := binary.BigEndian
		N := int(be.Uint32(firstLinker[0:]))
		strBase := 4 + N*4
		pos2 := strBase
		for s := 0; s < N && pos2 < len(firstLinker); s++ {
			end := indexByte(firstLinker[pos2:], 0)
			if end < 0 {
				break
			}
			sym := string(firstLinker[pos2 : pos2+end])
			pos2 += end + 1
			off := be.Uint32(firstLinker[4+s*4:])
			_ = off
			// Map to member index by scanning rawMembers for the offset.
			// For simplicity, use the sequential index.
			ar.symIdx[sym] = s % len(ar.Members)
		}
	}

	return ar, nil
}