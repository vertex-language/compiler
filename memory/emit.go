package memory

import "github.com/vertex-language/compiler/context"

// Emit generates all allocator stubs for x86_64 and appends them
// directly into the shared compilation context's WasmObj.
func Emit(ctx *context.BuildContext) error {
	e := newEmitter(ctx.Obj)

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

	// Flush all accumulated code, data, symbols, and relocs into the shared obj
	e.flush()

	return nil
}