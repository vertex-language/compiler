// reader.go
package elf

import (
	"encoding/binary"
	"fmt"
)

// reader is a bounds-checked little-endian view over a raw byte slice.
// Every method returns an error on out-of-bounds access rather than panicking.
type reader struct {
	data []byte
}

func newReader(data []byte) *reader { return &reader{data} }

func (r *reader) u8(off int) (uint8, error) {
	if off >= len(r.data) {
		return 0, fmt.Errorf("u8 at 0x%x: out of bounds (len=%d)", off, len(r.data))
	}
	return r.data[off], nil
}

func (r *reader) u16(off int) (uint16, error) {
	if off+2 > len(r.data) {
		return 0, fmt.Errorf("u16 at 0x%x: out of bounds", off)
	}
	return binary.LittleEndian.Uint16(r.data[off:]), nil
}

func (r *reader) u32(off int) (uint32, error) {
	if off+4 > len(r.data) {
		return 0, fmt.Errorf("u32 at 0x%x: out of bounds", off)
	}
	return binary.LittleEndian.Uint32(r.data[off:]), nil
}

func (r *reader) u32be(off int) (uint32, error) {
	if off+4 > len(r.data) {
		return 0, fmt.Errorf("u32be at 0x%x: out of bounds", off)
	}
	return binary.BigEndian.Uint32(r.data[off:]), nil
}

func (r *reader) u64be(off int) (uint64, error) {
	if off+8 > len(r.data) {
		return 0, fmt.Errorf("u64be at 0x%x: out of bounds", off)
	}
	return binary.BigEndian.Uint64(r.data[off:]), nil
}

func (r *reader) u64(off int) (uint64, error) {
	if off+8 > len(r.data) {
		return 0, fmt.Errorf("u64 at 0x%x: out of bounds", off)
	}
	return binary.LittleEndian.Uint64(r.data[off:]), nil
}

func (r *reader) i64(off int) (int64, error) {
	v, err := r.u64(off)
	return int64(v), err
}

// slice returns a copy of data[off : off+size].
func (r *reader) slice(off, size int) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	if off+size > len(r.data) {
		return nil, fmt.Errorf("slice [0x%x:0x%x]: out of bounds (len=%d)", off, off+size, len(r.data))
	}
	out := make([]byte, size)
	copy(out, r.data[off:])
	return out, nil
}

// view returns data[off : off+size] without copying — caller must not mutate.
func (r *reader) view(off, size int) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	if off+size > len(r.data) {
		return nil, fmt.Errorf("view [0x%x:0x%x]: out of bounds (len=%d)", off, off+size, len(r.data))
	}
	return r.data[off : off+size], nil
}

// put32 writes a little-endian uint32 at off (in-place mutation).
func (r *reader) put32(off int, v uint32) error {
	if off+4 > len(r.data) {
		return fmt.Errorf("put32 at 0x%x: out of bounds", off)
	}
	binary.LittleEndian.PutUint32(r.data[off:], v)
	return nil
}

// put64 writes a little-endian uint64 at off.
func (r *reader) put64(off int, v uint64) error {
	if off+8 > len(r.data) {
		return fmt.Errorf("put64 at 0x%x: out of bounds", off)
	}
	binary.LittleEndian.PutUint64(r.data[off:], v)
	return nil
}

// cstr reads a NUL-terminated string from strtab starting at off.
func cstr(strtab []byte, off uint32) (string, error) {
	if int(off) >= len(strtab) {
		return "", fmt.Errorf("cstr: offset 0x%x out of bounds (len=%d)", off, len(strtab))
	}
	end := int(off)
	for end < len(strtab) && strtab[end] != 0 {
		end++
	}
	return string(strtab[off:end]), nil
}

// alignUp rounds v up to the next multiple of align (must be power of two).
func alignUp(v, align uint64) uint64 {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}