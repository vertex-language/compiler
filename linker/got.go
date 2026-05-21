// Package linker — got.go
// Global Offset Table allocator.
//
// The GOT is a contiguous array of 8-byte slots appended to the RW PT_LOAD
// segment.  Two classes of slots exist:
//
//   • Regular slots — hold the absolute VA of a non-TLS symbol.
//     Written by fillGOT() once all section VAs are known.
//
//   • TLS IE slots  — hold tpoff(sym), the signed offset from the thread
//     pointer to the TLS variable.  Written by layoutTLS() using only
//     section sizes (no absolute VAs needed).
//
// Allocation is idempotent: the first GOT-generating relocation for a given
// symbol reserves its slot; subsequent references reuse it.
package linker

import "encoding/binary"

type got struct {
	regIdx map[string]int // regular symbol → slot index
	tlsIdx map[string]int // TLS symbol     → slot index
	nSlots int
	data   []byte // nSlots × 8 bytes; zero-initialised by make
}

func newGOT() *got {
	return &got{
		regIdx: make(map[string]int),
		tlsIdx: make(map[string]int),
	}
}

// slotFor returns (allocating on the first call) the slot index for a regular
// symbol.
func (g *got) slotFor(sym string) int {
	if idx, ok := g.regIdx[sym]; ok {
		return idx
	}
	idx := g.alloc()
	g.regIdx[sym] = idx
	return idx
}

// tlsSlotFor returns (allocating on the first call) the TLS IE slot index for
// a TLS symbol.
func (g *got) tlsSlotFor(sym string) int {
	if idx, ok := g.tlsIdx[sym]; ok {
		return idx
	}
	idx := g.alloc()
	g.tlsIdx[sym] = idx
	return idx
}

func (g *got) alloc() int {
	idx := g.nSlots
	g.nSlots++
	g.data = append(g.data, 0, 0, 0, 0, 0, 0, 0, 0)
	return idx
}

// set writes a 64-bit little-endian value into slot idx.
func (g *got) set(idx int, val uint64) {
	binary.LittleEndian.PutUint64(g.data[idx*8:], val)
}

// slotVA returns the virtual address of a slot given the GOT section's base VA.
func (g *got) slotVA(idx int, gotBaseVA uint64) uint64 {
	return gotBaseVA + uint64(idx*8)
}

// forEachReg calls f for every regular (non-TLS) slot.
func (g *got) forEachReg(f func(name string, idx int)) {
	for name, idx := range g.regIdx {
		f(name, idx)
	}
}

// Content returns the raw GOT bytes (len = Size()).
func (g *got) Content() []byte { return g.data }

// Size returns the GOT byte size.
func (g *got) Size() uint64 { return uint64(g.nSlots * 8) }