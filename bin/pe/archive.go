package pe

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// ────────────────────────────────────────────────────────────────────────────
// ArchiveBuilder – static .lib files (COFF archive)
// ────────────────────────────────────────────────────────────────────────────

// ArchiveMember is one COFF object file inside a .lib archive.
type ArchiveMember struct {
	// Name is the member filename (e.g. "foo.obj"). Must be ≤ 15 chars for
	// short names; longer names are placed in the longnames member automatically.
	Name string
	// Data is the raw COFF object file bytes.
	Data []byte
	// Symbols lists the public symbols defined in Data, for the linker symbol table.
	Symbols []string
}

// ArchiveBuilder builds a Windows COFF archive (.lib) from a set of object members.
type ArchiveBuilder struct {
	members []ArchiveMember
}

// NewArchiveBuilder returns a new ArchiveBuilder.
func NewArchiveBuilder() *ArchiveBuilder { return &ArchiveBuilder{} }

// AddMember appends a COFF object member.
func (ab *ArchiveBuilder) AddMember(m ArchiveMember) { ab.members = append(ab.members, m) }

// Emit serializes the archive and returns the raw .lib bytes.
//
// The output contains:
//   - Archive signature
//   - First linker member (big-endian sorted symbol offsets)
//   - Second linker member (little-endian member offsets + symbol indices)
//   - Longnames member (for member names > 15 characters)
//   - Object file members
func (ab *ArchiveBuilder) Emit() ([]byte, error) {
	const sig = "!<arch>\n"
	const hdrSize = 60
	const endMagic = "`\n"

	// ── Pass 1: assign each member an index and collect all symbols ──────────
	type memberInfo struct {
		name    string
		data    []byte
		symbols []string
	}
	members := make([]memberInfo, len(ab.members))
	for i, m := range ab.members {
		members[i] = memberInfo{m.Name, m.Data, m.Symbols}
	}

	// ── Longnames member ──────────────────────────────────────────────────────
	longNames := []byte{}
	longNameOffsets := make([]uint32, len(members))
	needLongNames := false
	for i, m := range members {
		if len(m.name) > 15 {
			longNameOffsets[i] = uint32(len(longNames))
			longNames = append(longNames, []byte(m.name)...)
			longNames = append(longNames, '/')
			needLongNames = true
		}
	}

	// ── Layout: compute file offsets for each member ──────────────────────────
	// We need to know offsets before building the linker members.
	// Order: sig | firstLinker | secondLinker | [longnames] | members

	// Placeholder sizes; we'll finalize after building linker members.
	// First, estimate by building everything with placeholder offsets, then rebuild.

	// ── Helper: serialize one archive member header ───────────────────────────
	padded := func(s string, n int, pad byte) []byte {
		b := make([]byte, n)
		copy(b, s)
		for i := len(s); i < n; i++ {
			b[i] = pad
		}
		return b
	}

	memberHeader := func(name string, size int) []byte {
		hdr := make([]byte, hdrSize)
		copy(hdr[0:16], padded(name, 16, ' '))
		copy(hdr[16:28], padded("0", 12, ' '))  // date
		copy(hdr[28:34], padded("0", 6, ' '))   // uid
		copy(hdr[34:40], padded("0", 6, ' '))   // gid
		copy(hdr[40:48], padded("0", 8, ' '))   // mode
		copy(hdr[48:58], padded(fmt.Sprintf("%d", size), 10, ' '))
		copy(hdr[58:60], endMagic)
		return hdr
	}

	pad2 := func(data []byte) []byte {
		if len(data)&1 != 0 {
			return append(data, '\n')
		}
		return data
	}

	// ── Build object member blobs ─────────────────────────────────────────────
	objBlobs := make([][]byte, len(members))
	for i, m := range members {
		var nameField string
		if len(m.name) <= 15 {
			nameField = m.name + "/"
		} else {
			nameField = fmt.Sprintf("/%d", longNameOffsets[i])
		}
		hdr := memberHeader(nameField, len(m.data))
		blob := append(hdr, m.data...)
		objBlobs[i] = pad2(blob)
	}

	// Compute layout offsets.
	// We build linker members with a two-pass approach.
	totalSymbols := 0
	for _, m := range members {
		totalSymbols += len(m.symbols)
	}

	// Sort all symbols for first linker member.
	type symEntry struct {
		name     string
		memberIdx int
	}
	var allSyms []symEntry
	for i, m := range members {
		for _, s := range m.symbols {
			allSyms = append(allSyms, symEntry{s, i})
		}
	}
	sort.Slice(allSyms, func(i, j int) bool { return allSyms[i].name < allSyms[j].name })

	// Estimate sizes to compute offsets.
	firstLinkerDataSize := func(memberOffsets []uint32) []byte {
		be := binary.BigEndian
		n := uint32(len(allSyms))
		buf := make([]byte, 4+n*4)
		be.PutUint32(buf[0:], n)
		for i, sym := range allSyms {
			be.PutUint32(buf[4+uint32(i)*4:], memberOffsets[sym.memberIdx])
		}
		for _, sym := range allSyms {
			buf = append(buf, []byte(sym.name)...)
			buf = append(buf, 0)
		}
		return buf
	}

	secondLinkerData := func(memberOffsets []uint32) []byte {
		le := binary.LittleEndian
		M := uint32(len(members))
		N := uint32(len(allSyms))
		// Build sorted-by-index symbol map.
		indices := make([]uint16, len(allSyms))
		for i, sym := range allSyms {
			indices[i] = uint16(sym.memberIdx + 1) // 1-based
		}
		buf := make([]byte, 4+M*4+4+N*2)
		le.PutUint32(buf[0:], M)
		for i, off := range memberOffsets {
			le.PutUint32(buf[4+uint32(i)*4:], off)
		}
		le.PutUint32(buf[4+M*4:], N)
		for i, idx := range indices {
			le.PutUint16(buf[4+M*4+4+uint32(i)*2:], idx)
		}
		for _, sym := range allSyms {
			buf = append(buf, []byte(sym.name)...)
			buf = append(buf, 0)
		}
		return buf
	}

	// Two-pass layout to resolve member file offsets.
	computeOffsets := func(firstLinkerSize, secondLinkerSize, longNamesSize int) []uint32 {
		cur := uint32(len(sig))
		cur += uint32(hdrSize + firstLinkerSize)
		cur = (cur + 1) &^ 1 // align to 2
		cur += uint32(hdrSize + secondLinkerSize)
		cur = (cur + 1) &^ 1
		if needLongNames {
			cur += uint32(hdrSize + longNamesSize)
			cur = (cur + 1) &^ 1
		}
		offsets := make([]uint32, len(objBlobs))
		for i, blob := range objBlobs {
			offsets[i] = cur
			cur += uint32(len(blob))
		}
		return offsets
	}

	// Pass 1: dummy offsets to get sizes.
	dummyOffsets := make([]uint32, len(members))
	fl1 := firstLinkerDataSize(dummyOffsets)
	sl1 := secondLinkerData(dummyOffsets)
	offsets := computeOffsets(len(pad2(fl1)), len(pad2(sl1)), len(longNames))

	// Pass 2: real offsets.
	fl2 := firstLinkerDataSize(offsets)
	sl2 := secondLinkerData(offsets)
	offsets = computeOffsets(len(pad2(fl2)), len(pad2(sl2)), len(longNames))

	// Final build with correct offsets.
	firstLinkerFinal := firstLinkerDataSize(offsets)
	secondLinkerFinal := secondLinkerData(offsets)

	// ── Assemble output ───────────────────────────────────────────────────────
	var out []byte
	out = append(out, []byte(sig)...)

	appendMember := func(name string, data []byte) {
		hdr := memberHeader(name, len(data))
		out = append(out, hdr...)
		out = append(out, data...)
		if len(data)&1 != 0 {
			out = append(out, '\n')
		}
	}

	appendMember("/", firstLinkerFinal)
	appendMember("/", secondLinkerFinal)
	if needLongNames {
		appendMember("//", longNames)
	}
	for _, blob := range objBlobs {
		out = append(out, blob...)
	}

	return out, nil
}

