package concurrency

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitProcessSpawn emits __vertex_process_spawn(func_native_ptr i32) → handle i32.
//
// Forks a child process that calls the given function and exits with its
// return value.  The parent receives a wasm handle containing the child PID.
//
// The handle is a plain heap.alloc (not ref-counted): the caller owns it and
// must call process.wait exactly once.
//
// Register allocation:
//
//	R13 = func_native_ptr
//	R12 = handle wasm ptr
//	RBX = native_handle_ptr (both parent and child have this after fork)
func emitProcessSpawn(e *emitter) {
	e.codeLabel("__vertex_process_spawn")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

	e.MovRR32(asm.R13, asm.RDI) // save func ptr

	// ── Allocate ProcessHandle ────────────────────────────────────────────────
	e.MovRI32(asm.RDI, ProcessHandleSize)
	e.callSym("__vertex_memory_heap_alloc") // returns zeroed block
	e.MovRR32(asm.R12, asm.RAX)

	// ── RBX = native_handle_ptr ───────────────────────────────────────────────
	e.MovRR32(asm.RBX, asm.R12)
	e.AddRR(asm.RBX, asm.R15)

	// Store func ptr so the child can read it from its COW copy.
	e.StoreMem64(asm.RBX, ProcFuncNative, asm.R13)

	// ── fork ──────────────────────────────────────────────────────────────────
	// Parent: RAX = child PID (> 0)
	// Child:  RAX = 0
	// R15 is valid in both (COW copy of the same wasm linear-memory mapping).
	e.SysFork()
	e.TestRR64(asm.RAX)
	isChild := e.JzRel32()

	// ═══════════════════════════════ PARENT PATH ══════════════════════════════
	e.StoreMem64(asm.RBX, ProcPID, asm.RAX) // store child PID
	e.MovRR32(asm.RAX, asm.R12)             // return wasm handle ptr
	done := e.JmpRel32()

	// ═══════════════════════════════ CHILD PATH ════════════════════════════════
	// RBX = native_handle_ptr (COW copy — safe to read ProcFuncNative).
	// R15 = wasm base (COW — same effective address).
	e.Patch32(isChild, e.Pos())

	e.LoadMem64(asm.RAX, asm.RBX, ProcFuncNative)
	e.CallReg(asm.RAX) // RAX = function return value

	// Exit with the function's return value.
	e.MovRR32(asm.RDI, asm.RAX) // rdi = exit code
	e.MovRI32(asm.RAX, 231)     // SYS_exit_group
	e.Syscall()
	e.UD2() // unreachable

	// ── Epilogue (parent only) ────────────────────────────────────────────────
	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)
}

// emitProcessWait emits __vertex_process_wait(handle i32) → i32.
//
// Blocks until the child exits, stores the raw wait4 status in the handle,
// and returns WEXITSTATUS (status >> 8).  A second call returns the cached
// result without another wait4.
func emitProcessWait(e *emitter) {
	e.codeLabel("__vertex_process_wait")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR32(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15) // rbx = native_handle_ptr

	// Return cached result if already waited.
	e.LoadMem32ZX(asm.RAX, asm.RBX, ProcWaited)
	e.TestRR64(asm.RAX)
	alreadyDone := e.JnzRel32()

	// wait4(pid, &ProcRawStatus, 0, NULL)
	e.LoadMem64(asm.RDI, asm.RBX, ProcPID)
	e.LeaRegDisp(asm.RSI, asm.RBX, ProcRawStatus)
	e.XorRR32(asm.RDX)  // options = 0
	e.XorRR32(asm.R10)  // rusage  = NULL
	e.SysWait4()

	// Mark as waited.
	e.StoreMem32Imm(asm.RBX, ProcWaited, 1)

	e.Patch32(alreadyDone, e.Pos())

	// WEXITSTATUS(status) = (status >> 8) & 0xFF.
	// In v1 the &0xFF mask is omitted (exit codes are 0–255 in practice).
	e.LoadMem32ZX(asm.RAX, asm.RBX, ProcRawStatus)
	e.ShrRI32(asm.RAX, 8)

	e.Epilogue([]int{asm.RBX}, align)
}

// emitProcessPid emits __vertex_process_pid(handle i32) → i32.
// Reads ProcPID (stored as int64; PIDs are 32-bit on Linux).  Leaf — no frame.
func emitProcessPid(e *emitter) {
	e.codeLabel("__vertex_process_pid")
	e.AddRR(asm.RDI, asm.R15)
	e.LoadMem32ZX(asm.RAX, asm.RDI, ProcPID)
	e.Ret()
}

// emitProcessExit emits __vertex_process_exit(code i32).
//
// Terminates the entire process via exit_group(2).  Valid from both the main
// process and any child spawned by process.spawn.  Leaf — no frame.
func emitProcessExit(e *emitter) {
	e.codeLabel("__vertex_process_exit")
	// RDI = exit_code (wasm i32 argument, already zero-extended).
	e.MovRI32(asm.RAX, 231) // SYS_exit_group
	e.Syscall()
}