package pe

import (
	"encoding/binary"
	"fmt"
)

// buildImage turns Builder state into a fully-populated PEImage.
func (b *Builder) buildImage() (*PEImage, error) {
	// Step 1: Resolve configuration defaults.
	imageBase := b.imageBase
	if imageBase == 0 {
		if b.dllMode {
			imageBase = defaultDLLImageBase
		} else {
			imageBase = defaultExeImageBase
		}
	}
	stackReserve := orDefault(b.stackReserve, defaultStackReserve)
	stackCommit  := orDefault(b.stackCommit, defaultStackCommit)
	heapReserve  := orDefault(b.heapReserve, defaultHeapReserve)
	heapCommit   := orDefault(b.heapCommit, defaultHeapCommit)

	// Step 2: Count synthetic sections.
	needsIdata := len(b.imports) > 0
	needsEdata := len(b.exports) > 0
	needsReloc := b.dllCharacteristics&IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE != 0
	needsDebug := b.debug != nil

	syntheticCount := 0
	if needsIdata { syntheticCount++ }
	if needsEdata { syntheticCount++ }
	if needsReloc { syntheticCount++ }
	if needsDebug { syntheticCount++ }

	numSections := len(b.sections) + syntheticCount

	// Step 3: Header region size and first section VA.
	sizeOfHeaders := align32(
		uint32(fixedHeaderBytes+numSections*sectionHeaderSize),
		fileAlignment,
	)
	firstSectionVA := align32(sizeOfHeaders, sectionAlignment)

	// Step 4: Pre-size synthetic sections.
	var idataPreSize, edataPreSize, debugPreSize uint32

	if needsIdata {
		pre, err := buildIDATA(b.imports, 0)
		if err != nil {
			return nil, fmt.Errorf("pe: pre-sizing .idata: %w", err)
		}
		idataPreSize = uint32(len(pre.Data))
	}
	if needsEdata {
		edataPreSize = edataSize(b.exports, b.dllName)
	}
	if needsDebug {
		debugPreSize = align32(28+uint32(len(b.debug.Data)), 4)
	}

	// Step 5: Assign virtual addresses.
	type sectionLayout struct {
		name        string
		chars       uint32
		virtualAddr uint32
		virtualSize uint32
		data        []byte
	}

	userLayouts := make([]sectionLayout, len(b.sections))
	va := firstSectionVA
	for i, sec := range b.sections {
		vsz := sec.VirtualSize
		if vsz == 0 {
			vsz = uint32(len(sec.Data))
		}
		userLayouts[i] = sectionLayout{
			name:        sec.Name,
			chars:       sec.Chars,
			virtualAddr: va,
			virtualSize: vsz,
			data:        append([]byte(nil), sec.Data...),
		}
		va = align32(va+vsz, sectionAlignment)
	}

	var idataLayout, edataLayout, debugLayout sectionLayout

	if needsIdata {
		idataLayout = sectionLayout{
			name:        ".idata",
			chars:       IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE,
			virtualAddr: va,
			virtualSize: idataPreSize,
		}
		va = align32(va+idataPreSize, sectionAlignment)
	}
	if needsEdata {
		edataLayout = sectionLayout{
			name:        ".edata",
			chars:       IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ,
			virtualAddr: va,
			virtualSize: edataPreSize,
		}
		va = align32(va+edataPreSize, sectionAlignment)
	}
	if needsDebug {
		debugLayout = sectionLayout{
			name:        ".debug",
			chars:       IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_DISCARDABLE,
			virtualAddr: va,
			virtualSize: debugPreSize,
		}
		va = align32(va+debugPreSize, sectionAlignment)
	}
	relocVA := va

	// Step 6: Build symbol VA map.
	symVA := make(map[string]uint64, len(b.symbols))
	for _, sym := range b.symbols {
		for _, lay := range userLayouts {
			if lay.name == sym.Section {
				symVA[sym.Name] = imageBase + uint64(lay.virtualAddr) + uint64(sym.Offset)
				break
			}
		}
	}

	// Step 7: Apply user relocations, collect base-reloc entries.
	type baseRelocEntry struct {
		rva uint32
		typ uint8
	}
	var baseRelocs []baseRelocEntry
	le := binary.LittleEndian

	for _, rel := range b.relocs {
		var lay *sectionLayout
		for i := range userLayouts {
			if userLayouts[i].name == rel.Section {
				lay = &userLayouts[i]
				break
			}
		}
		if lay == nil {
			return nil, fmt.Errorf("pe: reloc references unknown section %q", rel.Section)
		}
		targetVA, ok := symVA[rel.Symbol]
		if !ok {
			return nil, fmt.Errorf("pe: reloc references unknown symbol %q", rel.Symbol)
		}
		targetRVA := uint32(targetVA - imageBase)
		patchRVA  := lay.virtualAddr + rel.Offset

		switch rel.Type {
		case IMAGE_REL_AMD64_ADDR64:
			if int(rel.Offset)+8 > len(lay.data) {
				return nil, fmt.Errorf("pe: ADDR64 reloc offset %d out of range", rel.Offset)
			}
			le.PutUint64(lay.data[rel.Offset:], targetVA)
			baseRelocs = append(baseRelocs, baseRelocEntry{patchRVA, IMAGE_REL_BASED_DIR64})

		case IMAGE_REL_AMD64_ADDR32:
			if int(rel.Offset)+4 > len(lay.data) {
				return nil, fmt.Errorf("pe: ADDR32 reloc offset %d out of range", rel.Offset)
			}
			if targetVA > 0xFFFFFFFF {
				return nil, fmt.Errorf("pe: ADDR32 target VA 0x%X does not fit in 32 bits", targetVA)
			}
			le.PutUint32(lay.data[rel.Offset:], uint32(targetVA))
			baseRelocs = append(baseRelocs, baseRelocEntry{patchRVA, IMAGE_REL_BASED_HIGHLOW})

		case IMAGE_REL_AMD64_ADDR32NB:
			if int(rel.Offset)+4 > len(lay.data) {
				return nil, fmt.Errorf("pe: ADDR32NB reloc offset %d out of range", rel.Offset)
			}
			le.PutUint32(lay.data[rel.Offset:], targetRVA)

		case IMAGE_REL_AMD64_REL32:
			if int(rel.Offset)+4 > len(lay.data) {
				return nil, fmt.Errorf("pe: REL32 reloc offset %d out of range", rel.Offset)
			}
			rel32 := int64(targetVA) - int64(imageBase+uint64(patchRVA)+4)
			if rel32 < -0x80000000 || rel32 > 0x7FFFFFFF {
				return nil, fmt.Errorf("pe: REL32 displacement 0x%X out of range for %q", rel32, rel.Symbol)
			}
			le.PutUint32(lay.data[rel.Offset:], uint32(int32(rel32)))

		case IMAGE_REL_AMD64_REL32_1, IMAGE_REL_AMD64_REL32_2,
			IMAGE_REL_AMD64_REL32_3, IMAGE_REL_AMD64_REL32_4,
			IMAGE_REL_AMD64_REL32_5:
			if int(rel.Offset)+4 > len(lay.data) {
				return nil, fmt.Errorf("pe: REL32_N reloc offset %d out of range", rel.Offset)
			}
			extra := int64(rel.Type - IMAGE_REL_AMD64_REL32)
			rel32 := int64(targetVA) - int64(imageBase+uint64(patchRVA)+4+uint32(extra))
			if rel32 < -0x80000000 || rel32 > 0x7FFFFFFF {
				return nil, fmt.Errorf("pe: REL32_%d displacement out of range", extra)
			}
			le.PutUint32(lay.data[rel.Offset:], uint32(int32(rel32)))

		case IMAGE_REL_AMD64_SECREL:
			if int(rel.Offset)+4 > len(lay.data) {
				return nil, fmt.Errorf("pe: SECREL reloc offset %d out of range", rel.Offset)
			}
			le.PutUint32(lay.data[rel.Offset:], targetRVA-lay.virtualAddr)

		case IMAGE_REL_AMD64_ABSOLUTE:
			// no-op

		default:
			return nil, fmt.Errorf("pe: unsupported relocation type 0x%04X for %q", rel.Type, rel.Symbol)
		}
	}

	// Step 8: Build synthetic sections with final VAs.
	if needsIdata {
		idat, err := buildIDATA(b.imports, idataLayout.virtualAddr)
		if err != nil {
			return nil, fmt.Errorf("pe: building .idata: %w", err)
		}
		idataLayout.data        = idat.Data
		idataLayout.virtualSize = uint32(len(idat.Data))
	}
	if needsEdata {
		eb, err := buildEDATA(b.exports, b.dllName, b.symbols, userLayouts, imageBase)
		if err != nil {
			return nil, fmt.Errorf("pe: building .edata: %w", err)
		}
		edataLayout.data        = eb
		edataLayout.virtualSize = uint32(len(eb))
	}
	if needsDebug {
		debugLayout.data        = buildDebugSection(b.debug, debugLayout.virtualAddr)
		debugLayout.virtualSize = uint32(len(debugLayout.data))
	}

	var relocLayout sectionLayout
	if needsReloc {
		rb := buildBaseReloc(baseRelocs)
		relocLayout = sectionLayout{
			name:        ".reloc",
			chars:       IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_DISCARDABLE,
			virtualAddr: relocVA,
			virtualSize: uint32(len(rb)),
			data:        rb,
		}
	}

	// Step 9: Assemble final section list and SizeOfImage.
	var allLayouts []sectionLayout
	allLayouts = append(allLayouts, userLayouts...)
	if needsIdata { allLayouts = append(allLayouts, idataLayout) }
	if needsEdata { allLayouts = append(allLayouts, edataLayout) }
	if needsDebug { allLayouts = append(allLayouts, debugLayout) }
	if needsReloc { allLayouts = append(allLayouts, relocLayout) }

	sizeOfImage := align32(sizeOfHeaders, sectionAlignment)
	for _, lay := range allLayouts {
		if end := align32(lay.virtualAddr+lay.virtualSize, sectionAlignment); end > sizeOfImage {
			sizeOfImage = end
		}
	}

	// Step 10: Resolve entry-point RVA.
	var entryRVA uint32
	if b.entry != "" {
		v, ok := symVA[b.entry]
		if !ok {
			return nil, fmt.Errorf("pe: entry point symbol %q not found", b.entry)
		}
		entryRVA = uint32(v - imageBase)
	}

	// Step 11: Populate data directories.
	var dataDirs [numDataDirectories]DataDirectory
	if needsIdata {
		idat, _ := buildIDATA(b.imports, idataLayout.virtualAddr)
		dataDirs[dataDirImport] = DataDirectory{idat.ImportDirRVA, idat.ImportDirSize}
		dataDirs[dataDirIAT]    = DataDirectory{idat.IATRVA, idat.IATSize}
	}
	if needsEdata {
		dataDirs[dataDirExport] = DataDirectory{edataLayout.virtualAddr, edataLayout.virtualSize}
	}
	if needsReloc {
		dataDirs[dataDirBaseReloc] = DataDirectory{relocLayout.virtualAddr, relocLayout.virtualSize}
	}
	if needsDebug {
		dataDirs[dataDirDebug] = DataDirectory{debugLayout.virtualAddr, 28}
	}

	// Step 12: File characteristics.
	fileChars := IMAGE_FILE_EXECUTABLE_IMAGE | IMAGE_FILE_LARGE_ADDRESS_AWARE | b.extraFileChars
	if b.dllMode {
		fileChars |= IMAGE_FILE_DLL
	}
	if needsReloc && len(relocLayout.data) == 0 {
		fileChars |= IMAGE_FILE_RELOCS_STRIPPED
	}

	// Step 13: Convert to PESection slice.
	peSections := make([]PESection, len(allLayouts))
	for i, lay := range allLayouts {
		peSections[i] = PESection{
			Name:           lay.name,
			Chars:          lay.chars,
			VirtualAddress: lay.virtualAddr,
			VirtualSize:    lay.virtualSize,
			Data:           padToAlignment(lay.data, fileAlignment),
		}
	}

	return &PEImage{
		Arch:                b.arch,
		Subsystem:           b.subsystem,
		ImageBase:           imageBase,
		Sections:            peSections,
		DataDirs:            dataDirs,
		EntryRVA:            entryRVA,
		StackReserve:        stackReserve,
		StackCommit:         stackCommit,
		HeapReserve:         heapReserve,
		HeapCommit:          heapCommit,
		DllCharacteristics:  b.dllCharacteristics,
		FileCharacteristics: fileChars,
		IsDLL:               b.dllMode,
	}, nil
}