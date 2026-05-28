package pe

import "encoding/binary"

// serialize writes a fully-resolved PEImage to a flat byte slice.
// All virtual addresses, relocations, and Windows-specific tables must
// already be finalized in img before this is called; serialize performs
// no further resolution beyond computing file offsets.
func serialize(img *PEImage) ([]byte, error) {
	le := binary.LittleEndian

	numSections := len(img.Sections)

	// ── Resolve image-base and size defaults ──────────────────────────────────
	imageBase := img.ImageBase
	if imageBase == 0 {
		if img.IsDLL {
			imageBase = defaultDLLImageBase
		} else {
			imageBase = defaultExeImageBase
		}
	}
	stackReserve := orDefault(img.StackReserve, defaultStackReserve)
	stackCommit  := orDefault(img.StackCommit,  defaultStackCommit)
	heapReserve  := orDefault(img.HeapReserve,  defaultHeapReserve)
	heapCommit   := orDefault(img.HeapCommit,   defaultHeapCommit)

	majorOS := img.MajorOSVersion
	minorOS := img.MinorOSVersion
	if majorOS == 0 {
		majorOS = 6
	}
	majorSS := img.MajorSubsystemVersion
	minorSS := img.MinorSubsystemVersion
	if majorSS == 0 {
		majorSS = 6
	}

	// ── Compute SizeOfHeaders ─────────────────────────────────────────────────
	sizeOfHeaders := align32(
		uint32(fixedHeaderBytes+numSections*sectionHeaderSize),
		fileAlignment,
	)

	// ── Compute SizeOfImage and optional-header statistics ────────────────────
	sizeOfImage := align32(sizeOfHeaders, sectionAlignment)
	var sizeOfCode, sizeOfInitData, sizeOfUninitData, baseOfCode uint32
	for _, sec := range img.Sections {
		top := align32(sec.VirtualAddress+sec.VirtualSize, sectionAlignment)
		if top > sizeOfImage {
			sizeOfImage = top
		}
		rawSz := align32(sec.VirtualSize, fileAlignment)
		switch {
		case sec.Chars&IMAGE_SCN_CNT_CODE != 0:
			sizeOfCode += rawSz
			if baseOfCode == 0 {
				baseOfCode = sec.VirtualAddress
			}
		case sec.Chars&IMAGE_SCN_CNT_INITIALIZED_DATA != 0:
			sizeOfInitData += rawSz
		case sec.Chars&IMAGE_SCN_CNT_UNINITIALIZED_DATA != 0:
			sizeOfUninitData += rawSz
		}
	}

	// ── Assign file offsets to sections ───────────────────────────────────────
	// Section Data is already padded to fileAlignment by buildImage.
	secFileOff := make([]uint32, numSections)
	cur := sizeOfHeaders
	for i, sec := range img.Sections {
		if len(sec.Data) > 0 {
			secFileOff[i] = cur
			cur += uint32(len(sec.Data))
		}
	}

	// ── Allocate output buffer ────────────────────────────────────────────────
	out := make([]byte, int(cur))

	// ── DOS stub ──────────────────────────────────────────────────────────────
	copy(out[0:], dosStub[:])

	// ── PE signature ("PE\0\0") ───────────────────────────────────────────────
	copy(out[dosStubSize:], []byte{'P', 'E', 0, 0})

	// ── COFF file header (20 bytes) ───────────────────────────────────────────
	const coffBase = dosStubSize + peSignatureSize
	le.PutUint16(out[coffBase+0:],  uint16(img.Machine))
	le.PutUint16(out[coffBase+2:],  uint16(numSections))
	le.PutUint32(out[coffBase+4:],  0)                    // TimeDateStamp: 0 = reproducible
	le.PutUint32(out[coffBase+8:],  0)                    // PointerToSymbolTable
	le.PutUint32(out[coffBase+12:], 0)                    // NumberOfSymbols
	le.PutUint16(out[coffBase+16:], uint16(optHeaderSize))
	le.PutUint16(out[coffBase+18:], img.FileCharacteristics)

	// ── Optional header PE32+ (240 bytes) ────────────────────────────────────
	const optBase = coffBase + coffHeaderSize
	le.PutUint16(out[optBase+0:],   0x020B)               // Magic: PE32+
	out[optBase+2] = 14                                    // MajorLinkerVersion
	out[optBase+3] = 0                                     // MinorLinkerVersion
	le.PutUint32(out[optBase+4:],   sizeOfCode)
	le.PutUint32(out[optBase+8:],   sizeOfInitData)
	le.PutUint32(out[optBase+12:],  sizeOfUninitData)
	le.PutUint32(out[optBase+16:],  img.EntryRVA)
	le.PutUint32(out[optBase+20:],  baseOfCode)
	le.PutUint64(out[optBase+24:],  imageBase)
	le.PutUint32(out[optBase+32:],  sectionAlignment)
	le.PutUint32(out[optBase+36:],  fileAlignment)
	le.PutUint16(out[optBase+40:],  majorOS)
	le.PutUint16(out[optBase+42:],  minorOS)
	le.PutUint16(out[optBase+44:],  0)                    // MajorImageVersion
	le.PutUint16(out[optBase+46:],  0)                    // MinorImageVersion
	le.PutUint16(out[optBase+48:],  majorSS)
	le.PutUint16(out[optBase+50:],  minorSS)
	le.PutUint32(out[optBase+52:],  0)                    // Win32VersionValue (reserved)
	le.PutUint32(out[optBase+56:],  sizeOfImage)
	le.PutUint32(out[optBase+60:],  sizeOfHeaders)
	le.PutUint32(out[optBase+64:],  0)                    // CheckSum
	le.PutUint16(out[optBase+68:],  uint16(img.Subsystem))
	le.PutUint16(out[optBase+70:],  img.DllCharacteristics)
	le.PutUint64(out[optBase+72:],  stackReserve)
	le.PutUint64(out[optBase+80:],  stackCommit)
	le.PutUint64(out[optBase+88:],  heapReserve)
	le.PutUint64(out[optBase+96:],  heapCommit)
	le.PutUint32(out[optBase+104:], 0)                    // LoaderFlags (reserved)
	le.PutUint32(out[optBase+108:], NumDataDirectories)

	// Data directories: 16 × 8 bytes at optBase+112.
	for i, dd := range img.DataDirs {
		le.PutUint32(out[optBase+112+i*8:],   dd.VirtualAddress)
		le.PutUint32(out[optBase+112+i*8+4:], dd.Size)
	}

	// ── Section headers (40 bytes each) ──────────────────────────────────────
	secHdrBase := optBase + optHeaderSize
	for i, sec := range img.Sections {
		h := secHdrBase + i*sectionHeaderSize
		name := sec.Name
		if len(name) > 8 {
			name = name[:8]
		}
		copy(out[h:h+8], name)
		le.PutUint32(out[h+8:],  sec.VirtualSize)
		le.PutUint32(out[h+12:], sec.VirtualAddress)
		rawSz := uint32(len(sec.Data))
		le.PutUint32(out[h+16:], rawSz)
		if rawSz > 0 {
			le.PutUint32(out[h+20:], secFileOff[i]) // PointerToRawData
		}
		// h+24: PointerToRelocations = 0 (image file)
		// h+28: PointerToLinenumbers = 0
		// h+32: NumberOfRelocations  = 0
		// h+34: NumberOfLinenumbers  = 0
		le.PutUint32(out[h+36:], sec.Chars)
	}

	// ── Section raw data ──────────────────────────────────────────────────────
	for i, sec := range img.Sections {
		if len(sec.Data) > 0 {
			copy(out[secFileOff[i]:], sec.Data)
		}
	}

	// ── Fix up IMAGE_DEBUG_DIRECTORY.PointerToRawData ─────────────────────────
	// buildDebugSection stores PointerToRawData as a section-relative offset so
	// that serialize (the only caller that knows final file offsets) can convert
	// it to a true file pointer here.
	debugDirRVA  := img.DataDirs[DataDirDebug].VirtualAddress
	debugDirSize := img.DataDirs[DataDirDebug].Size
	if debugDirRVA != 0 && debugDirSize >= 28 {
		numDbgEntries := int(debugDirSize / 28)
		for si, sec := range img.Sections {
			if sec.VirtualAddress <= debugDirRVA &&
				debugDirRVA < sec.VirtualAddress+sec.VirtualSize {
				dirOffInSec := debugDirRVA - sec.VirtualAddress
				for j := 0; j < numDbgEntries; j++ {
					// PointerToRawData is at byte +24 within each 28-byte entry.
					ptrFieldOff := secFileOff[si] + dirOffInSec + uint32(j*28) + 24
					sectRelPtr := le.Uint32(out[ptrFieldOff:])
					le.PutUint32(out[ptrFieldOff:], sectRelPtr+secFileOff[si])
				}
				break
			}
		}
	}

	return out, nil
}