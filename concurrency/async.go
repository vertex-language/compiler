package concurrency

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitCoroJump emits __vertex_coro_jump — the symmetric context-switch leaf.
//
// Internal ABI:
//
//	__vertex_coro_jump(save_rsp *int64, load_rsp *int64)
//	  RDI = pointer where the current RSP is saved
//	  RSI = pointer from which the new RSP is loaded
//
// No standard prologue — this IS the low-level switch.  It saves the five
// callee-saved registers (RBX, R12–R14, RBP) by pushing them, saves RSP
// (which now points at the freshly-pushed RBP) into *RDI, loads a new RSP
// from *RSI, pops the restored registers, and returns — landing in the new
// context's call frame.
//
// Stack layout invariant (enforced by emitCoroSpawn for a fresh coroutine):
//
//	[saved_rsp + 0]  RBP  (0 on first entry)
//	[saved_rsp + 8]  R14  (0 on first entry)
//	[saved_rsp + 16] R13  (0 on first entry)
//	[saved_rsp + 24] R12  (0 on first entry)
//	[saved_rsp + 32] RBX  (native_handle_ptr on first entry)
//	[saved_rsp + 40] return address  (__vertex_coro_trampoline on first entry)
func emitCoroJump(e *emitter) {
	e.codeLabel("__vertex_coro_jump")

	// Push callee-saved registers (RBP last so it is at the lowest address).
	e.Push(asm.RBX)
	e.Push(asm.R12)
	e.Push(asm.R13)
	e.Push(asm.R14)
	e.Push(asm.RBP)

	// Save current RSP → *RDI, load new RSP from *RSI.
	e.MovSPToMem(asm.RDI, 0) // [rdi] = rsp
	e.MovMemToSP(asm.RSI, 0) // rsp   = [rsi]

	// Restore callee-saved registers from the new stack.
	e.Pop(asm.RBP)
	e.Pop(asm.R14)
	e.Pop(asm.R13)
	e.Pop(asm.R12)
	e.Pop(asm.RBX)
	e.Ret() // pops the return address from the new stack → lands in new context
}

// emitCoroTrampoline emits __vertex_coro_trampoline.
//
// Every new coroutine's stack is primed so the first coro.resume (via
// coro_jump's ret) lands here.
//
// On entry:
//
//	RBX = native_handle_ptr  (restored by coro_jump's pop rbx)
//	RSP = stack_top          (fresh, clean thread stack)
//
// The trampoline calls the coroutine body, stores its return value, marks the
// coroutine as done, and switches back to the caller by calling coro_jump.
func emitCoroTrampoline(e *emitter) {
	e.codeLabel("__vertex_coro_trampoline")
	// No standard prologue: we have a fresh stack and RBX = native_handle_ptr.

	// Call the coroutine body via its stored native pointer.
	e.LoadMem64(asm.RAX, asm.RBX, CoroFuncNative)
	e.CallReg(asm.RAX) // RAX = function return value on exit

	// Store the return value as the final CoroResult.
	e.StoreMem64(asm.RBX, CoroResult, asm.RAX)

	// Mark the coroutine as done.
	e.StoreMem32Imm(asm.RBX, CoroStatus, 2)

	// Switch back to the caller.  The caller will return from its coro.resume.
	e.LeaRegDisp(asm.RDI, asm.RBX, CoroCoroRSP)   // save (our done state)
	e.LeaRegDisp(asm.RSI, asm.RBX, CoroCallerRSP)  // load (caller's state)
	e.callSym("__vertex_coro_jump")

	e.UD2() // unreachable — coro is done and never resumed again
}

