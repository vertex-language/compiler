package memory

import "github.com/vertex-language/compiler/context"

// Emit generates all allocator stubs for x86_64 and appends them
// directly into the shared compilation context's WasmObj.
func Emit(ctx *context.BuildContext) error {
	e := newEmitter(ctx.Obj)

	// Allocator state block: one zeroed page, RIP-addressed by every stub.
	e.dataLabel("__vertex_alloc_state")
	e.dataZero(StateSize)

	// Native base address of the wasm address space (= R15 at runtime).
	// Written by __vertex_memory_init; read by every compiled function prologue.
	// Zero until the first wasm function runs and triggers lazy init.
	e.dataLabel("__wasm_mem_base")
	e.dataZero(8)

	// Number of bytes to copy from the .data static region into the freshly
	// mmap'd wasm address space during init.  Written by the x86_64 backend's
	// emitDataSegments after it knows the exact data-segment footprint.
	e.dataLabel("__wasm_static_bytes")
	e.dataZero(4)

	// Init stub — sets up the wasm address space and heap/arena on first call.
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

	e.flush()
	return nil
}