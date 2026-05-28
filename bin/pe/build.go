package pe

import (
	"encoding/binary"
	"fmt"
)

// buildImage converts Builder state into a fully populated PEImage.
func (b *Builder) buildImage() (*PEImage, error) {
	le := binary.LittleEndian

	// ── Step 1: Resolve defaults ──────────────────────────────────────────────
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

	// ── Step 2: Decide which synthetic sections are needed ────────────────────
	needsIdata      := len(b.imports) > 0
	needsEdata      := len(b.exports) > 0
	needsDelayIdata := len(b.delayImports) > 0
	needsPdata      := len(b.pdataFuncs) > 0
	needsXdata      := len(b.xdataBlob) > 0
	needsTLS        := len(b.tlsData) > 0 || len(b.tlsCallbacks) > 0
	needsLoadCfg    := b.loadConfig != nil
	needsDebug      := len(b.debugEntries) > 0
	needsReloc      := b.dllCharacteristics&IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE != 0

	// .xdata is always paired with .pdata; treat them together.
	_ = needsXdata

	// Total section count determines sizeOfHeaders.
	syntheticCount := 0
	for _, flag := range []bool{needsIdata, needsEdata, needsDelayIdata,
		needsPdata, len(b.xdataBlob) > 0,
		needsTLS, needsLoadCfg, needsDebug, needsReloc} {
		if flag {
			syntheticCount++
		}
	}
	numSections := len(b.sections) + syntheticCount

	// ── Step 3: Header size and first section VA ───────────────────────────────
	sizeOfHeaders := align32(
		uint32(fixedHeaderBytes+numSections*sectionHeaderSize),
		fileAlignment,
	)
	firstSectionVA := align32(sizeOfHeaders, sectionAlignment)

	// ── Step 4: Pre-size synthetic sections ───────────────────────────────────
	var idataPreSize, edataPreSize, delayPreSize uint32

	if needsIdata {
		pre, err := buildIDATA(b.imports, 0)
		if err != nil {
			return nil, fmt.Errorf("pe: pre-sizing .idata: %w", err)
		}
		idataPreSize = uint32(len(pre.Data))
	}
	if needsEdata {
		edataPreSize = measureEDATA(b.exports, b.dllName)
	}
	if needsDelayIdata {
		pre, err := buildDelayIDATA(b.delayImports, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("pe: pre-sizing delay .idata: %w", err)
		}
		delayPreSize = uint32(len(pre.descriptorData) + len(pre.iatData) +
			len(pre.intData) + len(pre.hnData) + len(pre.nameData) + len(pre.moduleHandles))
	}
	_ = delayPreSize

	// ── Step 5: Assign virtual addresses to all sections ──────────────────────
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

	// Synthetic section layout placeholders.
	var (
		idataLayout   sectionLayout
		edataLayout   sectionLayout
		delayLayout   sectionLayout
		pdataLayout   sectionLayout
		xdataLayout   sectionLayout
		tlsLayout     sectionLayout
		loadCfgLayout sectionLayout
		debugLayout   sectionLayout
		relocLayout   sectionLayout
	)

	if needsIdata {
		idataLayout = sectionLayout{
			name:  ".idata",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE,
			virtualAddr: va, virtualSize: idataPreSize,
		}
		va = align32(va+idataPreSize, sectionAlignment)
	}
	if needsEdata {
		edataLayout = sectionLayout{
			name:  ".edata",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ,
			virtualAddr: va, virtualSize: edataPreSize,
		}
		va = align32(va+edataPreSize, sectionAlignment)
	}
	if needsDelayIdata {
		delayLayout = sectionLayout{
			name:  ".didat",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE,
			virtualAddr: va,
		}
		va = align32(va+4096, sectionAlignment) // conservative pre-size
	}
	if needsPdata {
		pdataSz := uint32(len(b.pdataFuncs) * 12)
		pdataLayout = sectionLayout{
			name:  ".pdata",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ,
			virtualAddr: va, virtualSize: pdataSz,
		}
		va = align32(va+pdataSz, sectionAlignment)
	}
	if len(b.xdataBlob) > 0 {
		xdataLayout = sectionLayout{
			name:  ".xdata",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ,
			virtualAddr: va, virtualSize: uint32(len(b.xdataBlob)),
		}
		va = align32(va+uint32(len(b.xdataBlob)), sectionAlignment)
	}
	if needsTLS {
		tlsSz := uint32(40 + len(b.tlsData) + (len(b.tlsCallbacks)+1)*8)
		tlsLayout = sectionLayout{
			name:  ".tls",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE,
			virtualAddr: va, virtualSize: tlsSz,
		}
		va = align32(va+tlsSz, sectionAlignment)
	}
	if needsLoadCfg {
		lcSz := uint32(loadConfigSize)
		loadCfgLayout = sectionLayout{
			name:  ".rdata$lc",
			chars: IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ,
			virtualAddr: va, virtualSize: lcSz,
		}
		va = align32(va+lcSz, sectionAlignment)
	}
	if needsDebug {
		debugSz := buildDebugSectionSize(b.debugEntries)
		debugLayout = sectionLayout{
			name:  ".debug",
			chars: ScnDiscardable,
			virtualAddr: va, virtualSize: debugSz,
		}
		va = align32(va+debugSz, sectionAlignment)
	}
	relocVA := va

	// ── Step 6: Build symbol VA map ───────────────────────────────────────────
	symVA := make(map[string]uint64, len(b.symbols))
	for _, sym := range b.symbols {
		for _, lay := range userLayouts {
			if lay.name == sym.Section {
				symVA[sym.Name] = imageBase + uint64(lay.virtualAddr) + uint64(sym.Offset)
				break
			}
		}
	}

	// ── Step 7: Collect base-reloc entries ────────────────────────────────────
	// NOTE: baseRelocEntry is the package-level type defined in basereloc.go.
	var baseRelocs []baseRelocEntry

	// ── Step 8: Build synthetic sections with final VAs ───────────────────────
	if needsIdata {
		idat, err := buildIDATA(b.imports, idataLayout.virtualAddr)
		if err != nil {
			return nil, fmt.Errorf("pe: building .idata: %w", err)
		}
		idataLayout.data = idat.Data
		idataLayout.virtualSize = uint32(len(idat.Data))
	}
	if needsEdata {
		eb, err := buildEDATA(b.exports, b.dllName, symVA, imageBase)
		if err != nil {
			return nil, fmt.Errorf("pe: building .edata: %w", err)
		}
		// Fix up the section-relative pointers stored by buildEDATA into real RVAs.
		fixupEDATA(eb, edataLayout.virtualAddr)
		edataLayout.data = eb
		edataLayout.virtualSize = uint32(len(eb))
	}
	if needsDelayIdata {
		ddat, err := buildDelayIDATA(b.delayImports, delayLayout.virtualAddr, imageBase)
		if err != nil {
			return nil, fmt.Errorf("pe: building delay .idata: %w", err)
		}
		delayLayout.data = ddat.flat()
		delayLayout.virtualSize = uint32(len(delayLayout.data))
	}
	if needsPdata {
		pdataLayout.data = buildPdataSection(b.pdataFuncs)
	}
	if len(b.xdataBlob) > 0 {
		xdataLayout.data = b.xdataBlob
	}
	if needsTLS {
		tlsDir, tlsData := buildTLSSection(b.tlsData, b.tlsCallbacks,
			imageBase, tlsLayout.virtualAddr)
		tlsLayout.data = tlsData
		tlsLayout.virtualSize = uint32(len(tlsData))
		// TLS directory VAs require base relocations for ASLR.
		tlsDirVA := tlsLayout.virtualAddr
		for _, off := range []uint32{0, 8, 16, 24} {
			if off < 24 || tlsDir.AddressOfCallbacks != 0 {
				baseRelocs = append(baseRelocs, baseRelocEntry{tlsDirVA + off, IMAGE_REL_BASED_DIR64})
			}
		}
		_ = tlsDir
	}
	if needsLoadCfg {
		loadCfgLayout.data = buildLoadConfig(b.loadConfig)
	}
	if needsDebug {
		debugLayout.data = buildDebugSection(b.debugEntries, debugLayout.virtualAddr)
		debugLayout.virtualSize = uint32(len(debugLayout.data))
	}

	var relocData []byte
	if needsReloc {
		relocData = buildBaseReloc(baseRelocs)
		relocLayout = sectionLayout{
			name:  ".reloc",
			chars: ScnDiscardable,
			virtualAddr: relocVA, virtualSize: uint32(len(relocData)),
			data: relocData,
		}
	}

	// ── Step 9: Assemble final section list ───────────────────────────────────
	var all []sectionLayout
	all = append(all, userLayouts...)
	if needsIdata {
		all = append(all, idataLayout)
	}
	if needsEdata {
		all = append(all, edataLayout)
	}
	if needsDelayIdata {
		all = append(all, delayLayout)
	}
	if needsPdata {
		all = append(all, pdataLayout)
	}
	if len(b.xdataBlob) > 0 {
		all = append(all, xdataLayout)
	}
	if needsTLS {
		all = append(all, tlsLayout)
	}
	if needsLoadCfg {
		all = append(all, loadCfgLayout)
	}
	if needsDebug {
		all = append(all, debugLayout)
	}
	if needsReloc {
		all = append(all, relocLayout)
	}

	// ── Step 10: SizeOfImage ──────────────────────────────────────────────────
	sizeOfImage := align32(sizeOfHeaders, sectionAlignment)
	for _, lay := range all {
		if end := align32(lay.virtualAddr+lay.virtualSize, sectionAlignment); end > sizeOfImage {
			sizeOfImage = end
		}
	}
	_ = sizeOfImage

	// ── Step 11: Resolve entry-point RVA ─────────────────────────────────────
	var entryRVA uint32
	if b.entry != "" {
		v, ok := symVA[b.entry]
		if !ok {
			return nil, fmt.Errorf("pe: entry point symbol %q not found", b.entry)
		}
		entryRVA = uint32(v - imageBase)
	}

	// ── Step 12: Populate data directories ───────────────────────────────────
	var dataDirs [NumDataDirectories]DataDirectory

	if needsIdata {
		idat, _ := buildIDATA(b.imports, idataLayout.virtualAddr)
		dataDirs[DataDirImport] = DataDirectory{idat.ImportDirRVA, idat.ImportDirSize}
		dataDirs[DataDirIAT]    = DataDirectory{idat.IATRVA, idat.IATSize}
	}
	if needsEdata {
		dataDirs[DataDirExport] = DataDirectory{edataLayout.virtualAddr, edataLayout.virtualSize}
	}
	if needsDelayIdata {
		ddat, _ := buildDelayIDATA(b.delayImports, delayLayout.virtualAddr, imageBase)
		dataDirs[DataDirDelayImport] = DataDirectory{ddat.descriptorRVA, ddat.descriptorSize}
	}
	if needsPdata {
		dataDirs[DataDirException] = DataDirectory{pdataLayout.virtualAddr, pdataLayout.virtualSize}
	}
	if needsTLS {
		dataDirs[DataDirTLS] = DataDirectory{tlsLayout.virtualAddr, 40} // sizeof IMAGE_TLS_DIRECTORY64
	}
	if needsLoadCfg {
		dataDirs[DataDirLoadConfig] = DataDirectory{loadCfgLayout.virtualAddr, loadConfigSize}
	}
	if needsDebug {
		dataDirs[DataDirDebug] = DataDirectory{
			debugLayout.virtualAddr,
			uint32(len(b.debugEntries) * 28),
		}
	}
	if needsReloc {
		dataDirs[DataDirBaseReloc] = DataDirectory{relocLayout.virtualAddr, relocLayout.virtualSize}
	}

	// Apply any caller-supplied overrides (e.g. for sections added via AddSection).
	for i, dd := range b.extraDataDirs {
		if dd.VirtualAddress != 0 || dd.Size != 0 {
			dataDirs[i] = dd
		}
	}

	// ── Step 13: File characteristics ────────────────────────────────────────
	fileChars := IMAGE_FILE_EXECUTABLE_IMAGE | IMAGE_FILE_LARGE_ADDRESS_AWARE | b.extraFileChars
	if b.dllMode {
		fileChars |= IMAGE_FILE_DLL
	}
	if needsReloc && len(relocData) == 0 {
		fileChars |= IMAGE_FILE_RELOCS_STRIPPED
	}

	// ── Step 14: Convert to PESection slice ──────────────────────────────────
	peSections := make([]PESection, len(all))
	for i, lay := range all {
		peSections[i] = PESection{
			Name:           lay.name,
			Chars:          lay.chars,
			VirtualAddress: lay.virtualAddr,
			VirtualSize:    lay.virtualSize,
			Data:           padToAlignment(lay.data, fileAlignment),
		}
	}

	_ = le // used in sub-functions

	return &PEImage{
		Machine:               b.machine,
		Subsystem:             b.subsystem,
		ImageBase:             imageBase,
		Sections:              peSections,
		DataDirs:              dataDirs,
		EntryRVA:              entryRVA,
		StackReserve:          stackReserve,
		StackCommit:           stackCommit,
		HeapReserve:           heapReserve,
		HeapCommit:            heapCommit,
		MajorOSVersion:        b.majorOSVersion,
		MinorOSVersion:        b.minorOSVersion,
		MajorSubsystemVersion: b.majorSubsystemVersion,
		MinorSubsystemVersion: b.minorSubsystemVersion,
		DllCharacteristics:    b.dllCharacteristics,
		FileCharacteristics:   fileChars,
		IsDLL:                 b.dllMode,
	}, nil
}