// emitCoroSpawn emits __vertex_coro_spawn(func_native_ptr i32) → handle i32.
//
// func_native_ptr is the low 32 bits of the coroutine body's native code
// address.  This is valid for non-PIE executables (v1 requirement).
//
// Steps:
//  1. memory.ref.alloc(CoroHandleSize) → wasm handle
//  2. mmap(CoroStackSize + CoroGuardSize, PROT_READ|PROT_WRITE)
//  3. mprotect(stack_base, CoroGuardSize, PROT_NONE)   ← guard page
//  4. Populate CoroHandle fields
//  5. Prime the coroutine stack per coro_jump's invariant
//  6. Inline-store the destructor pointer in the RC header
//  7. Return wasm handle ptr
//
// Register allocation:
//
//	R13 = func_native_ptr
//	R12 = handle wasm ptr
//	R14 = stack_base (native mmap result)
//	RBX = native_handle_ptr = R12 + R15
func emitCoroSpawn(e *emitter) {
	e.codeLabel("__vertex_coro_spawn")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13, asm.R14})

	// Save the function pointer before any calls clobber RDI.
	e.MovRR32(asm.R13, asm.RDI)

	// ── 1. Allocate CoroHandle ────────────────────────────────────────────────
	e.MovRI32(asm.RDI, CoroHandleSize)
	e.callSym("__vertex_memory_ref_alloc")
	e.MovRR32(asm.R12, asm.RAX) // r12 = handle wasm ptr

	// ── 2. mmap coroutine stack ───────────────────────────────────────────────
	e.MovRI32(asm.RSI, CoroStackSize+CoroGuardSize)
	e.MmapAnon()
	failMmap := e.CheckMmapError()
	e.MovRR(asm.R14, asm.RAX) // r14 = stack_base (native)

	// ── 3. Guard page ─────────────────────────────────────────────────────────
	e.MovRR(asm.RDI, asm.R14)
	e.MovRI32(asm.RSI, CoroGuardSize)
	e.XorRR32(asm.RDX) // PROT_NONE = 0
	e.SysMprotect()    // error ignored in v1

	// ── RBX = native_handle_ptr ───────────────────────────────────────────────
	e.MovRR32(asm.RBX, asm.R12)
	e.AddRR(asm.RBX, asm.R15)

	// ── 4. Populate CoroHandle ────────────────────────────────────────────────
	// CoroStatus = 0    (already zeroed by ref.alloc)
	// CoroCallerRSP = 0 (zeroed; written on first coro.resume)
	e.StoreMem64(asm.RBX, CoroFuncNative, asm.R13)
	e.StoreMem64(asm.RBX, CoroStackBase, asm.R14)
	e.StoreMem64Imm(asm.RBX, CoroStackLen, int32(CoroGuardSize+CoroStackSize))

	// ── 5. Prime the coroutine stack ──────────────────────────────────────────
	// RCX = stack_top = stack_base + CoroGuardSize + CoroStackSize
	e.MovRR(asm.RCX, asm.R14)
	e.AddRI(asm.RCX, CoroGuardSize+CoroStackSize)

	// Write the layout that coro_jump expects when it restores this context:
	//   [stack_top-48] = 0                (rbp  — popped first by coro_jump)
	//   [stack_top-40] = 0                (r14)
	//   [stack_top-32] = 0                (r13)
	//   [stack_top-24] = 0                (r12)
	//   [stack_top-16] = native_handle_ptr  (rbx — trampoline reads this)
	//   [stack_top- 8] = trampoline_addr    (return address popped by ret)
	e.StoreMem64Zero(asm.RCX, -48)
	e.StoreMem64Zero(asm.RCX, -40)
	e.StoreMem64Zero(asm.RCX, -32)
	e.StoreMem64Zero(asm.RCX, -24)
	e.StoreMem64(asm.RCX, -16, asm.RBX) // rbx = native_handle_ptr

	e.leaRIPSym(asm.RAX, "__vertex_coro_trampoline")
	e.StoreMem64(asm.RCX, -8, asm.RAX) // return address = trampoline

	// CoroCoroRSP = stack_top - 48  (coro_jump will restore from here)
	e.MovRR(asm.RAX, asm.RCX)
	e.AddRI(asm.RAX, -48)
	e.StoreMem64(asm.RBX, CoroCoroRSP, asm.RAX)

	// ── 6. Store destructor in the RC header ──────────────────────────────────
	// We write the 64-bit native pointer directly, bypassing ref_set_dtor's
	// i32-only interface.  rcDtorOffset = -16 from the user pointer.
	e.leaRIPSym(asm.RAX, "__vertex_coro_dtor")
	e.StoreMem64(asm.RBX, rcDtorOffset, asm.RAX)

	// ── 7. Return wasm handle ptr ─────────────────────────────────────────────
	e.MovRR32(asm.RAX, asm.R12)

	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13, asm.R14}, align)

	// ── Fatal path ────────────────────────────────────────────────────────────
	e.Patch32(failMmap, e.Pos())
	e.ExitGroup(127)
}

