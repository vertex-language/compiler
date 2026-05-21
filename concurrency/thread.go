package concurrency

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitThreadSpawn emits __vertex_thread_spawn(func_native_ptr i32) → handle i32.
//
// func_native_ptr is the low 32 bits of the thread-body's native code address.
//
// Steps:
//  1. memory.ref.alloc(ThreadHandleSize) → wasm handle
//  2. mmap(ThreadStackSize + ThreadGuardSize)
//  3. mprotect(stack_base, ThreadGuardSize, PROT_NONE)
//  4. Populate ThreadHandle (func ptr, stack base/len; TID/ExitCode zeroed)
//  5. clone(CloneFlags, stack_top, ptid=NULL, ctid=&ThreadTID, tls=0)
//     — parent: inline-store dtor, return handle
//     — child:  call function, store exit code, SYS_exit
//     CLONE_CHILD_CLEARTID causes the kernel to zero ThreadTID and wake
//     the futex when the child calls SYS_exit, unblocking thread.join.
//
// Register allocation:
//
//	R13 = func_native_ptr
//	R12 = handle wasm ptr
//	R14 = stack_base (native)
//	RBX = native_handle_ptr (set before clone; inherited by child)
func emitThreadSpawn(e *emitter) {
	e.codeLabel("__vertex_thread_spawn")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13, asm.R14})

	e.MovRR32(asm.R13, asm.RDI) // save func ptr

	// ── 1. Allocate ThreadHandle ──────────────────────────────────────────────
	e.MovRI32(asm.RDI, ThreadHandleSize)
	e.callSym("__vertex_memory_ref_alloc")
	e.MovRR32(asm.R12, asm.RAX)

	// ── 2. mmap thread stack ──────────────────────────────────────────────────
	e.MovRI32(asm.RSI, ThreadStackSize+ThreadGuardSize)
	e.MmapAnon()
	failMmap := e.CheckMmapError()
	e.MovRR(asm.R14, asm.RAX)

	// ── 3. Guard page ─────────────────────────────────────────────────────────
	e.MovRR(asm.RDI, asm.R14)
	e.MovRI32(asm.RSI, ThreadGuardSize)
	e.XorRR32(asm.RDX)
	e.SysMprotect()

	// ── RBX = native_handle_ptr (set before clone, shared with child) ─────────
	e.MovRR32(asm.RBX, asm.R12)
	e.AddRR(asm.RBX, asm.R15)

	// ── 4. Populate ThreadHandle ──────────────────────────────────────────────
	// ThreadTID    = 0  (zeroed by ref.alloc; kernel writes real TID via SETTID)
	// ThreadExitCode = 0  (zeroed; written by child before exit)
	// ThreadDetached = 0  (zeroed)
	e.StoreMem64(asm.RBX, ThreadFuncNative, asm.R13)
	e.StoreMem64(asm.RBX, ThreadStackBase, asm.R14)
	e.StoreMem64Imm(asm.RBX, ThreadStackLen, int32(ThreadGuardSize+ThreadStackSize))

	// ── 5. clone ──────────────────────────────────────────────────────────────
	e.MovRI32(asm.RDI, CloneFlags)
	// RSI = child_stack_top
	e.MovRR(asm.RSI, asm.R14)
	e.AddRI(asm.RSI, ThreadGuardSize+ThreadStackSize)
	// RDX = ptid = NULL
	e.XorRR32(asm.RDX)
	// R10 = ctid = &ThreadTID = native_handle_ptr  (ThreadTID is at offset 0)
	e.MovRR(asm.R10, asm.RBX)
	// R8 = tls = 0
	e.XorRR32(asm.R8)

	e.SysClone()
	// RAX = child TID (parent) or 0 (child)
	e.TestRR64(asm.RAX)
	isChild := e.JzRel32()

	// ═══════════════════════════════ PARENT PATH ══════════════════════════════

	// Inline-store the 64-bit destructor pointer in the RC header.
	e.leaRIPSym(asm.RAX, "__vertex_thread_dtor")
	e.StoreMem64(asm.RBX, rcDtorOffset, asm.RAX)

	// Return wasm handle ptr.
	e.MovRR32(asm.RAX, asm.R12)
	done := e.JmpRel32()

	// ═══════════════════════════════ CHILD PATH ════════════════════════════════
	// New thread stack; RBX = native_handle_ptr; R15 = wasm base (both inherited).
	e.Patch32(isChild, e.Pos())

	e.LoadMem64(asm.RAX, asm.RBX, ThreadFuncNative)
	e.CallReg(asm.RAX) // RAX = thread function return value

	// Store exit code before the kernel clears ThreadTID on SYS_exit.
	// x86-64 TSO guarantees this store is globally visible before the kernel
	// processes the syscall, so thread.join always reads the correct value.
	e.StoreMem64(asm.RBX, ThreadExitCode, asm.RAX)

	// Exit the thread.  CLONE_CHILD_CLEARTID zeroes ThreadTID and issues a
	// futex wake, unblocking any thread.join caller.
	e.XorRR32(asm.RDI)   // exit code 0 (the wasm return value lives in ThreadExitCode)
	e.SysExitThread()     // never returns

	// ── Epilogue (parent only) ────────────────────────────────────────────────
	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13, asm.R14}, align)

	// ── Fatal: mmap failed ────────────────────────────────────────────────────
	e.Patch32(failMmap, e.Pos())
	e.ExitGroup(127)
}

