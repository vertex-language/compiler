package memory

import (
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/wasm"
)

// Detect scans m's import section for imports from the "memory" module,
// validates each against the known signature table, and returns one ImportInfo
// per import found. Returns an error if any import is malformed or unknown.
func Detect(m *wasm.Module) ([]ImportInfo, error) {
	var infos []ImportInfo
	for i, imp := range m.Imports.Entries {
		if imp.Module != ImportModule {
			continue
		}
		if imp.Kind != wasm.ImportFunc {
			return nil, fmt.Errorf("memory import %q: only function imports are supported", imp.Name)
		}
		sub, fn, err := parseName(imp.Name)
		if err != nil {
			return nil, fmt.Errorf("memory import %q: %w", imp.Name, err)
		}
		if !knownImport(sub, fn) {
			return nil, fmt.Errorf("unknown memory import %q", imp.Name)
		}
		if err := checkSignature(m, imp.TypeIdx, sub, fn); err != nil {
			return nil, fmt.Errorf("memory import %q: %w", imp.Name, err)
		}
		infos = append(infos, ImportInfo{
			FuncIdx: uint32(i),
			Sub:     sub,
			Fn:      fn,
			// PtrMask is intentionally left nil in v1. Pointer translation (+ R15)
			// is handled internally by the stubs, so the compiler never needs
			// to insert an external 'add r15' for these primitives.
			Symbol:  SymbolName(sub, fn),
		})
	}
	return infos, nil
}

// parseName splits "sub.fn" into its two parts.
func parseName(name string) (sub, fn string, err error) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format sub.fn")
	}
	return parts[0], parts[1], nil
}

// knownImport reports whether sub.fn is a recognised memory primitive.
func knownImport(sub, fn string) bool {
	switch sub {
	case "heap":
		switch fn {
		case "alloc", "alloc_raw", "alloc_aligned", "free", "realloc":
			return true
		}
	case "ref":
		switch fn {
		case "alloc", "retain", "release", "set_dtor",
			"alloc_weak", "weak", "upgrade":
			return true
		}
	case "arena":
		switch fn {
		case "push", "pop", "alloc":
			return true
		}
	}
	return false
}

// expectedSig describes the wasm type signature for each known import.
// Params and results are wasm ValTypes; I32 = 0x7F.
type sig struct {
	params  []wasm.ValType
	results []wasm.ValType
}

var i32 = wasm.I32

var knownSigs = map[string]sig{
	"heap.alloc":         {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"heap.alloc_raw":     {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"heap.alloc_aligned": {[]wasm.ValType{i32, i32}, []wasm.ValType{i32}},
	"heap.free":          {[]wasm.ValType{i32}, nil},
	"heap.realloc":       {[]wasm.ValType{i32, i32}, []wasm.ValType{i32}},
	"ref.alloc":          {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"ref.retain":         {[]wasm.ValType{i32}, nil},
	"ref.release":        {[]wasm.ValType{i32}, nil},
	"ref.set_dtor":       {[]wasm.ValType{i32, i32}, nil},
	"ref.alloc_weak":     {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"ref.weak":           {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"ref.upgrade":        {[]wasm.ValType{i32}, []wasm.ValType{i32}},
	"arena.push":         {nil, nil},
	"arena.pop":          {nil, nil},
	"arena.alloc":        {[]wasm.ValType{i32}, []wasm.ValType{i32}},
}

// checkSignature verifies that the wasm function type at typeIdx matches the
// expected signature for sub.fn.
func checkSignature(m *wasm.Module, typeIdx uint32, sub, fn string) error {
	key := sub + "." + fn
	want, ok := knownSigs[key]
	if !ok {
		return fmt.Errorf("no signature defined (internal error)")
	}
	if int(typeIdx) >= len(m.Types.Entries) {
		return fmt.Errorf("type index %d out of range", typeIdx)
	}
	got := m.Types.Entries[typeIdx]
	if !valtypesEqual(got.Params, want.params) {
		return fmt.Errorf("param mismatch: got %v, want %v", got.Params, want.params)
	}
	if !valtypesEqual(got.Results, want.results) {
		return fmt.Errorf("result mismatch: got %v, want %v", got.Results, want.results)
	}
	return nil
}

func valtypesEqual(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}