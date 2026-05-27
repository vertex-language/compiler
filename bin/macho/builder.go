package macho

import (
	"errors"
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// PageSize is the VM page / segment alignment used on both AMD64 and ARM64
	// macOS.  16 KiB is the ARM64 native page; x86-64 accepts it without issue.
	// Exported so that linker packages can use the same value when computing
	// chained-fixup page-start arrays and layout arithmetic.
	PageSize uint64 = 0x4000

	// baseVA is the conventional load address for the first user segment.
	// __PAGEZERO occupies [0, baseVA).
	baseVA uint64 = 0x100000000 // 4 GiB — standard macOS convention

	dylinkerPath = "/usr/lib/dyld"
)

// ──────────────────────────────────────────────────────────────────────────────
// DyldMode selects which dynamic-linking load commands are emitted.
// ──────────────────────────────────────────────────────────────────────────────

// DyldMode selects the dynamic-linking infrastructure emitted into __LINKEDIT.
type DyldMode uint8

const (
	// DyldModeLegacy emits LC_DYLD_INFO_ONLY (macOS < 12 / iOS < 14).
	DyldModeLegacy DyldMode = iota
	// DyldModeChained emits LC_DYLD_CHAINED_FIXUPS + LC_DYLD_EXPORTS_TRIE
	// (macOS 12+ / iOS 14+).  Default for new builds.
	DyldModeChained
)

// ──────────────────────────────────────────────────────────────────────────────
// Builder
// ──────────────────────────────────────────────────────────────────────────────

// Builder accumulates segments, symbols, dylib references, metadata, and
// dynamic-linking tables, then serialises them into a complete 64-bit Mach-O
// image via Emit.
//
// Workflow for a dynamically linked executable:
//
//	b := macho.NewBuilder(macho.ArchARM64)
//	b.SetFileType(macho.FileTypeExecute)
//	b.SetBuildVersion(macho.BuildVersion{
//	    Platform: macho.PlatformMacOS,
//	    MinOS: macho.PackVersion(14, 0, 0),
//	    SDK:   macho.PackVersion(14, 5, 0),
//	})
//	b.AddDylib(macho.DylibRef{Path: "/usr/lib/libSystem.B.dylib", Kind: macho.DylibLoad, ...})
//	b.AddSegment(textSeg)
//	b.AddSegment(dataSeg)
//	b.SetEntry("_main")
//	b.SetChainedFixups(cfBuilder)
//	b.SetExportsTrie(BuildExportTrie(exports))
//	out, err := b.Emit()
type Builder struct {
	arch     Arch
	fileType FileType
	flags    MHFlags

	segments []Segment
	dylibs   []DylibRef
	symbols  []Symbol

	// Dylib identity for MH_DYLIB / MH_BUNDLE output.
	dylibID *DylibRef

	// Entry point (MH_EXECUTE / MH_KEXT_BUNDLE).
	entry string

	// Metadata load commands.
	buildVersion  *BuildVersion
	sourceVersion uint64 // packed A.B.C.D.E; 0 = omit
	uuid          *[16]byte
	rpaths        []string

	// LC_LINKER_OPTION strings (e.g. "-framework", "Foundation").
	linkerOptions [][]string

	// Dynamic linking mode.
	dyldMode DyldMode

	// Legacy: LC_DYLD_INFO_ONLY blobs (set via SetDyldInfo).
	legacyRebase   []byte
	legacyBind     []byte
	legacyWeakBind []byte
	legacyLazyBind []byte
	legacyExport   []byte

	// Modern: LC_DYLD_CHAINED_FIXUPS blob (set via SetChainedFixups).
	chainedFixupsData []byte
	// Modern: LC_DYLD_EXPORTS_TRIE blob (set via SetExportsTrie).
	exportsTrie []byte

	// LC_FUNCTION_STARTS blob (built via SetFunctionStarts).
	functionStarts []byte

	// LC_DATA_IN_CODE entries.
	dataInCode []DataInCodeEntry

	// LC_CODE_SIGNATURE: size reserved in __LINKEDIT (actual signing is external).
	codeSignatureSize uint32
}

// NewBuilder returns a Builder targeting the given architecture.
// The default file type is FileTypeExecute.
func NewBuilder(arch Arch) *Builder {
	return &Builder{
		arch:     arch,
		fileType: FileTypeExecute,
		dyldMode: DyldModeChained,
	}
}

// ── File type and flags ──────────────────────────────────────────────────────

// SetFileType sets the Mach-O file type.  Must be called before Emit.
func (b *Builder) SetFileType(ft FileType) { b.fileType = ft }

// SetFlags replaces the header flags.  The Builder will OR in required flags
// (MHDyldLink, MHTwoLevel, MHPie, etc.) automatically; this is for extra flags
// you need to set explicitly.
func (b *Builder) SetFlags(f MHFlags) { b.flags = f }