// emitThreadJoin emits __vertex_thread_join(handle i32) → i32.
//
// Blocks until the thread exits, then returns its exit code.
// Uses futex(FUTEX_WAIT) on ThreadTID, which the kernel zeros via
// CLONE_CHILD_CLEARTID on thread exit (see emitThreadSpawn).
func emitThreadJoin(e *emitter) {
	e.codeLabel("__vertex_thread_join")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR32(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15) // rbx = native_handle_ptr

	// ── Futex wait loop ───────────────────────────────────────────────────────
	// Spin until ThreadTID == 0 (cleared by kernel on exit).
	waitTop := e.Pos()
	e.LoadMem32ZX(asm.RAX, asm.RBX, ThreadTID) // eax = current TID (or 0)
	e.TestRR64(asm.RAX)
	threadDone := e.JzRel32() // TID == 0 → already exited

	// futex(FUTEX_WAIT, &ThreadTID, expected_tid, NULL, NULL, 0)
	// Sleeps atomically if [uaddr] == val, wakes when the kernel clears it.
	e.MovRR(asm.RDI, asm.RBX)        // rdi = &ThreadTID (offset 0 ⇒ just rbx)
	e.MovRI32(asm.RSI, FutexWait)    // op  = FUTEX_WAIT
	e.MovRR32(asm.RDX, asm.RAX)      // val = expected TID (current value)
	e.XorRR32(asm.R10)               // timeout = NULL
	e.XorRR32(asm.R8)                // uaddr2  = NULL
	e.XorRR32(asm.R9)                // val3    = 0
	e.SysFutex()
	// Loop back: EINTR and EAGAIN are both safe to retry.
	e.JmpRel32Back(waitTop)

	e.Patch32(threadDone, e.Pos())

	// Return exit code (written by the child before SYS_exit).
	e.LoadMem32ZX(asm.RAX, asm.RBX, ThreadExitCode)

	e.Epilogue([]int{asm.RBX}, align)
}

// emitThreadDetach emits __vertex_thread_detach(handle i32).
// Sets ThreadDetached = 1.  The destructor uses this flag to decide whether
// to attempt to unmap the stack.  Leaf — no frame.
func emitThreadDetach(e *emitter) {
	e.codeLabel("__vertex_thread_detach")
	e.AddRR(asm.RDI, asm.R15)
	e.StoreMem32Imm(asm.RDI, ThreadDetached, 1)
	e.Ret()
}

// emitThreadSelf emits __vertex_thread_self() → i32.
// Returns the calling thread's TID via gettid(2).  Leaf — no frame.
func emitThreadSelf(e *emitter) {
	e.codeLabel("__vertex_thread_self")
	e.SysGettid() // result in RAX
	e.Ret()
}

// emitThreadExit emits __vertex_thread_exit(code i32).
//
// Terminates the calling thread via SYS_exit.  CLONE_CHILD_CLEARTID causes
// the kernel to zero ThreadTID and wake the futex, so thread.join unblocks.
//
// The exit code propagated to thread.join is the value stored in ThreadExitCode
// by the thread-entry path (the thread body's normal return).  thread.exit
// bypasses that store, so join will return 0.  Leaf — no frame.
func emitThreadExit(e *emitter) {
	e.codeLabel("__vertex_thread_exit")
	// RDI = exit_code from wasm caller; not used by join (see note above).
	e.SysExitThread()
}

// emitThreadDtor emits __vertex_thread_dtor(wasm_ptr i32).
//
// Called by memory.ref.release when the strong count reaches zero.
// Unmaps the thread stack only if the thread has already exited
// (ThreadTID == 0).  If the thread is still running (detached and not yet
// done), the stack is leaked in v1 — a future revision can set a "free on
// exit" flag the thread checks immediately before SYS_exit.
// Leaf — no frame.
func emitThreadDtor(e *emitter) {
	e.codeLabel("__vertex_thread_dtor")
	e.AddRR(asm.RDI, asm.R15) // native_handle_ptr

	// Only munmap if the thread has exited.
	e.LoadMem32ZX(asm.RAX, asm.RDI, ThreadTID)
	e.TestRR64(asm.RAX)
	stillRunning := e.JnzRel32()

	e.LoadMem64(asm.RSI, asm.RDI, ThreadStackLen)  // rsi = len
	e.LoadMem64(asm.RDI, asm.RDI, ThreadStackBase) // rdi = base (OK: RSI loaded first)
	e.SysMunmap()

	e.Patch32(stillRunning, e.Pos())
	e.Ret()
}