// ────────────────────────────────────────────────────────────────────────────
// ImportLibBuilder – Windows import library (.lib with short import stubs)
// ────────────────────────────────────────────────────────────────────────────

// ImportEntry describes one function or datum imported from a DLL.
type ImportEntry struct {
	// Name is the exported symbol name. Empty → ordinal-only import.
	Name string
	// ExportName overrides the looked-up name for IMPORT_NAME_EXPORTAS.
	// Usually empty.
	ExportName string
	// Ordinal is used for ordinal imports (when Name is empty) and stored as a hint otherwise.
	Ordinal uint16
	// Kind controls whether the import stub is CODE, DATA, or CONST.
	Kind uint16 // IMPORT_CODE, IMPORT_DATA, or IMPORT_CONST (default: IMPORT_CODE)
	// NameType controls the name-lookup strategy (default: IMPORT_NAME).
	NameType uint16
}

// ImportLibBuilder constructs a Windows import library from DLL export information.
// The result is a .lib file containing short-format COFF import stubs that the
// linker uses to generate the IAT at link time.
type ImportLibBuilder struct {
	machine MachineType
	dll     string
	entries []ImportEntry
}

// NewImportLibBuilder returns a new ImportLibBuilder targeting the given machine
// and DLL name (e.g. "kernel32.dll").
func NewImportLibBuilder(machine MachineType, dll string) *ImportLibBuilder {
	return &ImportLibBuilder{machine: machine, dll: dll}
}

