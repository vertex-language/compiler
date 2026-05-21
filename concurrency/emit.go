package concurrency

import "github.com/vertex-language/compiler/context"

// Emit generates native stubs only for the concurrency variants actually flagged
// in the build context. Unused backends produce no code.
//
// The generated machine code, symbols, and relocations are flushed directly 
// into the shared compilation context's WasmObj.
func Emit(ctx *context.BuildContext) error {
	e := newEmitter(ctx.Obj)

	if ctx.NeedsAsync {
		// Core context-switch leaf must be emitted first: spawn/resume/yield
		// all reference it via callSym.
		emitCoroJump(e)
		emitCoroTrampoline(e)
		emitCoroSpawn(e)
		emitCoroResume(e)
		emitCoroYield(e)
		emitCoroDone(e)
		emitCoroResult(e)
		emitCoroDtor(e)
	}

	if ctx.NeedsThread {
		emitThreadSpawn(e)
		emitThreadJoin(e)
		emitThreadDetach(e)
		emitThreadSelf(e)
		emitThreadExit(e)
		emitThreadDtor(e)
	}

	if ctx.NeedsProcess {
		emitProcessSpawn(e)
		emitProcessWait(e)
		emitProcessPid(e)
		emitProcessExit(e)
	}

	// Flush all accumulated code, symbols, and relocs into the shared obj
	e.flush()

	return nil
}