package decode

import (
	"errors"
	"fmt"
)

var ErrUnexpectedEOF = errors.New("decode: unexpected EOF")

// Reader is a cursor over a byte slice for reading WebAssembly binary data.
type Reader struct {
	data []byte
	pos  int
}

func NewReader(data []byte) *Reader { return &Reader{data: data} }

func (r *Reader) Len() int  { return len(r.data) - r.pos }
func (r *Reader) Pos() int  { return r.pos }
func (r *Reader) EOF() bool { return r.pos >= len(r.data) }

func (r *Reader) ReadByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *Reader) ReadULEB128() (uint64, error) {
	var result uint64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, fmt.Errorf("decode: ULEB128 overflow")
		}
	}
}

func (r *Reader) ReadSLEB128() (int64, error) {
	var result int64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int64(b&0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			if shift < 64 && b&0x40 != 0 {
				result |= -(1 << shift)
			}
			return result, nil
		}
		if shift >= 64 {
			return 0, fmt.Errorf("decode: SLEB128 overflow")
		}
	}
}

func (r *Reader) ReadU32() (uint32, error) {
	v, err := r.ReadULEB128()
	return uint32(v), err
}

func (r *Reader) ReadS32() (int32, error) {
	v, err := r.ReadSLEB128()
	return int32(v), err
}

func (r *Reader) ReadS33() (int64, error) { return r.ReadSLEB128() }

// ReadFixedBytes reads exactly n bytes.
func (r *Reader) ReadFixedBytes(n uint32) ([]byte, error) {
	end := r.pos + int(n)
	if end > len(r.data) {
		return nil, ErrUnexpectedEOF
	}
	b := make([]byte, n)
	copy(b, r.data[r.pos:end])
	r.pos = end
	return b, nil
}

// ReadByteVec reads a u32-length-prefixed byte slice.
func (r *Reader) ReadByteVec() ([]byte, error) {
	n, err := r.ReadU32()
	if err != nil {
		return nil, err
	}
	return r.ReadFixedBytes(n)
}

// ReadString reads a u32-length-prefixed UTF-8 name.
func (r *Reader) ReadString() (string, error) {
	b, err := r.ReadByteVec()
	return string(b), err
}

// ReadVecHeader reads the u32 element count that opens a wasm vector.
func (r *Reader) ReadVecHeader() (uint32, error) { return r.ReadU32() }

// Sub returns a new Reader over the next n bytes, advancing r past them.
func (r *Reader) Sub(n uint32) (*Reader, error) {
	b, err := r.ReadFixedBytes(n)
	if err != nil {
		return nil, err
	}
	return NewReader(b), nil
}

// RawSlice returns a copy of data[from:to]. Used to capture raw bytes for
// ConstExpr and function bodies.
func (r *Reader) RawSlice(from, to int) []byte {
	b := make([]byte, to-from)
	copy(b, r.data[from:to])
	return b
}