// Add appends an import entry.
func (lb *ImportLibBuilder) Add(e ImportEntry) { lb.entries = append(lb.entries, e) }

// Emit serializes the import library and returns the raw .lib bytes.
//
// Each import is a "short import" archive member using the Windows short-format
// import header (Sig1=0x0000, Sig2=0xFFFF), which is the modern replacement for
// full COFF stub objects.
func (lb *ImportLibBuilder) Emit() ([]byte, error) {
	ab := NewArchiveBuilder()

	for _, e := range lb.entries {
		stub, syms := lb.makeShortImport(e)
		ab.AddMember(ArchiveMember{
			Name:    lb.dll,
			Data:    stub,
			Symbols: syms,
		})
	}

	return ab.Emit()
}

// makeShortImport builds one short-format import stub member and returns the
// public symbols it defines.
//
// Short import format (a minimal COFF-like header, NOT a full object file):
//
//	Sig1           uint16  = IMAGE_FILE_MACHINE_UNKNOWN (0x0000)
//	Sig2           uint16  = 0xFFFF
//	Version        uint16  = 0
//	Machine        uint16
//	TimeDateStamp  uint32
//	SizeOfData     uint32  (size of following string data)
//	OrdinalOrHint  uint16
//	TypeInfo       uint16  (ImportType[1:0] | ImportNameType[5:2])
//	<SizeOfData bytes: null-terminated symbol name, then null-terminated DLL name>
func (lb *ImportLibBuilder) makeShortImport(e ImportEntry) (data []byte, symbols []string) {
	le := binary.LittleEndian

	symName := e.Name
	dllName := lb.dll

	nameType := e.NameType
	if nameType == 0 && e.Name != "" {
		nameType = IMPORT_NAME
	}
	kind := e.Kind // defaults to IMPORT_CODE (0)

	// String data: symbol name (null-terminated) + DLL name (null-terminated).
	stringData := append([]byte(symName), 0)
	stringData = append(stringData, []byte(dllName)...)
	stringData = append(stringData, 0)

	data = make([]byte, 20+len(stringData))
	le.PutUint16(data[0:], 0x0000)                   // Sig1 = UNKNOWN
	le.PutUint16(data[2:], 0xFFFF)                   // Sig2
	le.PutUint16(data[4:], 0)                        // Version
	le.PutUint16(data[6:], uint16(lb.machine))
	le.PutUint32(data[8:], 0)                        // TimeDateStamp
	le.PutUint32(data[12:], uint32(len(stringData))) // SizeOfData
	le.PutUint16(data[16:], e.Ordinal)               // OrdinalOrHint
	typeInfo := kind | (nameType << 2)
	le.PutUint16(data[18:], typeInfo)
	copy(data[20:], stringData)

	// The linker expects to find "__imp_" + symName and symName itself as symbols.
	if symName != "" {
		symbols = []string{"__imp_" + symName, symName}
	}
	return
}