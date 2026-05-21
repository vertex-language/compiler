package concurrency

import (
	"github.com/vertex-language/compiler/encode"
	"github.com/vertex-language/compiler/wasm"
)

// NameHints writes @kind-annotated names into a wasm "name" custom section
// (subsection 1: function names). Use this to mark internally-defined
// concurrent functions that are not exported and therefore cannot carry a
// @kind suffix on an export entry.
//
//	m.Customs = append(m.Customs, concurrency.NameHints(map[uint32]string{
//	    workerIdx: "worker@thread:ptr.i32",
//	    genIdx:    "generator@async",
//	}))
//
// The resulting section is spec-compliant: wasm-validate passes, wasm2wat
// renders the names intact, and any runtime that does not know about Vertex
// simply sees ordinary function name entries.
func NameHints(hints map[uint32]string) wasm.CustomSection {
	indices := make([]uint32, 0, len(hints))
	for idx := range hints {
		indices = append(indices, idx)
	}
	sortU32(indices)

	var payload []byte
	payload = encode.AppendU32(payload, uint32(len(hints)))
	for _, idx := range indices {
		name := hints[idx]
		payload = encode.AppendU32(payload, idx)
		payload = appendWasmString(payload, name)
	}

	var section []byte
	section = append(section, 1) // subsection id: function names
	section = encode.AppendU32(section, uint32(len(payload)))
	section = append(section, payload...)

	return wasm.CustomSection{
		Name: "name",
		Data: section,
	}
}

func appendWasmString(dst []byte, s string) []byte {
	dst = encode.AppendU32(dst, uint32(len(s)))
	return append(dst, s...)
}

func sortU32(s []uint32) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}