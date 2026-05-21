package encode

import "math/bits"

func AppendULEB128(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			break
		}
	}
	return dst
}

func AppendSLEB128(dst []byte, v int64) []byte {
	more := true
	for more {
		b := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			more = false
		} else {
			b |= 0x80
		}
		dst = append(dst, b)
	}
	return dst
}

func AppendU32(dst []byte, v uint32) []byte { return AppendULEB128(dst, uint64(v)) }
func AppendS32(dst []byte, v int32) []byte  { return AppendSLEB128(dst, int64(v)) }
func AppendS33(dst []byte, v int64) []byte  { return AppendSLEB128(dst, v) }

func ULEBSize(v uint64) int {
	if v == 0 {
		return 1
	}
	return (bits.Len64(v) + 6) / 7
}

func AppendVecHeader(dst []byte, count uint32) []byte { return AppendU32(dst, count) }

func AppendBytes(dst, data []byte) []byte {
	dst = AppendU32(dst, uint32(len(data)))
	return append(dst, data...)
}

func AppendString(dst []byte, s string) []byte {
	dst = AppendU32(dst, uint32(len(s)))
	return append(dst, s...)
}