package async

import (
	"fmt"

	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Compile transforms a @async-marked function into a stackful coroutine.
//
// Output placed in the returned WasmObj:
//
//	.text — coroutine entry trampoline
//	        suspend stub  (coro.suspend → saves context, returns to caller)
//	        resume stub   (coro.resume  → restores context, re-enters body)
//	        stack-alloc stub (mmap on first spawn)
//
// The original function body is compiled by the CPU backend (x86_64.Compile)
// with a transformed entry that initialises the coroutine context block.
// TODO: wire body transformation through cpu/x86_64.
func Compile(m *wasm.Module, f FuncInfo, opts CompileOptions) (*object.WasmObj, error) {
	if opts.StackSize == 0 {
		opts.StackSize = DefaultStackSize
	}

	obj := &object.WasmObj{}

	if err := emitStackAllocStub(obj, f, opts.StackSize); err != nil {
		return nil, fmt.Errorf("async: %s: stack alloc: %w", f.Name, err)
	}
	if err := emitSuspendStub(obj, f); err != nil {
		return nil, fmt.Errorf("async: %s: suspend stub: %w", f.Name, err)
	}
	if err := emitResumeStub(obj, f); err != nil {
		return nil, fmt.Errorf("async: %s: resume stub: %w", f.Name, err)
	}

	return obj, nil
}