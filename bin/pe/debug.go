package pe

import "encoding/binary"

func buildDebugSection(d *DebugData, sectionVA uint32) []byte {
	le := binary.LittleEndian
	const dirSize = 28
	rawOff    := uint32(dirSize)
	totalSize := align32(dirSize+uint32(len(d.Data)), 4)
	buf       := make([]byte, totalSize)

	le.PutUint32(buf[12:], d.Type)
	le.PutUint32(buf[16:], uint32(len(d.Data)))
	le.PutUint32(buf[20:], sectionVA+rawOff)
	copy(buf[rawOff:], d.Data)
	return buf
}