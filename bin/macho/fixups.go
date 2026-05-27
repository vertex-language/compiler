package macho

import "encoding/binary"

// ──────────────────────────────────────────────────────────────────────────────
// Chained fixups — LC_DYLD_CHAINED_FIXUPS  (macOS 12+ / iOS 14+)
// ──────────────────────────────────────────────────────────────────────────────
//
// Reference:  <mach-o/fixup-chains.h>

// ChainedPtrFormat selects the in-page pointer encoding written into data pages.
type ChainedPtrFormat uint16

const (
	// ChainedPtrArm64e — arm64e authenticated/plain 64-bit chained pointer.
	ChainedPtrArm64e ChainedPtrFormat = 1
	// ChainedPtr64 — plain 64-bit chained pointer (arm64 / x86-64 non-pie).
	ChainedPtr64 ChainedPtrFormat = 2
	// ChainedPtr32 — 32-bit chained pointer.
	ChainedPtr32 ChainedPtrFormat = 3
	// ChainedPtr32Cache — 32-bit pointer relative to dyld shared cache.
	ChainedPtr32Cache ChainedPtrFormat = 4
	// ChainedPtr32FirmwareIF — 32-bit firmware intermediate format.
	ChainedPtr32FirmwareIF ChainedPtrFormat = 7
	// ChainedPtr64Offset — 64-bit offset-based chained pointer (arm64 modern default).
	ChainedPtr64Offset ChainedPtrFormat = 6
	// ChainedPtrX8664KernelCache — kernel cache pointer format.
	ChainedPtrX8664KernelCache ChainedPtrFormat = 8
	// ChainedPtrArm64eKernel — arm64e kernel format.
	ChainedPtrArm64eKernel ChainedPtrFormat = 9
	// ChainedPtr64KernelCache — 64-bit kernel cache.
	ChainedPtr64KernelCache ChainedPtrFormat = 10
	// ChainedPtrArm64eUserland — arm64e userland with bind/rebase metadata.
	ChainedPtrArm64eUserland ChainedPtrFormat = 12
	// ChainedPtrArm64eUserland24 — arm64e userland with 24-bit bind ordinal.
	ChainedPtrArm64eUserland24 ChainedPtrFormat = 13
)

const (
	chainedFixupsVersion    = 0
	chainedImportFormat     = 1 // DYLD_CHAINED_IMPORT
	chainedImportFormatAddend = 2 // DYLD_CHAINED_IMPORT_ADDEND
	chainedImportFormatAddend64 = 3 // DYLD_CHAINED_IMPORT_ADDEND64
	chainedStartsNone       = 0xffff // DYLD_CHAINED_PTR_START_NONE
)

// BindTarget describes one external symbol referenced via chained fixups.
type BindTarget struct {
	// LibOrdinal is 1-based into the LC_LOAD_DYLIB list, or a BindSpecial* value.
	LibOrdinal int
	Name       string
	Addend     int64
	WeakImport bool
}

// ChainedRebase is a data pointer that requires an ASLR rebase fixup.
type ChainedRebase struct {
	// SegIndex is zero-based, matching the order segments were added (excluding __PAGEZERO).
	SegIndex  int
	SegOffset uint64 // byte offset from segment start to the pointer word
}

// ChainedBind is a data pointer that must be resolved to an external symbol.
type ChainedBind struct {
	SegIndex  int
	SegOffset uint64
	TargetIdx int // index into the BindTargets slice added via AddBindTarget
	Addend    int64
}

// ChainedFixupsBuilder constructs the LC_DYLD_CHAINED_FIXUPS blob.
// The companion LC_DYLD_EXPORTS_TRIE is built via BuildExportTrie.
type ChainedFixupsBuilder struct {
	format   ChainedPtrFormat
	pageSize uint32        // 0x1000 or 0x4000
	targets  []BindTarget
	rebases  []ChainedRebase
	binds    []ChainedBind
	// segCount is set by Build() from the number of segments in the image.
}

// NewChainedFixupsBuilder returns a builder using the given pointer format and
// page size.
//
// For macOS ARM64 userland use (ChainedPtr64Offset, 0x4000).
// For macOS AMD64 userland use (ChainedPtr64Offset, 0x1000).
func NewChainedFixupsBuilder(format ChainedPtrFormat, pageSize uint32) *ChainedFixupsBuilder {
	return &ChainedFixupsBuilder{format: format, pageSize: pageSize}
}

