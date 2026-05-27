package memory

import (
	"github.com/vertex-language/compiler/context"
)

func Emit(ctx *context.BuildContext) error {
	e := newEmitter(ctx.Obj, ctx.StaticDataSize)

	e.dataLabel("__vertex_alloc_state")
	e.dataZero(StateSize)

	e.dataLabel("__wasm_mem_base")
	e.dataZero(8)

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