// context/build.go
package context

import (
	"github.com/vertex-language/compiler/abi"
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// BuildContext flows through the entire compiler pipeline.
type BuildContext struct {
	Module *wasm.Module
	Obj    object.Object

	// StaticDataSize is the number of wasm linear-memory bytes covered by
	// active data segments. Baked into __vertex_memory_init as an immediate
	// at compile time — no runtime symbol load needed.
	StaticDataSize uint32

	// ImportPtrMasks maps a wasm import function index to a per-parameter
	// boolean mask. True means the parameter is a linear-memory 'ptr' and
	// needs native-address translation before the call.
	ImportPtrMasks map[int][]bool

	// ImportHptrMasks maps an import to its opaque native-handle parameters.
	ImportHptrMasks map[int][]bool

	// ReturnHptrMasks flags imports whose return value is a native handle
	// that must be interned in the Handle Table before wasm sees it.
	ReturnHptrMasks map[int]bool

	// KernelParams maps a routed wasm function index to its explicit
	// parameter type annotations from the export suffix.
	KernelParams map[int][]string

	// SystemLibs maps a wasm import function index to its resolved system
	// library entry. Only populated for imports whose RouteKind IsSystemLib().
	// The driver uses this to emit DT_NEEDED (ELF), LC_LOAD_DYLIB (Mach-O),
	// or the PE import lib path (COFF) for the call site.
	SystemLibs map[int]abi.SystemLibResult

	// NeedsMemory flags whether the module imports any "memory" primitives,
	// signalling the driver to inject the allocator stubs.
	NeedsMemory bool
}

// NewBuildContext initialises a fresh compilation session for module m,
// writing into obj. StaticDataSize is computed eagerly so every downstream
// pass (including memory.Emit) can use it immediately.
func NewBuildContext(m *wasm.Module, obj object.Object) *BuildContext {
	return &BuildContext{
		Module:          m,
		Obj:             obj,
		StaticDataSize:  computeStaticDataSize(m),
		ImportPtrMasks:  make(map[int][]bool),
		ImportHptrMasks: make(map[int][]bool),
		ReturnHptrMasks: make(map[int]bool),
		KernelParams:    make(map[int][]string),
		SystemLibs:      make(map[int]abi.SystemLibResult),
	}
}

// computeStaticDataSize scans active data segments and returns the
// high-water mark of initialised wasm linear-memory bytes relative to
// offset 0. This is the byte count that __vertex_memory_init copies from
// the compiled-in .data region into the freshly mmap'd wasm address space.
func computeStaticDataSize(m *wasm.Module) uint32 {
	var max uint32
	for _, d := range m.Datas.Entries {
		active, ok := d.Mode.(wasm.DataModeActive)
		if !ok {
			continue
		}
		b := active.Offset.Bytes()
		if len(b) == 0 || b[0] != 0x41 { // must be i32.const
			continue
		}
		off, err := decode.NewReader(b[1:]).ReadS32()
		if err != nil || off < 0 {
			continue
		}
		if end := uint32(off) + uint32(len(d.Data)); end > max {
			max = end
		}
	}
	return max
}