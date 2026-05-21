package gpu

import (
	"fmt"

	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// CompileOptions controls GPU kernel compilation behaviour.
// Extended by individual backends as they are implemented.
type CompileOptions struct{}

// CompileResult holds the compiled GPU artifacts produced for a set of kernels.
// The Obj field carries GPU blobs in .rodata, probe and dispatch stubs in .text,
// and any symbols/relocations the linker needs to wire everything together.
type CompileResult struct {
	Obj     *object.WasmObj
	Kernels []KernelInfo
}

// Compile compiles the GPU kernels in m identified by kernels.
//
// Each kernel is dispatched to the backend that matches its Vendor field:
//
//	VendorCUDA   → gpu/cuda  (PTX, Linux & Windows)
//	VendorVulkan → gpu/spirv (SPIR-V, Linux & Windows)
//	VendorMetal  → gpu/msl   (MSL, macOS only)
//
// Until a backend package is wired in, kernels targeting that vendor return
// an error wrapping ErrBackendNotAvailable. The CPU compilation of the rest
// of the module still proceeds normally.
func Compile(m *wasm.Module, kernels []KernelInfo, opts CompileOptions) (*CompileResult, error) {
	if len(kernels) == 0 {
		return &CompileResult{Obj: &object.WasmObj{}}, nil
	}

	result := &CompileResult{
		Obj:     &object.WasmObj{},
		Kernels: kernels,
	}

	for _, k := range kernels {
		if err := compileKernel(m, k, result); err != nil {
			return nil, fmt.Errorf("gpu: kernel %q (@%s, func %d): %w",
				k.Name, k.Vendor, k.FuncIdx, err)
		}
	}

	return result, nil
}

// ErrBackendNotAvailable is returned when the relevant vendor backend package
// (gpu/cuda, gpu/spirv, gpu/msl) has not yet been implemented.
var ErrBackendNotAvailable = fmt.Errorf("gpu backend not yet available")

// compileKernel dispatches a single kernel to its vendor backend.
func compileKernel(m *wasm.Module, k KernelInfo, result *CompileResult) error {
	switch k.Vendor {
	case VendorCUDA:
		return compileCUDA(m, k, result)
	case VendorVulkan:
		return compileVulkan(m, k, result)
	case VendorMetal:
		return compileMetal(m, k, result)
	default:
		return fmt.Errorf("unknown vendor %q", k.Vendor)
	}
}

// compileCUDA compiles a @cuda kernel to PTX and embeds the blob + driver stubs.
// Delegates to the gpu/cuda package (not yet implemented).
func compileCUDA(_ *wasm.Module, _ KernelInfo, _ *CompileResult) error {
	// TODO: delegate to gpu/cuda package
	return fmt.Errorf("CUDA (PTX) backend: %w", ErrBackendNotAvailable)
}

// compileVulkan compiles a @vulkan kernel to SPIR-V and embeds the blob + Vulkan stubs.
// Delegates to the gpu/spirv package (not yet implemented).
func compileVulkan(_ *wasm.Module, _ KernelInfo, _ *CompileResult) error {
	// TODO: delegate to gpu/spirv package
	return fmt.Errorf("Vulkan (SPIR-V) backend: %w", ErrBackendNotAvailable)
}

// compileMetal compiles a @metal kernel to MSL and embeds the blob + Metal stubs.
// Delegates to the gpu/msl package (not yet implemented).
func compileMetal(_ *wasm.Module, _ KernelInfo, _ *CompileResult) error {
	// TODO: delegate to gpu/msl package
	return fmt.Errorf("Metal (MSL) backend: %w", ErrBackendNotAvailable)
}