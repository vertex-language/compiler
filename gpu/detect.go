package gpu

import (
	"fmt"
	"strings"

	"github.com/vertex-language/compiler/wasm"
)

// Vendor identifies which GPU backend a kernel targets.
type Vendor string

const (
	VendorCUDA   Vendor = "cuda"
	VendorVulkan Vendor = "vulkan"
	VendorMetal  Vendor = "metal"
)

// KernelSource records how a kernel was detected.
type KernelSource uint8

const (
	SourceExport      KernelSource = iota // @vendor suffix on an export entry
	SourceNameSection                     // @vendor suffix in the wasm name custom section
)

// KernelInfo describes a single GPU kernel function found in the module.
type KernelInfo struct {
	FuncIdx uint32       // absolute wasm function index (imports count toward this)
	Name    string       // function name with @vendor[:types] suffix stripped
	Vendor  Vendor       // target GPU vendor
	Params  []bool       // ptr mask: Params[i] true → param i is a linear-memory pointer
	Source  KernelSource // how the kernel was detected
}

// Detect scans m for GPU kernel functions, returning one KernelInfo per kernel.
//
// Detection order:
//  1. Export names carrying a @vendor suffix — highest priority.
//  2. Wasm "name" custom section entries carrying a @vendor suffix.
//
// A function that appears in both is recorded once (from the export entry).
// An error is returned for conflicting annotations or out-of-range indices.
func Detect(m *wasm.Module) ([]KernelInfo, error) {
	var kernels []KernelInfo
	seen := make(map[uint32]bool)

	// Pass 1: export names.
	for _, e := range m.Exports.Entries {
		if e.Kind != wasm.ExportFunc {
			continue
		}
		v, params, ok := parseVendorSuffix(e.Name)
		if !ok {
			continue
		}
		seen[e.Idx] = true
		kernels = append(kernels, KernelInfo{
			FuncIdx: e.Idx,
			Name:    stripSuffix(e.Name),
			Vendor:  v,
			Params:  params,
			Source:  SourceExport,
		})
	}

	// Pass 2: name custom section (catches non-exported kernels).
	for _, entry := range parseFuncNamesFromModule(m) {
		if seen[entry.idx] {
			continue
		}
		v, params, ok := parseVendorSuffix(entry.name)
		if !ok {
			continue
		}
		seen[entry.idx] = true
		kernels = append(kernels, KernelInfo{
			FuncIdx: entry.idx,
			Name:    stripSuffix(entry.name),
			Vendor:  v,
			Params:  params,
			Source:  SourceNameSection,
		})
	}

	return validate(m, kernels)
}

// KernelSet returns the set of absolute function indices that are GPU kernels.
// Passed to the CPU compiler so it knows which function bodies to skip.
func KernelSet(kernels []KernelInfo) map[uint32]bool {
	s := make(map[uint32]bool, len(kernels))
	for _, k := range kernels {
		s[k.FuncIdx] = true
	}
	return s
}

// parseVendorSuffix splits a name on '@' and returns the vendor and optional
// ptr mask decoded from the colon-separated type list.
//
//	"warpReduce@cuda"              → VendorCUDA,   nil,               true
//	"vectorAdd@cuda:ptr.f32.i32"   → VendorCUDA,   [true,false,false], true
//	"histogram@vulkan:ptr.i32"     → VendorVulkan, [true,false],       true
//	"tileConv@metal"               → VendorMetal,  nil,               true
//	"normalFunction"               → "",            nil,               false
func parseVendorSuffix(name string) (Vendor, []bool, bool) {
	at := strings.IndexByte(name, '@')
	if at == -1 {
		return "", nil, false
	}
	rest := name[at+1:]

	colon := strings.IndexByte(rest, ':')
	var vendorStr, typeStr string
	if colon == -1 {
		vendorStr = rest
	} else {
		vendorStr = rest[:colon]
		typeStr = rest[colon+1:]
	}

	var v Vendor
	switch Vendor(vendorStr) {
	case VendorCUDA:
		v = VendorCUDA
	case VendorVulkan:
		v = VendorVulkan
	case VendorMetal:
		v = VendorMetal
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
	return v, params, true
}

// stripSuffix removes the @vendor[:types] portion from name.
func stripSuffix(name string) string {
	at := strings.IndexByte(name, '@')
	if at == -1 {
		return name
	}
	return name[:at]
}

// validate checks for conflicting annotations and out-of-range/import indices.
func validate(m *wasm.Module, kernels []KernelInfo) ([]KernelInfo, error) {
	numFuncs := uint32(m.Imports.NumFuncs()) + uint32(m.Functions.Len())
	numImports := m.Imports.NumFuncs()

	// Detect same funcIdx with conflicting vendors.
	byIdx := make(map[uint32]KernelInfo, len(kernels))
	for _, k := range kernels {
		if prev, dup := byIdx[k.FuncIdx]; dup {
			if prev.Vendor != k.Vendor {
				return nil, fmt.Errorf(
					"gpu: mixed @vendor on function index %d: @%s (from %s) vs @%s (from %s)",
					k.FuncIdx, prev.Vendor, sourceLabel(prev.Source),
					k.Vendor, sourceLabel(k.Source),
				)
			}
			continue
		}
		byIdx[k.FuncIdx] = k
	}

	for _, k := range kernels {
		if k.FuncIdx >= numFuncs {
			return nil, fmt.Errorf(
				"gpu: @%s suffix references function index %d, but module only has %d functions",
				k.Vendor, k.FuncIdx, numFuncs,
			)
		}
		if k.FuncIdx < numImports {
			return nil, fmt.Errorf(
				"gpu: @vendor suffix on import not allowed"+
					" (function index %d, name %q — @vendor is only valid on locally-defined functions)",
				k.FuncIdx, k.Name,
			)
		}
	}

	return kernels, nil
}

func sourceLabel(s KernelSource) string {
	if s == SourceExport {
		return "export"
	}
	return "name section"
}