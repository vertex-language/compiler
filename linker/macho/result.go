package macho

import (
	binmacho "github.com/vertex-language/compiler/bin/macho"
)

// ──────────────────────────────────────────────────────────────────────────────
// LinkResult
// ──────────────────────────────────────────────────────────────────────────────

// LinkResult holds everything the linker produced and can construct a
// bin/macho.Builder ready for emission.
type LinkResult struct {
	Arch        uint32
	OutputType  OutputType
	Entry       string
	InstallName string
	Platform    binmacho.BuildVersion

	Layout   *Layout
	Symtab   *SymbolTable
	Stubs    *StubTable
	Dylibs   []*DylibFile // in ordinal order (1-based)
	Rpaths   []string
	DyldMode binmacho.DyldMode
}

// Builder constructs and returns a fully-configured bin/macho.Builder.
func (r *LinkResult) Builder() *binmacho.Builder {
	arch := binmacho.ArchARM64
	if r.Arch == ArchAMD64 {
		arch = binmacho.ArchAMD64
	}

	b := binmacho.NewBuilder(arch)
	b.SetDyldMode(r.DyldMode)

	// File type.
	switch r.OutputType {
	case OutputDylib:
		b.SetFileType(binmacho.FileTypeDylib)
		if r.InstallName != "" {
			b.SetDylibID(binmacho.DylibRef{Path: r.InstallName, Kind: binmacho.DylibLoad})
		}
	case OutputBundle:
		b.SetFileType(binmacho.FileTypeBundle)
	default:
		b.SetFileType(binmacho.FileTypeExecute)
	}

	// Build version.
	b.SetBuildVersion(r.Platform)

	// Dylib dependencies.
	for _, d := range r.Dylibs {
		b.AddDylib(binmacho.DylibRef{
			Path: d.Soname,
			Kind: binmacho.DylibLoad,
		})
	}

	// RPATHs.
	for _, rp := range r.Rpaths {
		b.AddRpath(rp)
	}

	// Segments and sections.
	for _, outSeg := range r.Layout.Segments {
		seg := binmacho.Segment{
			Name: outSeg.Name,
		}
		switch outSeg.Name {
		case "__TEXT":
			seg.InitProt = binmacho.ProtRead | binmacho.ProtExec
			seg.MaxProt = binmacho.ProtRead | binmacho.ProtExec
		case "__DATA_CONST":
			seg.InitProt = binmacho.ProtRead | binmacho.ProtWrite
			seg.MaxProt = binmacho.ProtRead | binmacho.ProtWrite
			seg.Flags = binmacho.SegReadOnly
		case "__DATA":
			seg.InitProt = binmacho.ProtRead | binmacho.ProtWrite
			seg.MaxProt = binmacho.ProtRead | binmacho.ProtWrite
		default:
			seg.InitProt = binmacho.ProtRead
			seg.MaxProt = binmacho.ProtRead
		}

		for _, ms := range outSeg.Sections {
			sect := binmacho.Section{
				Name:  ms.SectName,
				Flags: ms.Flags(),
				Align: ms.Align,
			}
			if ms.IsZerofill() {
				sect.Size = ms.Size
			} else {
				sect.Data = ms.Data
			}
			seg.Sections = append(seg.Sections, sect)
		}
		b.AddSegment(seg)
	}

	// Symbols.
	for _, rs := range r.Symtab.All() {
		if rs.Kind != kindDefined && rs.Kind != kindUndef {
			continue
		}
		sym := binmacho.Symbol{
			Name:   rs.Name,
			Global: rs.IsGlobal,
			Weak:   rs.IsWeak,
			Value:  rs.Value,
		}
		if rs.Kind == kindDefined && !rs.IsAbs {
			sym.SegmentName = rs.SegmentName
			sym.SectionName = rs.SectionName
		}
		b.AddSymbol(sym)
	}

	// Entry point.
	if r.OutputType == OutputExec && r.Entry != "" {
		b.SetEntry(r.Entry)
	}

	// Dynamic linking tables.
	if r.DyldMode == binmacho.DyldModeChained {
		cfb := buildChainedFixups(r)
		if cfb != nil {
			b.SetChainedFixups(cfb)
		}
		exports := buildExportTrie(r)
		if exports != nil {
			b.SetExportsTrie(exports)
		}
	} else {
		rebase, bind, weakBind, lazyBind, exportTrie := buildLegacyDyldInfo(r)
		b.SetDyldInfo(rebase, bind, weakBind, lazyBind, exportTrie)
	}

	return b
}