// ── Segments and sections ────────────────────────────────────────────────────

// AddSegment appends a segment (and all its sections) to the image.
// Segments are emitted in the order added; __TEXT should come first,
// followed by __DATA_CONST, __DATA, and any others.
// __PAGEZERO and __LINKEDIT are synthesised automatically.
func (b *Builder) AddSegment(seg Segment) {
	b.segments = append(b.segments, seg)
}

// ── Dynamic libraries ────────────────────────────────────────────────────────

// AddDylib records a dynamic library dependency (LC_LOAD_DYLIB and variants).
// The order of calls determines the 1-based dylib ordinal used in bind tables.
func (b *Builder) AddDylib(ref DylibRef) {
	b.dylibs = append(b.dylibs, ref)
}

// SetDylibID sets the identity of this image when building a dylib or bundle
// (LC_ID_DYLIB).  Has no effect on MH_EXECUTE output.
func (b *Builder) SetDylibID(ref DylibRef) {
	b.dylibID = &ref
}

// ── Symbols ──────────────────────────────────────────────────────────────────

// AddSymbol adds an entry to the symbol table.
// Local symbols must be added before global symbols.
func (b *Builder) AddSymbol(sym Symbol) {
	b.symbols = append(b.symbols, sym)
}

// ── Entry point ──────────────────────────────────────────────────────────────

// SetEntry names the entry-point symbol (LC_MAIN).  Required for MH_EXECUTE.
// The symbol must be present in the symbol table with a valid section.
func (b *Builder) SetEntry(name string) { b.entry = name }

// ── Metadata ─────────────────────────────────────────────────────────────────

// SetBuildVersion sets LC_BUILD_VERSION (platform, minimum OS, SDK, tools).
func (b *Builder) SetBuildVersion(bv BuildVersion) { b.buildVersion = &bv }

// SetSourceVersion sets LC_SOURCE_VERSION.
// Use PackSourceVersion to construct the uint64 value.
func (b *Builder) SetSourceVersion(v uint64) { b.sourceVersion = v }

// SetUUID embeds a UUID into LC_UUID.
// If never called the Builder omits LC_UUID; callers should generate a random UUID.
func (b *Builder) SetUUID(uuid [16]byte) { b.uuid = &uuid }

// AddRpath appends an LC_RPATH search path, e.g. "@executable_path/../Frameworks".
func (b *Builder) AddRpath(path string) { b.rpaths = append(b.rpaths, path) }

// AddLinkerOption appends one LC_LINKER_OPTION record (a slice of option strings,
// e.g. []string{"-framework", "Foundation"}).
func (b *Builder) AddLinkerOption(opts []string) {
	b.linkerOptions = append(b.linkerOptions, opts)
}

// ── Dynamic linking ──────────────────────────────────────────────────────────

// SetDyldMode selects whether to emit legacy (LC_DYLD_INFO_ONLY) or modern
// (LC_DYLD_CHAINED_FIXUPS + LC_DYLD_EXPORTS_TRIE) dynamic-linking commands.
// Default is DyldModeChained.
func (b *Builder) SetDyldMode(m DyldMode) { b.dyldMode = m }

// SetDyldInfo supplies pre-built blobs for LC_DYLD_INFO_ONLY (legacy mode).
// Use DyldInfoBuilder.Build() to produce the blobs.
func (b *Builder) SetDyldInfo(rebase, bind, weakBind, lazyBind, exportTrie []byte) {
	b.legacyRebase = rebase
	b.legacyBind = bind
	b.legacyWeakBind = weakBind
	b.legacyLazyBind = lazyBind
	b.legacyExport = exportTrie
	b.dyldMode = DyldModeLegacy
}

// SetChainedFixups supplies a pre-built LC_DYLD_CHAINED_FIXUPS blob (modern mode).
// Use ChainedFixupsBuilder.Build() to produce the blob.
func (b *Builder) SetChainedFixups(data []byte) {
	b.chainedFixupsData = data
	b.dyldMode = DyldModeChained
}

// SetExportsTrie supplies a pre-built LC_DYLD_EXPORTS_TRIE blob (modern mode).
// Use BuildExportTrie() to produce the blob.
func (b *Builder) SetExportsTrie(data []byte) {
	b.exportsTrie = data
}

// ── Linkedit extras ──────────────────────────────────────────────────────────

// SetFunctionStarts supplies the LC_FUNCTION_STARTS blob.
// Use BuildFunctionStarts() to produce it.
func (b *Builder) SetFunctionStarts(data []byte) { b.functionStarts = data }

