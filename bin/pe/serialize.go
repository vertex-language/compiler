package pe

import (
	"encoding/binary"
)

// Emit serializes a fully-resolved PEImage into raw PE32+ bytes.
func serialize(img *PEImage) ([]byte, error) {
	le := binary.LittleEndian

	imageBase   := img.ImageBase
	if imageBase == 0 {
		if img.IsDLL { imageBase = defaultDLLImageBase } else { imageBase = defaultExeImageBase }
	}
	stackReserve := orDefault(img.StackReserve, defaultStackReserve)
	stackCommit  := orDefault(img.StackCommit, defaultStackCommit)
	heapReserve  := orDefault(img.HeapReserve, defaultHeapReserve)
	heapCommit   := orDefault(img.HeapCommit, defaultHeapCommit)

	numSec        := len(img.Sections)
	sizeOfHeaders := align32(uint32(fixedHeaderBytes+numSec*sectionHeaderSize), fileAlignment)

	sizeOfImage := align32(sizeOfHeaders, sectionAlignment)
	for _, sec := range img.Sections {
		if end := align32(sec.VirtualAddress+sec.VirtualSize, sectionAlignment); end > sizeOfImage {
			sizeOfImage = end
		}
	}

	var sizeOfCode, sizeOfInitData, sizeOfUninitData, baseOfCode uint32
	for _, sec := range img.Sections {
		if sec.Chars&IMAGE_SCN_CNT_CODE != 0 {
			sizeOfCode += align32(sec.VirtualSize, fileAlignment)
			if baseOfCode == 0 { baseOfCode = sec.VirtualAddress }
		}
		if sec.Chars&IMAGE_SCN_CNT_INITIALIZED_DATA != 0 {
			sizeOfInitData += align32(sec.VirtualSize, fileAlignment)
		}
		if sec.Chars&IMAGE_SCN_CNT_UNINITIALIZED_DATA != 0 {
			sizeOfUninitData += align32(sec.VirtualSize, fileAlignment)
		}
	}

	sectionFileOffsets := make([]uint32, numSec)
	fileOff := sizeOfHeaders
	for i, sec := range img.Sections {
		if len(sec.Data) > 0 {
			sectionFileOffsets[i] = fileOff
			fileOff += uint32(len(sec.Data))
		}
	}
	out := make([]byte, fileOff)

	// DOS stub
	copy(out[0:], dosStub[:])

	// PE signature
	peOff := uint32(dosStubSize)
	copy(out[peOff:], []byte{'P', 'E', 0, 0})

	// COFF header
	coffOff := peOff + peSignatureSize
	le.PutUint16(out[coffOff+0:], uint16(img.Arch))
	le.PutUint16(out[coffOff+2:], uint16(numSec))
	le.PutUint32(out[coffOff+4:], 0)
	le.PutUint32(out[coffOff+8:], 0)
	le.PutUint32(out[coffOff+12:], 0)
	le.PutUint16(out[coffOff+16:], optHeaderSize)
	le.PutUint16(out[coffOff+18:], img.FileCharacteristics)

	// Optional header
	optOff := coffOff + coffHeaderSize
	le.PutUint16(out[optOff+0:], 0x020B)
	out[optOff+2] = 1
	le.PutUint32(out[optOff+4:], sizeOfCode)
	le.PutUint32(out[optOff+8:], sizeOfInitData)
	le.PutUint32(out[optOff+12:], sizeOfUninitData)
	le.PutUint32(out[optOff+16:], img.EntryRVA)
	le.PutUint32(out[optOff+20:], baseOfCode)

	w := optOff + 24
	le.PutUint64(out[w+0:], imageBase)
	le.PutUint32(out[w+8:], sectionAlignment)
	le.PutUint32(out[w+12:], fileAlignment)
	le.PutUint16(out[w+16:], 6)
	le.PutUint16(out[w+24:], 6)
	le.PutUint32(out[w+32:], sizeOfImage)
	le.PutUint32(out[w+36:], sizeOfHeaders)
	le.PutUint16(out[w+44:], uint16(img.Subsystem))
	le.PutUint16(out[w+46:], img.DllCharacteristics)
	le.PutUint64(out[w+48:], stackReserve)
	le.PutUint64(out[w+56:], stackCommit)
	le.PutUint64(out[w+64:], heapReserve)
	le.PutUint64(out[w+72:], heapCommit)
	le.PutUint32(out[w+84:], numDataDirectories)

	dd := optOff + 112
	for i, dir := range img.DataDirs {
		le.PutUint32(out[dd+i*8:], dir.VirtualAddress)
		le.PutUint32(out[dd+i*8+4:], dir.Size)
	}

	// Section headers
	shOff := optOff + optHeaderSize
	for i, sec := range img.Sections {
		h := shOff + i*sectionHeaderSize
		name := sec.Name
		if len(name) > 8 { name = name[:8] }
		copy(out[h:h+8], name)
		le.PutUint32(out[h+8:], sec.VirtualSize)
		le.PutUint32(out[h+12:], sec.VirtualAddress)
		le.PutUint32(out[h+16:], uint32(len(sec.Data)))
		le.PutUint32(out[h+20:], sectionFileOffsets[i])
		le.PutUint32(out[h+36:], sec.Chars)
	}

	// Raw section data
	for i, sec := range img.Sections {
		if len(sec.Data) > 0 {
			copy(out[sectionFileOffsets[i]:], sec.Data)
		}
	}

	return out, nil
}