// buildChainedFixups builds the LC_DYLD_CHAINED_FIXUPS blob from stub/GOT
// bind records.
func buildChainedFixups(r *LinkResult) []byte {
	if r.Stubs == nil || len(r.Stubs.Entries()) == 0 {
		return nil
	}

	// binmacho.PageSize is a typed constant, not a function.
	pageSize := uint32(binmacho.PageSize)
	format := binmacho.ChainedPtr64Offset
	if r.Arch == ArchARM64 {
		format = binmacho.ChainedPtr64Offset
	}

	cfb := binmacho.NewChainedFixupsBuilder(format, pageSize)

	// Register bind targets.
	for _, e := range r.Stubs.Entries() {
		idx := cfb.AddBindTarget(binmacho.BindTarget{
			LibOrdinal: e.LibOrdinal,
			Name:       e.SymName,
		})

		gotMs := r.Layout.SectionByKey("__DATA_CONST", "__got")
		if gotMs == nil {
			continue
		}
		segIdx := segmentIndex(r.Layout, "__DATA_CONST")
		cfb.AddBind(binmacho.ChainedBind{
			SegIndex:  segIdx,
			SegOffset: gotMs.FileOffset - r.Layout.segFileOffset("__DATA_CONST") + e.GotOffset,
			TargetIdx: idx,
		})
	}

	segOffsets, segSizes := collectSegExtents(r.Layout)
	return cfb.Build(segOffsets, segSizes)
}

func buildExportTrie(r *LinkResult) []byte {
	var exports []binmacho.ExportEntry
	for _, rs := range r.Symtab.All() {
		if !rs.IsGlobal || rs.Kind != kindDefined {
			continue
		}
		exports = append(exports, binmacho.ExportEntry{
			Name:    rs.Name,
			Address: rs.VAddr,
			Flags:   binmacho.ExportKindRegular,
		})
	}
	if len(exports) == 0 {
		return nil
	}
	return binmacho.BuildExportTrie(exports)
}

func buildLegacyDyldInfo(r *LinkResult) (rebase, bind, weakBind, lazyBind, exportTrie []byte) {
	dib := binmacho.NewDyldInfoBuilder()

	if r.Stubs != nil {
		gotMs := r.Layout.SectionByKey("__DATA_CONST", "__got")
		if gotMs != nil {
			segIdx := segmentIndex(r.Layout, "__DATA_CONST")
			for _, e := range r.Stubs.Entries() {
				dib.AddBind(binmacho.BindEntry{
					SegIndex:   segIdx,
					SegOffset:  gotMs.VAddr - r.Layout.segVAddr("__DATA_CONST") + e.GotOffset,
					LibOrdinal: e.LibOrdinal,
					Name:       e.SymName,
					Type:       binmacho.BindTypePointer,
				})
			}
		}
	}

	for _, rs := range r.Symtab.All() {
		if !rs.IsGlobal || rs.Kind != kindDefined {
			continue
		}
		dib.AddExport(binmacho.ExportEntry{
			Name:    rs.Name,
			Address: rs.VAddr,
			Flags:   binmacho.ExportKindRegular,
		})
	}

	rebase, bind, weakBind, lazyBind, exportTrie = dib.Build()
	return
}

func segmentIndex(layout *Layout, name string) int {
	for i, seg := range layout.Segments {
		if seg.Name == name {
			return i
		}
	}
	return 0
}

func collectSegExtents(layout *Layout) (offsets []uint64, sizes []uint64) {
	for _, seg := range layout.Segments {
		offsets = append(offsets, seg.FileOff)
		sizes = append(sizes, seg.VMSize)
	}
	return
}

// segFileOffset returns the file offset of the named segment.
func (l *Layout) segFileOffset(name string) uint64 {
	for _, seg := range l.Segments {
		if seg.Name == name {
			return seg.FileOff
		}
	}
	return 0
}

// segVAddr returns the VM address of the named segment.
func (l *Layout) segVAddr(name string) uint64 {
	for _, seg := range l.Segments {
		if seg.Name == name {
			return seg.VMAddr
		}
	}
	return 0
}