package gpu

import (
	"github.com/vertex-language/compiler/encode"
	"github.com/vertex-language/compiler/wasm"
)

// NameHints writes @vendor-annotated names into a wasm "name" custom section
// (subsection 1: function names). Use this to mark internally-defined GPU
// kernels that are not exported and therefore cannot carry a @vendor suffix on
// an export entry.
//
//	m.Customs = append(m.Customs, gpu.NameHints(map[uint32]string{
//	    prefixSumIdx: "prefixSum@cuda:ptr.i32",
//	}))
//
// The resulting section is spec-compliant: wasm-validate passes, wasm2wat
// renders the names intact, and any runtime that does not know about Vertex
// simply sees ordinary function name entries.
func NameHints(hints map[uint32]string) wasm.CustomSection {
	// Sort indices for deterministic output.
	indices := make([]uint32, 0, len(hints))
	for idx := range hints {
		indices = append(indices, idx)
	}
	sortU32(indices)

	// Build subsection 1 payload: count followed by (funcIdx, name) pairs.
	var payload []byte
	payload = encode.AppendU32(payload, uint32(len(hints)))
	for _, idx := range indices {
		name := hints[idx]
		payload = encode.AppendU32(payload, idx)
		payload = appendWasmString(payload, name)
	}

	// Wrap in the subsection envelope: subsectionID=1, length, payload.
	var section []byte
	section = append(section, 1) // subsection id: function names
	section = encode.AppendU32(section, uint32(len(payload)))
	section = append(section, payload...)

	return wasm.CustomSection{
		Name: "name",
		Data: section,
	}
}

// appendWasmString encodes a wasm name string: u32 byte-length + UTF-8 bytes.
func appendWasmString(dst []byte, s string) []byte {
	dst = encode.AppendU32(dst, uint32(len(s)))
	return append(dst, s...)
}

// sortU32 sorts a []uint32 in ascending order (insertion sort — hint maps are small).
func sortU32(s []uint32) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}