package concurrency

import (
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/wasm"
)

// Kind identifies which concurrency model a function targets.
type Kind string

const (
	KindAsync   Kind = "async"
	KindThread  Kind = "thread"
	KindProcess Kind = "process"
)

// FuncSource records how a concurrent function was detected.
type FuncSource uint8

const (
	SourceExport      FuncSource = iota // @kind suffix on an export entry
	SourceNameSection                   // @kind suffix in the wasm name custom section
)

// FuncInfo describes a single concurrent function found in the module.
type FuncInfo struct {
	FuncIdx uint32     // absolute wasm function index (imports count toward this)
	Name    string     // function name with @kind[:types] suffix stripped
	Kind    Kind       // target concurrency model
	Params  []bool     // ptr mask: Params[i] true → param i is a linear-memory pointer
	Source  FuncSource // how the function was detected
}

// Detect scans m for functions annotated with a @kind suffix, returning one
// FuncInfo per function found.
//
// Detection order:
//  1. Export names carrying a @kind suffix — highest priority.
//  2. Wasm "name" custom section entries carrying a @kind suffix.
//
// A function that appears in both is recorded once from the export entry.
// An error is returned for conflicting annotations or out-of-range indices.
func Detect(m *wasm.Module) ([]FuncInfo, error) {
	var funcs []FuncInfo
	seen := make(map[uint32]bool)

	// Pass 1: export names.
	for _, e := range m.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		k, params, ok := parseKindSuffix(e.Name)
		if !ok {
			continue
		}
		seen[e.Idx] = true
		funcs = append(funcs, FuncInfo{
			FuncIdx: e.Idx,
			Name:    stripSuffix(e.Name),
			Kind:    k,
			Params:  params,
			Source:  SourceExport,
		})
	}

	// Pass 2: name custom section (catches non-exported functions).
	for _, entry := range parseFuncNamesFromModule(m) {
		if seen[entry.idx] {
			continue
		}
		k, params, ok := parseKindSuffix(entry.name)
		if !ok {
			continue
		}
		seen[entry.idx] = true
		funcs = append(funcs, FuncInfo{
			FuncIdx: entry.idx,
			Name:    stripSuffix(entry.name),
			Kind:    k,
			Params:  params,
			Source:  SourceNameSection,
		})
	}

	return validate(m, funcs)
}

// FuncSet returns the set of absolute function indices claimed by the
// concurrency package. Passed to the CPU compiler so it skips these bodies.
func FuncSet(funcs []FuncInfo) map[uint32]bool {
	s := make(map[uint32]bool, len(funcs))
	for _, f := range funcs {
		s[f.FuncIdx] = true
	}
	return s
}

// parseKindSuffix splits a name on '@' and returns the kind and optional ptr
// mask decoded from the colon-separated type list.
//
//	"counter@async"              → KindAsync,   nil,                true
//	"worker@thread:ptr.i32"      → KindThread,  [true, false],      true
//	"child@process:i32"          → KindProcess, [false],            true
//	"normalFunction"             → "",           nil,               false
func parseKindSuffix(name string) (Kind, []bool, bool) {
	at := strings.IndexByte(name, '@')
	if at == -1 {
		return "", nil, false
	}
	rest := name[at+1:]

	colon := strings.IndexByte(rest, ':')
	var kindStr, typeStr string
	if colon == -1 {
		kindStr = rest
	} else {
		kindStr = rest[:colon]
		typeStr = rest[colon+1:]
	}

	var k Kind
	switch Kind(kindStr) {
	case KindAsync:
		k = KindAsync
	case KindThread:
		k = KindThread
	case KindProcess:
		k = KindProcess
	default:
		return "", nil, false
	}

	var params []bool
	if typeStr != "" {
		parts := strings.Split(typeStr, ".")
		params = make([]bool, len(parts))
		for i, p := range parts {
			params[i] = p == "ptr"
		}
	}
	return k, params, true
}

// stripSuffix removes the @kind[:types] portion from a name.
func stripSuffix(name string) string {
	at := strings.IndexByte(name, '@')
	if at == -1 {
		return name
	}
	return name[:at]
}

// validate checks for conflicting annotations, import references, and
// out-of-range indices.
func validate(m *wasm.Module, funcs []FuncInfo) ([]FuncInfo, error) {
	numFuncs := uint32(m.Imports.NumFuncs()) + uint32(m.Functions.Len())
	numImports := m.Imports.NumFuncs()

	byIdx := make(map[uint32]FuncInfo, len(funcs))
	for _, f := range funcs {
		if prev, dup := byIdx[f.FuncIdx]; dup {
			if prev.Kind != f.Kind {
				return nil, fmt.Errorf(
					"concurrency: mixed @kind on function index %d: @%s (from %s) vs @%s (from %s)",
					f.FuncIdx, prev.Kind, sourceLabel(prev.Source),
					f.Kind, sourceLabel(f.Source),
				)
			}
			continue
		}
		byIdx[f.FuncIdx] = f
	}

	for _, f := range funcs {
		if f.FuncIdx >= numFuncs {
			return nil, fmt.Errorf(
				"concurrency: @%s suffix references function index %d, "+
					"but module only has %d functions",
				f.Kind, f.FuncIdx, numFuncs,
			)
		}
		if f.FuncIdx < numImports {
			return nil, fmt.Errorf(
				"concurrency: @kind suffix on import not allowed"+
					" (function index %d, name %q — @kind is only valid on locally-defined functions)",
				f.FuncIdx, f.Name,
			)
		}
	}

	return funcs, nil
}

func sourceLabel(s FuncSource) string {
	if s == SourceExport {
		return "export"
	}
	return "name section"
}