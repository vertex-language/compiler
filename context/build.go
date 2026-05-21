package context

import (
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// BuildContext flows through the entire compiler pipeline.
// Targets write their machine code and symbols directly into Obj.
type BuildContext struct {
	Module *wasm.Module
	Obj    *object.WasmObj

	// ImportPtrMasks maps a WebAssembly import function index to a boolean mask.
	// True means the parameter is a 'ptr' and requires native memory translation.
	// e.g., "write@i32.ptr.i32" -> [false, true, false]
	ImportPtrMasks map[int][]bool

	// KernelParams maps a routed Wasm function index to its explicit parameter types.
	// e.g., "@cuda:ptr.ptr.i32" -> ["ptr", "ptr", "i32"]
	KernelParams map[int][]string

	// NeedsMemory flags whether the module imports any "memory" primitives,
	// signaling the driver to inject the allocator stubs.
	NeedsMemory bool

	// Concurrency flags to trigger platform stub generation.
	NeedsAsync   bool
	NeedsThread  bool
	NeedsProcess bool

	// ConcurrentFuncs maps a function index to its concurrency kind ("async", "thread", "process").
	// The CPU backend uses this to know it should compile the body under the 
	// internal "__vertex_fn_X" symbol rather than exporting it normally.
	ConcurrentFuncs map[int]string
}

// NewBuildContext initializes a fresh compilation session.
func NewBuildContext(m *wasm.Module) *BuildContext {
	return &BuildContext{
		Module:          m,
		Obj:             &object.WasmObj{},
		ImportPtrMasks:  make(map[int][]bool),
		KernelParams:    make(map[int][]string),
		ConcurrentFuncs: make(map[int]string),
	}
}