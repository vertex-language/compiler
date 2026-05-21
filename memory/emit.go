package memory

import "github.com/vertex-language/compiler/object"

// CompileResult holds the compiled allocator stubs and state block.
type CompileResult struct {
	Obj *object.WasmObj
}

// Compile generates all allocator stubs and returns a WasmObj to be merged
// with the CPU object before linking. The returned object contains:
//
//	.data  — __vertex_alloc_state (StateSize zeroed bytes)
//	.text  — __vertex_memory_init + all stub functions
func Compile() (*CompileResult, error) {
	e := newEmitter()

	// Allocator state block: one zeroed page, RIP-addressed by every stub.
	e.dataLabel("__vertex_alloc_state")
	e.dataZero(StateSize)

	// Init stub — called lazily on the first allocation.
	emitInit(e)

	// Heap stubs.
	emitHeapAlloc(e)
	emitHeapAllocRaw(e)
	emitHeapAllocAligned(e)
	emitHeapFree(e)
	emitHeapRealloc(e)

	// Reference-counting stubs.
	emitRefAlloc(e)
	emitRefAllocWeak(e)
	emitRefRetain(e)
	emitRefRelease(e)
	emitRefSetDtor(e)
	emitRefWeak(e)
	emitRefUpgrade(e)

	// Arena stubs.
	emitArenaPush(e)
	emitArenaPop(e)
	emitArenaAlloc(e)

	return &CompileResult{Obj: e.obj()}, nil
}