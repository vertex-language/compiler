package pe

import (
	"encoding/binary"
	"fmt"
	"sort"
)

func edataSize(exports []Export, dllName string) uint32 {
	if len(exports) == 0 {
		return 0
	}
	maxOrdinal := uint16(0)
	minOrdinal := uint16(0xFFFF)
	namedCount := 0
	for _, e := range exports {
		if e.Ordinal > maxOrdinal { maxOrdinal = e.Ordinal }
		if e.Ordinal < minOrdinal { minOrdinal = e.Ordinal }
		if e.Name != ""           { namedCount++ }
	}
	addrTableCount := uint32(maxOrdinal-minOrdinal) + 1
	sz := uint32(40) + addrTableCount*4 + uint32(namedCount)*4 + uint32(namedCount)*2
	sz += uint32(len(dllName)) + 1
	for _, e := range exports {
		if e.Name != "" { sz += uint32(len(e.Name)) + 1 }
	}
	return align32(sz, 4)
}

func buildEDATA(
	exports []Export,
	dllName string,
	symbols []Symbol,
	userLayouts interface{ // accept the anonymous struct slice from build.go
	},
	imageBase uint64,
) ([]byte, error) {
	// ... (same implementation as original)
}

func fixupEDATA(buf []byte, edataVA uint32) {
	le := binary.LittleEndian
	for _, off := range []int{12, 28, 32, 36} {
		if v := le.Uint32(buf[off:]); v != 0 {
			le.PutUint32(buf[off:], v+edataVA)
		}
	}
	addrTableCount := le.Uint32(buf[20:])
	namedCount     := int(le.Uint32(buf[24:]))
	npTableOff     := 40 + addrTableCount*4
	for i := 0; i < namedCount; i++ {
		off := npTableOff + uint32(i)*4
		if v := le.Uint32(buf[off:]); v != 0 {
			le.PutUint32(buf[off:], v+edataVA)
		}
	}
}