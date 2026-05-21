// Package async compiles @async-marked wasm functions into stackful coroutines.
//
// Each coroutine gets its own stack allocated via mmap. A 64-byte Context
// struct saves the registers that must survive a suspend/resume cycle.
// R15 (the linear memory base) is always included — its value is identical
// for every coroutine in the module since they share one linear memory.
//
// Context layout (saved on suspend, restored on resume):
//
//	+0   rsp  — stack pointer of the suspended coroutine
//	+8   rbp  — frame pointer
//	+16  rbx  — callee-saved (SysV)
//	+24  r12  — callee-saved (SysV)
//	+32  r13  — callee-saved (SysV)
//	+40  r14  — callee-saved (SysV)
//	+48  r15  — linear memory base (MemBase — must survive context switch)
//	+56  rip  — saved return address (pushed by the suspend trampoline)
//
// Total: 64 bytes per context.
package async

import "github.com/vertex-language/compiler/object"

// DefaultStackSize is the default coroutine stack size: 128 KB.
// Must be a multiple of the system page size (4096).
const DefaultStackSize = 128 * 1024

// ContextSize is the size in bytes of the saved register context.
const ContextSize = 64

// Context field offsets within the 64-byte context block.
const (
	CtxRSP = 0
	CtxRBP = 8
	CtxRBX = 16
	CtxR12 = 24
	CtxR13 = 32
	CtxR14 = 40
	CtxR15 = 48 // MemBase
	CtxRIP = 56
)

// FuncInfo describes a single @async function to be compiled.
type FuncInfo struct {
	FuncIdx uint32
	Name    string
	Params  []bool // ptr mask
}

// CompileOptions controls async compilation behaviour.
type CompileOptions struct {
	StackSize int // 0 → DefaultStackSize
}

// CompileResult holds the compiled coroutine artifacts.
type CompileResult = object.WasmObj