// AddBindTarget registers an external symbol and returns its ordinal index
// (to be stored in ChainedBind.TargetIdx).
func (c *ChainedFixupsBuilder) AddBindTarget(t BindTarget) int {
	idx := len(c.targets)
	c.targets = append(c.targets, t)
	return idx
}

// AddRebase records a data pointer that needs a rebase fixup.
func (c *ChainedFixupsBuilder) AddRebase(r ChainedRebase) {
	c.rebases = append(c.rebases, r)
}

// AddBind records a data pointer that binds to an external symbol.
func (c *ChainedFixupsBuilder) AddBind(b ChainedBind) {
	c.binds = append(c.binds, b)
}

// segFixup groups a rebase or bind by segment/page.
type segFixup struct {
	segIndex  int
	segOffset uint64
	isBind    bool
	bindIdx   int   // valid when isBind
	addend    int64 // valid when isBind
}

// Build serialises the dyld_chained_fixups blob.
// segFileOffsets must contain the file offset of each segment (in the same
// order as segments were added to the Builder, excluding __PAGEZERO).
// segVMSizes must contain the VM size of each segment.
func (c *ChainedFixupsBuilder) Build(segFileOffsets []uint64, segVMSizes []uint64) []byte {
	segCount := len(segFileOffsets)

	// Collect all fixups per segment, sorted by segOffset.
	segFixups := make([][]segFixup, segCount)
	for _, r := range c.rebases {
		if r.SegIndex < segCount {
			segFixups[r.SegIndex] = append(segFixups[r.SegIndex], segFixup{
				segIndex:  r.SegIndex,
				segOffset: r.SegOffset,
			})
		}
	}
	for _, b := range c.binds {
		if b.SegIndex < segCount {
			segFixups[b.SegIndex] = append(segFixups[b.SegIndex], segFixup{
				segIndex:  b.SegIndex,
				segOffset: b.SegOffset,
				isBind:    true,
				bindIdx:   b.TargetIdx,
				addend:    b.Addend,
			})
		}
	}
	// Sort each segment's fixups by offset.
	for si := range segFixups {
		fxs := segFixups[si]
		for i := 1; i < len(fxs); i++ {
			for j := i; j > 0 && fxs[j].segOffset < fxs[j-1].segOffset; j-- {
				fxs[j], fxs[j-1] = fxs[j-1], fxs[j]
			}
		}
		segFixups[si] = fxs
	}

	// Build per-segment starts structures.
	type segStartsInfo struct {
		data      []byte // serialised dyld_chained_starts_in_segment
		pageCount uint16
	}
	segsInfo := make([]segStartsInfo, segCount)
	for si := 0; si < segCount; si++ {
		vmSize := uint64(0)
		if si < len(segVMSizes) {
			vmSize = segVMSizes[si]
		}
		nPages := uint16((vmSize + uint64(c.pageSize) - 1) / uint64(c.pageSize))
		pageStarts := make([]uint16, nPages)
		for i := range pageStarts {
			pageStarts[i] = chainedStartsNone
		}
		// For each fixup in this segment, record page start.
		fxs := segFixups[si]
		for _, fx := range fxs {
			page := uint16(fx.segOffset / uint64(c.pageSize))
			offsetInPage := uint16(fx.segOffset % uint64(c.pageSize))
			if page < nPages && pageStarts[page] == chainedStartsNone {
				pageStarts[page] = offsetInPage
			}
		}
		// Now chain the fixup words into the segment data (caller manages that;
		// we just record the page_start array here).
		//
		// dyld_chained_starts_in_segment:
		//   uint32 size
		//   uint16 page_size
		//   uint16 pointer_format
		//   uint64 segment_offset
		//   uint32 max_valid_pointer (0 for 64-bit)
		//   uint16 page_count
		//   uint16 page_start[page_count]
		hdrSize := 22 + int(nPages)*2
		seg := make([]byte, hdrSize)
		binary.LittleEndian.PutUint32(seg[0:], uint32(hdrSize))
		binary.LittleEndian.PutUint16(seg[4:], uint16(c.pageSize))
		binary.LittleEndian.PutUint16(seg[6:], uint16(c.format))
		fileOff := uint64(0)
		if si < len(segFileOffsets) {
			fileOff = segFileOffsets[si]
		}
		binary.LittleEndian.PutUint64(seg[8:], fileOff)
		binary.LittleEndian.PutUint32(seg[16:], 0) // max_valid_pointer
		binary.LittleEndian.PutUint16(seg[20:], nPages)
		for i, ps := range pageStarts {
			binary.LittleEndian.PutUint16(seg[22+i*2:], ps)
		}
		segsInfo[si] = segStartsInfo{data: seg, pageCount: nPages}
	}

	// Build the imports table and symbol strings.
	var symStrings []byte
	symStrings = append(symStrings, 0) // NUL at index 0 is conventional
	type importEntry struct {
		libOrdinal uint8
		weakImport bool
		nameOffset uint32
		addend     int64
	}
	imports := make([]importEntry, len(c.targets))
	for i, t := range c.targets {
		nameOff := uint32(len(symStrings))
		symStrings = append(symStrings, []byte(t.Name)...)
		symStrings = append(symStrings, 0)
		ord := uint8(0)
		if t.LibOrdinal > 0 {
			ord = uint8(t.LibOrdinal)
		}
		imports[i] = importEntry{
			libOrdinal: ord,
			weakImport: t.WeakImport,
			nameOffset: nameOff,
			addend:     t.Addend,
		}
	}
	for len(symStrings)%8 != 0 {
		symStrings = append(symStrings, 0)
	}

	// Layout:
	//   dyld_chained_fixups_header  (28 bytes)
	//   dyld_chained_starts_in_image  (4 + 4*segCount bytes)
	//   dyld_chained_starts_in_segment[] for each segment
	//   imports[]  (4 bytes each for DYLD_CHAINED_IMPORT)
	//   symbol strings
	headerSize := 28
	startsInImageSize := 4 + 4*segCount
	var segStartsOffset []int // offset of each segment starts within the blob
	startsDataSize := 0
	for si := 0; si < segCount; si++ {
		segStartsOffset = append(segStartsOffset, startsDataSize)
		startsDataSize += len(segsInfo[si].data)
	}
	importsOffset := headerSize + startsInImageSize + startsDataSize
	importsSize := len(imports) * 4
	symbolsOffset := importsOffset + importsSize
	totalSize := symbolsOffset + len(symStrings)

	out := make([]byte, totalSize)

	// Header.
	binary.LittleEndian.PutUint32(out[0:], chainedFixupsVersion)
	binary.LittleEndian.PutUint32(out[4:], uint32(headerSize)) // starts_offset
	binary.LittleEndian.PutUint32(out[8:], uint32(importsOffset))
	binary.LittleEndian.PutUint32(out[12:], uint32(symbolsOffset))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(imports)))
	binary.LittleEndian.PutUint32(out[20:], chainedImportFormat)
	binary.LittleEndian.PutUint32(out[24:], 0) // symbols_format (0 = uncompressed)

	// dyld_chained_starts_in_image.
	imgBase := headerSize
	binary.LittleEndian.PutUint32(out[imgBase:], uint32(segCount))
	for si := 0; si < segCount; si++ {
		// offset relative to the dyld_chained_starts_in_image start
		relOff := uint32(4 + 4*segCount + segStartsOffset[si])
		binary.LittleEndian.PutUint32(out[imgBase+4+si*4:], relOff)
	}

	// Segment starts data.
	segDataBase := headerSize + startsInImageSize
	for si := 0; si < segCount; si++ {
		copy(out[segDataBase+segStartsOffset[si]:], segsInfo[si].data)
	}

	// Imports (DYLD_CHAINED_IMPORT: uint32 packed as lib_ordinal:8 weak:1 name_offset:23).
	for i, imp := range imports {
		packed := uint32(imp.libOrdinal)
		if imp.weakImport {
			packed |= 1 << 8
		}
		packed |= imp.nameOffset << 9
		binary.LittleEndian.PutUint32(out[importsOffset+i*4:], packed)
	}

	// Symbol strings.
	copy(out[symbolsOffset:], symStrings)

	return out
}