package compiler

import (
	"runtime"

	"github.com/vertex-language/compiler/cpu/x86_64"
	"github.com/vertex-language/compiler/driver"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Options controls code generation behaviour.
type Options struct {
	// QualifiedSymbols includes the wasm import module name in every linker
	// symbol, producing "c::malloc" instead of "malloc".
	QualifiedSymbols bool
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

// CompileFor orchestrates the pipeline, routing functions to their proper backends.
func CompileFor(m *wasm.Module, arch string, opts Options) (*object.WasmObj, error) {
	
	// 1. Initialize the compilation driver
	drv := driver.New()

	// 2. Register available CPU targets based on architecture
	switch arch {
	case "amd64":
		drv.Register(&x86_64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	// case "arm64":
	// 	drv.Register(&arm64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	}

	// Note: You will also register your GPU targets here once they are 
	// updated to implement the driver.Target interface.
	// drv.Register(&ptx.Target{})
	// drv.Register(&spirv.Target{})
	// drv.Register(&msl.Target{})

	// 3. Execute the pipeline
	return drv.Compile(m, arch)
}