// AddDataInCode appends a DataInCodeEntry for LC_DATA_IN_CODE.
func (b *Builder) AddDataInCode(e DataInCodeEntry) { b.dataInCode = append(b.dataInCode, e) }

// ReserveCodeSignature reserves space in __LINKEDIT for an ad-hoc or
// post-processed code signature of the given byte size.  The actual signature
// bytes are written by the caller after Emit returns (e.g. using codesign(1)).
func (b *Builder) ReserveCodeSignature(size uint32) { b.codeSignatureSize = size }

// ──────────────────────────────────────────────────────────────────────────────
// Emit
// ──────────────────────────────────────────────────────────────────────────────

// Emit serialises the complete Mach-O image and returns the raw bytes.
func (b *Builder) Emit() ([]byte, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	// ── Phase 1: dry-run load-command size ────────────────────────────────────
	lcSize := b.computeLoadCommandSize()
	headerAndLC := uint64(sizeofMachHeader64) + lcSize

	// ── Phase 2: assign file offsets and VAs to each section ─────────────────
	type sectionLayout struct {
		segIdx  int
		sectIdx int
		fileOff uint64
		vmAddr  uint64
		size    uint64 // byte count (0 for zerofill)
	}

	fileOff := alignUp(headerAndLC, PageSize) // ← was pageSize
	vmAddr := baseVA

	var layouts []sectionLayout

	// Track per-segment file/VM extents for the segment command.
	type segExtent struct {
		vmStart, vmEnd, fileStart, fileEnd uint64
		set                                bool
	}
	segExtents := make([]segExtent, len(b.segments))

	for si, seg := range b.segments {
		for ki, sect := range seg.Sections {
			a := uint64(sect.Align)
			if a < 1 {
				a = 1
			}
			isZerofill := (sect.Flags & 0xff) == S_ZEROFILL ||
				(sect.Flags&0xff) == S_GB_ZEROFILL ||
				(sect.Flags&0xff) == S_THREAD_LOCAL_ZEROFILL

			fileOff = alignUp(fileOff, a)
			vmAddr = alignUp(vmAddr, a)

			sz := uint64(len(sect.Data))
			if isZerofill && sz == 0 {
				sz = sect.Size
			}

			l := sectionLayout{
				segIdx:  si,
				sectIdx: ki,
				fileOff: fileOff,
				vmAddr:  vmAddr,
				size:    sz,
			}
			layouts = append(layouts, l)

			// Update segment extents.
			e := &segExtents[si]
			if !e.set {
				e.vmStart = vmAddr
				e.fileStart = fileOff
				e.set = true
			}
			vmEnd := vmAddr + sz
			if vmEnd > e.vmEnd {
				e.vmEnd = vmEnd
			}
			fileEndForSect := fileOff
			if !isZerofill {
				fileEndForSect = fileOff + sz
			}
			if fileEndForSect > e.fileEnd {
				e.fileEnd = fileEndForSect
			}

			if !isZerofill {
				fileOff += sz
			}
			vmAddr += sz
		}
	}

	// ── Phase 3: build symbol and string tables ───────────────────────────────
	type symEntry struct {
		strx  uint32
		ntype uint8
		nsect uint8
		ndesc uint16
		value uint64
	}

	strTable := []byte{0}
	addStr := func(s string) uint32 {
		idx := uint32(len(strTable))
		strTable = append(strTable, []byte(s)...)
		strTable = append(strTable, 0)
		return idx
	}

	// Build flat section list for 1-based index lookup.
	type flatSect struct {
		segName  string
		sectName string
		vmAddr   uint64
	}
	var flatSects []flatSect
	for si, seg := range b.segments {
		for ki := range seg.Sections {
			var l sectionLayout
			for _, ll := range layouts {
				if ll.segIdx == si && ll.sectIdx == ki {
					l = ll
					break
				}
			}
			flatSects = append(flatSects, flatSect{
				segName:  seg.Name,
				sectName: seg.Sections[ki].Name,
				vmAddr:   l.vmAddr,
			})
		}
	}
	sectIndex := func(segName, sectName string) uint8 {
		for i, fs := range flatSects {
			if fs.segName == segName && fs.sectName == sectName {
				return uint8(i + 1)
			}
		}
		return 0
	}

	var localSyms, extSyms []symEntry
	for _, sym := range b.symbols {
		strx := addStr(sym.Name)
		nsect := sectIndex(sym.SegmentName, sym.SectionName)
		ntype := nSect
		if nsect == 0 {
			ntype = nUndf
		}
		if sym.PrivateExtern {
			ntype |= nPExt
		}
		ndesc := sym.Desc
		if sym.Weak && nsect != 0 {
			ndesc |= nDescWeakDef
		}
		if sym.Weak && nsect == 0 {
			ndesc |= nDescWeakRef
		}
		if sym.AltEntry {
			ndesc |= nDescAltEntry
		}

		// Resolve value: if the symbol has a section, value is relative to
		// section start; we convert to absolute VA.
		value := sym.Value
		if nsect != 0 {
			value += flatSects[nsect-1].vmAddr
		}

		e := symEntry{
			strx:  strx,
			ntype: ntype,
			nsect: nsect,
			ndesc: ndesc,
			value: value,
		}
		if sym.Global || sym.PrivateExtern {
			e.ntype |= nExt
			extSyms = append(extSyms, e)
		} else {
			localSyms = append(localSyms, e)
		}
	}
	allSyms := append(localSyms, extSyms...)

	for len(strTable)%8 != 0 {
		strTable = append(strTable, 0)
	}

	// ── Phase 4: collect relocations ─────────────────────────────────────────
	type sectReloc struct {
		flatIdx int
		relocs  []Reloc
	}
	var allRelocs []sectReloc
	symNameIdx := func(name string) uint32 {
		for i, sym := range b.symbols {
			if sym.Name == name {
				return uint32(i)
			}
		}
		return 0
	}
	for si, seg := range b.segments {
		for ki, sect := range seg.Sections {
			if len(sect.Relocs) == 0 {
				continue
			}
			fi := -1
			for i, fs := range flatSects {
				if fs.segName == seg.Name && fs.sectName == sect.Name {
					fi = i
					break
				}
			}
			_ = si
			_ = ki
			if fi >= 0 {
				allRelocs = append(allRelocs, sectReloc{fi, sect.Relocs})
			}
		}
	}

	// ── Phase 5: build indirect symbol table ─────────────────────────────────
	var indirectSyms []uint32

	for _, seg := range b.segments {
		for _, sect := range seg.Sections {
			stype := sect.Flags & 0xff
			if stype == S_SYMBOL_STUBS ||
				stype == S_NON_LAZY_SYMBOL_POINTERS ||
				stype == S_LAZY_SYMBOL_POINTERS ||
				stype == S_LAZY_DYLIB_SYMBOL_POINTERS {
				entrySize := uint64(8)
				if stype == S_SYMBOL_STUBS && sect.Reserved2 > 0 {
					entrySize = uint64(sect.Reserved2)
				}
				sz := uint64(len(sect.Data))
				if sz == 0 {
					sz = sect.Size
				}
				count := uint64(0)
				if entrySize > 0 {
					count = sz / entrySize
				}
				for i := uint64(0); i < count; i++ {
					indirectSyms = append(indirectSyms, 0)
				}
			}
		}
	}

	// ── Phase 6: assign __LINKEDIT offsets ───────────────────────────────────
	linkeditFileStart := alignUp(fileOff, PageSize) // ← was pageSize
	linkeditVMAddr := alignUp(vmAddr, PageSize)     // ← was pageSize

	cur := linkeditFileStart

	var (
		rebaseOff, rebaseSz             uint32
		bindOff, bindSz                 uint32
		weakBindOff, weakBindSz         uint32
		lazyBindOff, lazyBindSz         uint32
		exportOff, exportSz             uint32
		chainedFixupsOff, chainedFixupsSz uint32
		exportTrieOff, exportTrieSz     uint32
		funcStartsOff, funcStartsSz     uint32
		dataInCodeOff, dataInCodeSz     uint32
		symOff, symSz                   uint32
		indirSymOff, indirSymSz         uint32
		strOff                          uint32
		codeSignOff                     uint32
	)

	assign := func(data []byte, off *uint32, sz *uint32, align uint64) {
		if len(data) == 0 {
			*off = 0
			*sz = 0
			return
		}
		cur = alignUp(cur, align)
		*off = uint32(cur)
		*sz = uint32(len(data))
		cur += uint64(len(data))
	}

	if b.dyldMode == DyldModeLegacy {
		assign(b.legacyRebase, &rebaseOff, &rebaseSz, 8)
		assign(b.legacyBind, &bindOff, &bindSz, 1)
		assign(b.legacyWeakBind, &weakBindOff, &weakBindSz, 1)
		assign(b.legacyLazyBind, &lazyBindOff, &lazyBindSz, 1)
		assign(b.legacyExport, &exportOff, &exportSz, 8)
	} else {
		assign(b.chainedFixupsData, &chainedFixupsOff, &chainedFixupsSz, 8)
		assign(b.exportsTrie, &exportTrieOff, &exportTrieSz, 8)
	}

	assign(b.functionStarts, &funcStartsOff, &funcStartsSz, 8)

	// data_in_code blob.
	var dicBlob []byte
	for _, e := range b.dataInCode {
		dicBlob = append(dicBlob, make([]byte, sizeofDataInCodeEntry)...)
		emitDataInCodeEntry(dicBlob, len(dicBlob)-sizeofDataInCodeEntry, e)
	}
	assign(dicBlob, &dataInCodeOff, &dataInCodeSz, 4)

	// Symbol table.
	symSzU := uint64(len(allSyms)) * sizeofNlist64
	cur = alignUp(cur, 8)
	symOff = uint32(cur)
	symSz = uint32(symSzU)
	cur += symSzU

	// Indirect symbol table.
	if len(indirectSyms) > 0 {
		cur = alignUp(cur, 4)
		indirSymOff = uint32(cur)
		indirSymSz = uint32(len(indirectSyms) * 4)
		cur += uint64(indirSymSz)
	}

	// String table.
	cur = alignUp(cur, 8)
	strOff = uint32(cur)
	cur += uint64(len(strTable))

	// Code signature (if reserved).
	if b.codeSignatureSize > 0 {
		cur = alignUp(cur, 16)
		codeSignOff = uint32(cur)
		cur += uint64(b.codeSignatureSize)
	}

	linkeditSize := cur - linkeditFileStart

	// ── Phase 7: resolve entry point ─────────────────────────────────────────
	entryFileOff := uint64(0)
	if b.fileType == FileTypeExecute && b.entry != "" {
		found := false
		for _, sym := range b.symbols {
			if sym.Name != b.entry {
				continue
			}
			nsect := sectIndex(sym.SegmentName, sym.SectionName)
			if nsect > 0 {
				sectVMAddr := flatSects[nsect-1].vmAddr
				textStart := uint64(0)
				if len(b.segments) > 0 && segExtents[0].set {
					textStart = segExtents[0].vmStart
				}
				entryFileOff = (sectVMAddr + sym.Value) - textStart
				found = true
			}
			if !found {
				entryFileOff = sym.Value
				found = true
			}
			break
		}
		if !found {
			return nil, fmt.Errorf("macho: entry symbol %q not found", b.entry)
		}
	}

	// ── Phase 8: count and size load commands ─────────────────────────────────
	ncmds, lcBytes := b.countLoadCommands(
		linkeditFileStart, linkeditSize, linkeditVMAddr,
		rebaseOff, rebaseSz, bindOff, bindSz,
		weakBindOff, weakBindSz, lazyBindOff, lazyBindSz,
		exportOff, exportSz,
		chainedFixupsOff, chainedFixupsSz,
		exportTrieOff, exportTrieSz,
		funcStartsOff, funcStartsSz,
		dataInCodeOff, dataInCodeSz,
		symOff, uint32(len(allSyms)),
		indirSymOff, uint32(len(indirectSyms)),
		strOff, uint32(len(strTable)),
		uint32(len(localSyms)), uint32(len(extSyms)),
		entryFileOff,
	)

	// ── Phase 9: allocate and write everything ────────────────────────────────
	totalSize := cur
	out := make([]byte, totalSize)

	// ── Header ───────────────────────────────────────────────────────────────
	flags := b.computeFlags()
	emitMachHeader64(out, b.arch, uint32(b.fileType), flags, uint32(ncmds), uint32(lcBytes))

	// ── Load commands ─────────────────────────────────────────────────────────
	lcOff := sizeofMachHeader64

	// __PAGEZERO (executables and bundles).
	if b.fileType == FileTypeExecute || b.fileType == FileTypeBundle || b.fileType == FileTypeKextBundle {
		lcOff = emitSegmentCommand64(out, lcOff,
			"__PAGEZERO",
			0, baseVA, 0, 0,
			ProtNone, ProtNone, 0, 0)
	}

	// User segments.
	for si, seg := range b.segments {
		e := segExtents[si]
		vmStart, vmEnd := e.vmStart, e.vmEnd
		fileStart, fileEnd := e.fileStart, e.fileEnd
		if !e.set {
			vmStart = vmAddr
			fileStart = fileOff
		}
		vmSize := alignUp(vmEnd-vmStart, PageSize) // ← was pageSize
		fileSize := fileEnd - fileStart

		maxProt := seg.MaxProt
		if maxProt == ProtNone && seg.InitProt != ProtNone {
			maxProt = seg.InitProt
		}
		nsects := uint32(len(seg.Sections))
		lcOff = emitSegmentCommand64(out, lcOff,
			seg.Name,
			vmStart, vmSize,
			fileStart, fileSize,
			maxProt, seg.InitProt,
			nsects, seg.Flags)

		for ki, sect := range seg.Sections {
			var l sectionLayout
			for _, ll := range layouts {
				if ll.segIdx == si && ll.sectIdx == ki {
					l = ll
					break
				}
			}
			relocOff := uint32(0)
			nReloc := uint32(0)
			for _, sr := range allRelocs {
				fi := -1
				for i, fs := range flatSects {
					if fs.segName == seg.Name && fs.sectName == sect.Name {
						fi = i
						break
					}
				}
				if sr.flatIdx == fi {
					nReloc = uint32(len(sr.relocs))
					_ = relocOff
					break
				}
			}
			alignLog := log2ceil(sect.Align)
			isZerofill := (sect.Flags & 0xff) == S_ZEROFILL ||
				(sect.Flags&0xff) == S_GB_ZEROFILL ||
				(sect.Flags&0xff) == S_THREAD_LOCAL_ZEROFILL
			sectFileOff := uint32(l.fileOff)
			if isZerofill {
				sectFileOff = 0
			}
			lcOff = emitSection64(out, lcOff,
				sect.Name, seg.Name,
				l.vmAddr, l.size,
				sectFileOff, alignLog,
				relocOff, nReloc, sect.Flags,
				sect.Reserved1, sect.Reserved2)
		}
	}

	// __LINKEDIT.
	lcOff = emitSegmentCommand64(out, lcOff, "__LINKEDIT",
		linkeditVMAddr, alignUp(linkeditSize, PageSize), // ← was pageSize
		linkeditFileStart, linkeditSize,
		ProtRead, ProtRead, 0, 0)

	// LC_DYLD_INFO_ONLY or LC_DYLD_CHAINED_FIXUPS / LC_DYLD_EXPORTS_TRIE.
	if b.fileType != FileTypeObject {
		if b.dyldMode == DyldModeLegacy {
			if len(b.legacyRebase) > 0 || len(b.legacyBind) > 0 || len(b.legacyExport) > 0 {
				lcOff = emitDyldInfoCommand(out, lcOff,
					rebaseOff, rebaseSz,
					bindOff, bindSz,
					weakBindOff, weakBindSz,
					lazyBindOff, lazyBindSz,
					exportOff, exportSz)
			}
		} else {
			if len(b.chainedFixupsData) > 0 {
				lcOff = emitLinkeditDataCommand(out, lcOff, lcDyldChainedFixups, chainedFixupsOff, chainedFixupsSz)
			}
			if len(b.exportsTrie) > 0 {
				lcOff = emitLinkeditDataCommand(out, lcOff, lcDyldExportsTrie, exportTrieOff, exportTrieSz)
			}
		}
	}

	// LC_SYMTAB.
	lcOff = emitSymtabCommand(out, lcOff, symOff, uint32(len(allSyms)), strOff, uint32(len(strTable)))

	// LC_DYSYMTAB.
	ilocal := uint32(0)
	nlocal := uint32(len(localSyms))
	iextdef := nlocal
	nextdef := uint32(len(extSyms))
	iundef := iextdef + nextdef
	nundef := uint32(0)
	lcOff = emitDysymtabCommand(out, lcOff,
		ilocal, nlocal, iextdef, nextdef, iundef, nundef,
		indirSymOff, uint32(len(indirectSyms)))

	// LC_LOAD_DYLINKER (not for object files).
	if b.fileType != FileTypeObject {
		lcOff = emitLoadDylinkerCommand(out, lcOff, dylinkerPath)
	}

	// LC_UUID.
	if b.uuid != nil {
		lcOff = emitUUIDCommand(out, lcOff, *b.uuid)
	}

	// LC_BUILD_VERSION.
	if b.buildVersion != nil {
		lcOff = emitBuildVersionCommand(out, lcOff, *b.buildVersion)
	}

	// LC_SOURCE_VERSION.
	if b.sourceVersion != 0 {
		lcOff = emitSourceVersionCommand(out, lcOff, b.sourceVersion)
	}

	// LC_ID_DYLIB.
	if b.dylibID != nil && (b.fileType == FileTypeDylib || b.fileType == FileTypeBundle) {
		lcOff = emitIDDylibCommand(out, lcOff, *b.dylibID)
	}

	// LC_LOAD_DYLIB / LC_LOAD_WEAK_DYLIB / LC_REEXPORT_DYLIB / etc.
	for _, ref := range b.dylibs {
		cmd := dylibCmdFor(ref.Kind)
		lcOff = emitDylibCommand(out, lcOff, cmd, ref)
	}

	// LC_RPATH.
	for _, rp := range b.rpaths {
		lcOff = emitRpathCommand(out, lcOff, rp)
	}

	// LC_FUNCTION_STARTS.
	if funcStartsSz > 0 {
		lcOff = emitLinkeditDataCommand(out, lcOff, lcFunctionStarts, funcStartsOff, funcStartsSz)
	}

	// LC_DATA_IN_CODE.
	if dataInCodeSz > 0 {
		lcOff = emitLinkeditDataCommand(out, lcOff, lcDataInCode, dataInCodeOff, dataInCodeSz)
	}

	// LC_LINKER_OPTION.
	for _, opts := range b.linkerOptions {
		lcOff = emitLinkerOptionCommand(out, lcOff, opts)
	}

	// LC_MAIN (executables only).
	if b.fileType == FileTypeExecute {
		lcOff = emitMainCommand(out, lcOff, entryFileOff)
	}

	// LC_CODE_SIGNATURE (reserved space marker).
	if b.codeSignatureSize > 0 {
		lcOff = emitLinkeditDataCommand(out, lcOff, lcCodeSignature, codeSignOff, b.codeSignatureSize)
	}

	_ = lcOff

	// ── Section data ─────────────────────────────────────────────────────────
	for si, seg := range b.segments {
		for ki, sect := range seg.Sections {
			isZerofill := (sect.Flags & 0xff) == S_ZEROFILL ||
				(sect.Flags&0xff) == S_GB_ZEROFILL ||
				(sect.Flags&0xff) == S_THREAD_LOCAL_ZEROFILL
			if isZerofill {
				continue
			}
			var l sectionLayout
			for _, ll := range layouts {
				if ll.segIdx == si && ll.sectIdx == ki {
					l = ll
					break
				}
			}
			copy(out[l.fileOff:], sect.Data)
		}
	}

	// ── Relocation entries ───────────────────────────────────────────────────
	for _, sr := range allRelocs {
		for _, r := range sr.relocs {
			_ = symNameIdx(r.Symbol)
		}
	}

	// ── __LINKEDIT: dyld blobs ───────────────────────────────────────────────
	if b.dyldMode == DyldModeLegacy {
		if rebaseSz > 0 {
			copy(out[rebaseOff:], b.legacyRebase)
		}
		if bindSz > 0 {
			copy(out[bindOff:], b.legacyBind)
		}
		if weakBindSz > 0 {
			copy(out[weakBindOff:], b.legacyWeakBind)
		}
		if lazyBindSz > 0 {
			copy(out[lazyBindOff:], b.legacyLazyBind)
		}
		if exportSz > 0 {
			copy(out[exportOff:], b.legacyExport)
		}
	} else {
		if chainedFixupsSz > 0 {
			copy(out[chainedFixupsOff:], b.chainedFixupsData)
		}
		if exportTrieSz > 0 {
			copy(out[exportTrieOff:], b.exportsTrie)
		}
	}

	// ── Function starts ──────────────────────────────────────────────────────
	if funcStartsSz > 0 {
		copy(out[funcStartsOff:], b.functionStarts)
	}

	// ── Data-in-code ─────────────────────────────────────────────────────────
	if dataInCodeSz > 0 {
		copy(out[dataInCodeOff:], dicBlob)
	}

	// ── Symbol table (nlist_64) ───────────────────────────────────────────────
	soff := int(symOff)
	for _, sym := range allSyms {
		soff = emitNlist64(out, soff, sym.strx, sym.ntype, sym.nsect, sym.ndesc, sym.value)
	}

	// ── Indirect symbol table ────────────────────────────────────────────────
	if len(indirectSyms) > 0 {
		ioff := int(indirSymOff)
		for _, idx := range indirectSyms {
			putU32(out, ioff, idx)
			ioff += 4
		}
	}

	// ── String table ─────────────────────────────────────────────────────────
	copy(out[strOff:], strTable)

	return out, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func (b *Builder) validate() error {
	if b.arch != ArchAMD64 && b.arch != ArchARM64 {
		return errors.New("macho: unsupported architecture")
	}
	if b.fileType == FileTypeExecute && b.entry == "" {
		return errors.New("macho: SetEntry must be called for MH_EXECUTE")
	}
	return nil
}

func (b *Builder) computeFlags() MHFlags {
	f := b.flags
	if b.fileType != FileTypeObject {
		f |= MHDyldLink | MHTwoLevel
	}
	if b.fileType == FileTypeExecute {
		f |= MHPie
	}
	hasUndef := false
	for _, sym := range b.symbols {
		if sym.SegmentName == "" && sym.SectionName == "" && sym.Global {
			hasUndef = true
			break
		}
	}
	if !hasUndef {
		f |= MHNoUndefs
	}
	return f
}

// computeLoadCommandSize returns the total byte count of all load commands.
func (b *Builder) computeLoadCommandSize() uint64 {
	sz := uint64(0)

	// __PAGEZERO.
	if b.fileType == FileTypeExecute || b.fileType == FileTypeBundle || b.fileType == FileTypeKextBundle {
		sz += sizeofSegmentCommand64
	}
	// User segments.
	for _, seg := range b.segments {
		sz += uint64(sizeofSegmentCommand64) + uint64(len(seg.Sections))*uint64(sizeofSection64)
	}
	// __LINKEDIT.
	sz += sizeofSegmentCommand64

	// Dynamic linking.
	if b.fileType != FileTypeObject {
		if b.dyldMode == DyldModeLegacy {
			if len(b.legacyRebase) > 0 || len(b.legacyBind) > 0 || len(b.legacyExport) > 0 {
				sz += sizeofDyldInfoCommand
			}
		} else {
			if len(b.chainedFixupsData) > 0 {
				sz += sizeofLinkeditDataCommand
			}
			if len(b.exportsTrie) > 0 {
				sz += sizeofLinkeditDataCommand
			}
		}
	}

	sz += sizeofSymtabCommand
	sz += sizeofDysymtabCommand

	// LC_LOAD_DYLINKER.
	if b.fileType != FileTypeObject {
		sz += alignUp(uint64(sizeofLoadDylinkerCommand)+uint64(len(dylinkerPath))+1, 8)
	}
	// LC_UUID.
	if b.uuid != nil {
		sz += sizeofUUIDCommand
	}
	// LC_BUILD_VERSION.
	if b.buildVersion != nil {
		sz += uint64(sizeofBuildVersionCommand + len(b.buildVersion.Tools)*sizeofBuildToolVersion)
	}
	// LC_SOURCE_VERSION.
	if b.sourceVersion != 0 {
		sz += sizeofSourceVersionCommand
	}
	// LC_ID_DYLIB.
	if b.dylibID != nil && (b.fileType == FileTypeDylib || b.fileType == FileTypeBundle) {
		sz += alignUp(uint64(sizeofDylibCommand)+uint64(len(b.dylibID.Path))+1, 8)
	}
	// LC_LOAD_DYLIB / variants.
	for _, ref := range b.dylibs {
		sz += alignUp(uint64(sizeofDylibCommand)+uint64(len(ref.Path))+1, 8)
	}
	// LC_RPATH.
	for _, rp := range b.rpaths {
		sz += alignUp(uint64(sizeofRpathCommandBase)+uint64(len(rp))+1, 8)
	}
	// LC_FUNCTION_STARTS.
	if len(b.functionStarts) > 0 {
		sz += sizeofLinkeditDataCommand
	}
	// LC_DATA_IN_CODE.
	if len(b.dataInCode) > 0 {
		sz += sizeofLinkeditDataCommand
	}
	// LC_LINKER_OPTION.
	for _, opts := range b.linkerOptions {
		raw := 0
		for _, o := range opts {
			raw += len(o) + 1
		}
		sz += alignUp(uint64(sizeofLinkerOptionBase)+uint64(raw), 8)
	}
	// LC_MAIN.
	if b.fileType == FileTypeExecute {
		sz += sizeofEntryPointCommand
	}
	// LC_CODE_SIGNATURE.
	if b.codeSignatureSize > 0 {
		sz += sizeofLinkeditDataCommand
	}
	return sz
}

// countLoadCommands returns (ncmds, sizeofcmds) by exactly mirroring
// computeLoadCommandSize's logic.
func (b *Builder) countLoadCommands(
	_ uint64, _ uint64, _ uint64,
	_, _ uint32, _, _ uint32,
	_, _ uint32, _, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_, _ uint32,
	_ uint64,
) (ncmds int, sizeofcmds int) {
	raw := b.computeLoadCommandSize()
	count := 0
	if b.fileType == FileTypeExecute || b.fileType == FileTypeBundle || b.fileType == FileTypeKextBundle {
		count++
	}
	count += len(b.segments)
	count++ // __LINKEDIT
	if b.fileType != FileTypeObject {
		if b.dyldMode == DyldModeLegacy {
			if len(b.legacyRebase) > 0 || len(b.legacyBind) > 0 || len(b.legacyExport) > 0 {
				count++
			}
		} else {
			if len(b.chainedFixupsData) > 0 {
				count++
			}
			if len(b.exportsTrie) > 0 {
				count++
			}
		}
		count++ // LC_LOAD_DYLINKER
	}
	count += 2 // LC_SYMTAB + LC_DYSYMTAB
	if b.uuid != nil {
		count++
	}
	if b.buildVersion != nil {
		count++
	}
	if b.sourceVersion != 0 {
		count++
	}
	if b.dylibID != nil && (b.fileType == FileTypeDylib || b.fileType == FileTypeBundle) {
		count++
	}
	count += len(b.dylibs)
	count += len(b.rpaths)
	if len(b.functionStarts) > 0 {
		count++
	}
	if len(b.dataInCode) > 0 {
		count++
	}
	count += len(b.linkerOptions)
	if b.fileType == FileTypeExecute {
		count++
	}
	if b.codeSignatureSize > 0 {
		count++
	}
	return count, int(raw)
}