// emitCoroResume emits __vertex_coro_resume(handle i32).
//
// Transfers control into the coroutine.  If the coroutine is already done
// (status == 2) the call is a silent no-op.
//
// The switch is symmetric: coro_jump saves the caller's RSP into
// CoroCallerRSP and loads the coroutine's from CoroCoroRSP.  On yield or
// completion the coroutine reverses this, and coro_jump's ret lands on the
// instruction immediately after the callSym below.
func emitCoroResume(e *emitter) {
	e.codeLabel("__vertex_coro_resume")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR32(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15) // rbx = native_handle_ptr

	// Early exit if already done.
	e.LoadMem32ZX(asm.RAX, asm.RBX, CoroStatus)
	e.CmpRI(asm.RAX, 2)
	done := e.JeRel32()

	// coro_jump(&CoroCallerRSP, &CoroCoroRSP)
	e.LeaRegDisp(asm.RDI, asm.RBX, CoroCallerRSP)
	e.LeaRegDisp(asm.RSI, asm.RBX, CoroCoroRSP)
	e.callSym("__vertex_coro_jump")
	// ↑ Returns here when the coroutine yields or finishes.

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX}, align)
}

// emitCoroYield emits __vertex_coro_yield(handle i32, value i32).
//
// Suspends the running coroutine, stores value in CoroResult, and resumes the
// caller.  The caller returns from its coro.resume call with CoroResult set.
func emitCoroYield(e *emitter) {
	e.codeLabel("__vertex_coro_yield")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR32(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15) // rbx = native_handle_ptr

	// Store the yielded value *before* RSI is overwritten with an address.
	e.StoreMem64(asm.RBX, CoroResult, asm.RSI)

	// coro_jump(&CoroCoroRSP, &CoroCallerRSP)
	e.LeaRegDisp(asm.RDI, asm.RBX, CoroCoroRSP)
	e.LeaRegDisp(asm.RSI, asm.RBX, CoroCallerRSP)
	e.callSym("__vertex_coro_jump")
	// ↑ Returns here when the caller calls coro.resume again.

	e.Epilogue([]int{asm.RBX}, align)
}

// emitCoroDone emits __vertex_coro_done(handle i32) → i32.
// Returns 1 if CoroStatus == 2 (done), else 0.  Leaf — no frame.
func emitCoroDone(e *emitter) {
	e.codeLabel("__vertex_coro_done")
	e.AddRR(asm.RDI, asm.R15)
	e.LoadMem32ZX(asm.RAX, asm.RDI, CoroStatus)
	e.CmpRI(asm.RAX, 2)
	notDone := e.JneRel32()
	e.MovRI32(asm.RAX, 1)
	skip := e.JmpRel32()
	e.Patch32(notDone, e.Pos())
	e.XorRR32(asm.RAX)
	e.Patch32(skip, e.Pos())
	e.Ret()
}

// emitCoroResult emits __vertex_coro_result(handle i32) → i32.
// Reads the low 32 bits of CoroResult.  Leaf — no frame.
func emitCoroResult(e *emitter) {
	e.codeLabel("__vertex_coro_result")
	e.AddRR(asm.RDI, asm.R15)
	e.LoadMem32ZX(asm.RAX, asm.RDI, CoroResult)
	e.Ret()
}

// emitCoroDtor emits __vertex_coro_dtor(wasm_ptr i32).
// Called by memory.ref.release when the strong count reaches zero.
// Unmaps the coroutine stack via munmap.  Leaf — no frame.
func emitCoroDtor(e *emitter) {
	e.codeLabel("__vertex_coro_dtor")
	e.AddRR(asm.RDI, asm.R15)                    // native_handle_ptr
	e.LoadMem64(asm.RSI, asm.RDI, CoroStackLen)  // rsi = len  (load before clobbering RDI)
	e.LoadMem64(asm.RDI, asm.RDI, CoroStackBase) // rdi = base
	e.SysMunmap()
	e.Ret()
}