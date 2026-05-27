// cpu/arm64/memory/emit.go
package memory

import (
	"github.com/vertex-language/compiler/context"
)

// Emit generates all allocator stubs for the arm64 target and writes them
// into the shared build context's object. Must be called before the CPU
// backend's Emit so that __wasm_mem_base and __vertex_alloc_state are
// defined before any compiled wasm function references them.
func Emit(ctx *context.BuildContext) error {
	e := newEmitter(ctx.Obj, ctx.StaticDataSize)

	// ── Data ──────────────────────────────────────────────────────────────────
	e.dataLabel("__vertex_alloc_state")
	e.dataZero(StateSize)

	e.dataLabel("__wasm_mem_base")
	e.dataZero(8)

	// ── Text ──────────────────────────────────────────────────────────────────
	emitInit(e)

	emitHeapAlloc(e)
	emitHeapAllocRaw(e)
	emitHeapAllocAligned(e)
	emitHeapFree(e)
	emitHeapRealloc(e)

	emitRefAlloc(e)
	emitRefAllocWeak(e)
	emitRefRetain(e)
	emitRefRelease(e)
	emitRefSetDtor(e)
	emitRefWeak(e)
	emitRefUpgrade(e)

	emitArenaPush(e)
	emitArenaPop(e)
	emitArenaAlloc(e)

	return e.flush()
}