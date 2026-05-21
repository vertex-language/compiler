package gpu

import (
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/wasm"
)

type funcNameEntry struct {
	idx  uint32
	name string
}

// parseFuncNamesFromModule locates the wasm "name" custom section in m and
// extracts function name entries (subsection id 1).
// Returns nil if no such section exists or it cannot be parsed.
func parseFuncNamesFromModule(m *wasm.Module) []funcNameEntry {
	for _, cs := range m.Customs {
		if cs.Name == "name" {
			entries, _ := parseFuncNameSubsection(cs.Data)
			return entries
		}
	}
	return nil
}

// parseFuncNameSubsection parses the binary layout of the wasm "name" custom
// section. The section is a sequence of:
//
//	id:   byte     — subsection kind (1 = function names)
//	size: u32      — byte length of the payload that follows
//	...payload...
//
// For subsection 1, the payload is:
//
//	count: u32
//	(funcIdx: u32, name: u32-length-prefixed UTF-8 string)*
func parseFuncNameSubsection(data []byte) ([]funcNameEntry, error) {
	r := decode.NewReader(data)
	var entries []funcNameEntry

	for !r.EOF() {
		id, err := r.ReadByte()
		if err != nil {
			break
		}
		size, err := r.ReadU32()
		if err != nil {
			break
		}
		
		// FIXED: size is already uint32, removed int() cast
		payload, err := r.ReadFixedBytes(size)
		if err != nil {
			break
		}
		if id != 1 {
			continue // skip module-name (0), local-name (2), etc.
		}

		sub := decode.NewReader(payload)
		count, err := sub.ReadU32()
		if err != nil {
			continue
		}
		for i := uint32(0); i < count; i++ {
			idx, err := sub.ReadU32()
			if err != nil {
				break
			}
			nameLen, err := sub.ReadU32()
			if err != nil {
				break
			}
			
			// FIXED: nameLen is already uint32, removed int() cast
			nameBytes, err := sub.ReadFixedBytes(nameLen)
			if err != nil {
				break
			}
			entries = append(entries, funcNameEntry{idx: idx, name: string(nameBytes)})
		}
	}
	return entries, nil
}