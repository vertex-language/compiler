package compiler

import (
	"fmt"
	"runtime"

	"github.com/vertex-language/compiler/cpu/x86_64"
	"github.com/vertex-language/compiler/gpu"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Options controls code generation behaviour.
type Options struct {
	// QualifiedSymbols includes the wasm import module name in every linker
	// symbol, producing "c::malloc" instead of "malloc".
	QualifiedSymbols bool

	// Pointer and type information is encoded directly in the import name
	// using the @ signature format:
	//
	//   "write@i32.ptr.i32"
	//   "open@ptr.i32.i32"
	//   "mmap@ptr.i64.i32.i32.i32.i64"
	//
	// Types: i32  i64  f32  f64  ptr
	// ptr = linear-memory i32 offset that needs R15 translation.
	// No PointerArgs map — the wasm IR is self-describing.
}

// Compile translates m into a native object for the current host architecture.
func Compile(m *wasm.Module) (*object.WasmObj, error) {
	return CompileWith(m, Options{})
}

// CompileWith translates m into a native object for the current host
// architecture using the supplied options.
func CompileWith(m *wasm.Module, opts Options) (*object.WasmObj, error) {
	return CompileFor(m, runtime.GOARCH, opts)
}

// CompileFor translates m into a native object for the named architecture.
// Supported values: "amd64".
//
// GPU kernels — functions whose export name or name-section entry carries a
// @cuda / @vulkan / @metal suffix — are detected first and routed to the gpu
// package. The CPU backend only sees and compiles the remaining functions.
func CompileFor(m *wasm.Module, arch string, opts Options) (*object.WasmObj, error) {
	// ── GPU detection ─────────────────────────────────────────────────────────
	//
	// Must run before the CPU compiler so the CPU backend never attempts to
	// lower a GPU kernel body as x86-64 machine code.

	gpuKernels, err := gpu.Detect(m)
	if err != nil {
		return nil, fmt.Errorf("compiler: gpu detection: %w", err)
	}

	// Build the skip-set that the CPU backend uses to ignore GPU functions.
	gpuSet := gpu.KernelSet(gpuKernels)

	// Compile GPU kernels via the gpu package. Each kernel is dispatched to
	// its vendor backend (cuda → PTX, vulkan → SPIR-V, metal → MSL).
	var gpuResult *gpu.CompileResult
	if len(gpuKernels) > 0 {
		gpuResult, err = gpu.Compile(m, gpuKernels, gpu.CompileOptions{})
		if err != nil {
			return nil, fmt.Errorf("compiler: gpu compilation: %w", err)
		}
	}

	// ── CPU compilation ───────────────────────────────────────────────────────

	switch arch {
	case "amd64":
		cpuObj, err := x86_64.Compile(m, arch, opts.QualifiedSymbols, gpuSet)
		if err != nil {
			return nil, err
		}
		// Merge GPU artifacts (blobs, probe stubs, dispatch stubs) into the
		// CPU object so the linker sees a single unified WasmObj.
		if gpuResult != nil && gpuResult.Obj != nil {
			mergeObjects(cpuObj, gpuResult.Obj)
		}
		return cpuObj, nil

	default:
		return nil, fmt.Errorf("compiler: unsupported architecture %q", arch)
	}
}

// mergeObjects appends the code, data, symbols, and relocations from src into
// dst, adjusting code-section offsets so that symbols and relocations from src
// point to the correct locations after the merge.
//
// GPU data (blobs in .rodata) is appended without offset adjustment because
// the GPU package places each blob into its own named section; the linker
// handles its final placement independently of the flat CPU .data region.
func mergeObjects(dst, src *object.WasmObj) {
	if len(src.Code) == 0 && len(src.Data) == 0 {
		return
	}

	codeBase := len(dst.Code)
	dst.Code = append(dst.Code, src.Code...)

	if len(src.Data) > 0 {
		dst.Data = append(dst.Data, src.Data...)
	}

	// Shift code-section symbol offsets by codeBase so they remain valid
	// after the GPU code is appended after the CPU code.
	for _, sym := range src.Symbols {
		sym.Offset += codeBase
		dst.Symbols = append(dst.Symbols, sym)
	}

	// Shift code-section relocation offsets by codeBase for the same reason.
	for _, rel := range src.Relocs {
		rel.Offset += codeBase
		dst.Relocs = append(dst.Relocs, rel)
	}
}