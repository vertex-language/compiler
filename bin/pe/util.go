package pe

// align32 rounds v up to the nearest multiple of align (which must be a power of two).
func align32(v, align uint32) uint32 {
	return (v + align - 1) &^ (align - 1)
}

// align64 rounds v up to the nearest multiple of align (which must be a power of two).
func align64(v, align uint64) uint64 {
	return (v + align - 1) &^ (align - 1)
}

// orDefault returns v if v != 0, otherwise returns def.
func orDefault(v, def uint64) uint64 {
	if v != 0 {
		return v
	}
	return def
}

// padToAlignment appends zero bytes to b until len(b) is a multiple of align.
func padToAlignment(b []byte, align uint32) []byte {
	if n := align32(uint32(len(b)), align); int(n) > len(b) {
		b = append(b, make([]byte, int(n)-len(b))...)
	}
	return b
}

// putCString writes a null-terminated ASCII string into dst starting at off,
// returning the offset just past the null byte.
func putCString(dst []byte, off int, s string) int {
	copy(dst[off:], s)
	dst[off+len(s)] = 0
	return off + len(s) + 1
}