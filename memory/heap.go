package memory

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitComputeClass computes the size class and slot size for a total allocation.
//
//   totalReg — 64-bit register containing total bytes (user_size + header).
//              Minimum value is HeapBlockHeaderSize (8); BSR is always defined.
//   classReg — output: size class 0–10. MUST be RCX (ShlCL32 uses CL).
//   slotReg  — output: slot size (8<<class for class<10, 0 for large).
//
// Uses BSR to map total bytes to a power-of-two size class:
//
//	class = clamp(BSR(total−1) − 2, 0, 10)
//	slot  = (class < 10) ? 8<<class : 0
//
// Clobbers classReg and slotReg; does not modify totalReg.
func emitComputeClass(e *emitter, totalReg, classReg, slotReg int) {
	// classReg32 = totalReg32 − 1  (minimum 7, so BSR is always defined)
	e.MovRR32(classReg, totalReg)
	e.Dec32(classReg)
	// classReg = floor(log2(classReg))  via BSR
	e.BSR32(classReg, classReg)
	// Adjust: class 0 covers ≤8 B, and BSR(7)=2, so subtract 2.
	e.SubRI(classReg, 2)
	// Clamp to [0, 10]. Since minimum input total=8, BSR−2 ≥ 0; no lower clamp.
	e.CmpRI(classReg, int64(NumSizeClasses-1))
	clampOK := e.JbeRel32() // unsigned ≤ 10: already in range
	e.MovRI32(classReg, uint32(NumSizeClasses-1))
	e.Patch32(clampOK, e.Pos())

	// slotReg = (class == 10) ? 0 : 8 << class
	e.CmpRI(classReg, int64(NumSizeClasses-1))
	isLarge := e.JeRel32()
	e.MovRI32(slotReg, 8)
	e.ShlCL32(slotReg) // classReg must be RCX so CL holds the count
	done := e.JmpRel32()
	e.Patch32(isLarge, e.Pos())
	e.XorRR32(slotReg)
	e.Patch32(done, e.Pos())
}

// emitBumpAlloc allocates sizeReg bytes from the heap bump pointer (non-atomic,
// v1 single-threaded assumption). On success dstReg = native pointer to block.
// On OOM calls ExitGroup(127) — no return.
//
//   stateReg — &__vertex_alloc_state (not RAX or RDX)
//   dstReg   — output native block pointer (receives heap_cur before advance)
//   sizeReg  — allocation size in bytes; modified (rounded up to 8-byte alignment)
//
// Clobbers: RAX, RDX, sizeReg.
func emitBumpAlloc(e *emitter, stateReg, dstReg, sizeReg int) {
	// Round sizeReg up to 8-byte alignment.
	e.AddRI(sizeReg, 7)
	e.AndRI8(sizeReg, -8)

	e.LoadMem64(dstReg, stateReg, StateHeapCur)  // dstReg = old cur (our block)
	e.MovRR(asm.RAX, dstReg)
	e.AddRR(asm.RAX, sizeReg)                    // rax = new cur
	e.LoadMem64(asm.RDX, stateReg, StateHeapEnd) // rdx = heap_end
	e.CmpRR(asm.RAX, asm.RDX)
	ok := e.JbeRel32() // new_cur ≤ heap_end → OK
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())
	e.StoreMem64(stateReg, StateHeapCur, asm.RAX) // commit new cur
}

// emitAllocBlock emits the block-acquisition sequence: try the per-class
// Treiber free-list first, fall back to bump allocation. On return dstReg
// holds the native pointer to the raw block (before any header write).
//
// On entry:
//
//	RCX      = size class (0–10)  from emitComputeClass
//	R12      = slot size (8<<class or 0 for large)  from emitComputeClass
//	RDI      = total bytes (user + header); used only for the large path
//	stateReg = &__vertex_alloc_state
//
// On exit:
//
//	dstReg = native pointer to allocated block
//	RCX    = size class (preserved — needed for header writes)
//	R12    = modified by emitBumpAlloc (no longer slot size)
//
// Clobbers: RAX, RDX, R8 (all caller-saved).
func emitAllocBlock(e *emitter, stateReg, dstReg int) {
	// Large allocations skip the free list entirely.
	e.CmpRI(asm.RCX, int64(NumSizeClasses-1))
	toLarge := e.JeRel32()

	// ── Small / medium: Treiber-stack free-list pop ───────────────────────────
	// RDX = &free_list[class] = state + StateFreeListBase + class*8
	e.MovRR(asm.RDX, stateReg)
	e.AddRI(asm.RDX, StateFreeListBase)
	e.LeaScale(asm.RDX, asm.RDX, asm.RCX, 8)

	// CAS pop loop.
	// RAX = expected head; R8 = head->next (the new head on success).
	// On cmpxchg failure the CPU updates RAX to the current [RDX], so we just
	// reload next from the new head and retry.
	casTop := e.Pos()
	e.LoadMem64(asm.RAX, asm.RDX, 0) // rax = current head
	e.TestRR64(asm.RAX)
	toEmpty := e.JzRel32()             // head == null → bump alloc
	e.LoadMem64(asm.R8, asm.RAX, 0)  // r8 = head->next
	e.LockCmpxchg(asm.RDX, 0, asm.R8)
	e.JneRel32Back(casTop)            // retry if lost the race
	// Success: RAX = our block.
	e.MovRR(dstReg, asm.RAX)
	toGotBlock := e.JmpRel32()

	// ── Empty list: bump allocate slot-sized block ────────────────────────────
	e.Patch32(toEmpty, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.R12) // R12 = slot size
	toGotBlock2 := e.JmpRel32()

	// ── Large: bump allocate total-sized block ────────────────────────────────
	e.Patch32(toLarge, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.RDI) // RDI = total size

	e.Patch32(toGotBlock, e.Pos())
	e.Patch32(toGotBlock2, e.Pos())
}

func emitHeapAllocCore(e *emitter, zero bool) {
	sym := "__vertex_memory_heap_alloc"
	if !zero {
		sym = "__vertex_memory_heap_alloc_raw"
	}
	e.codeLabel(sym)
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13, asm.R14})

	e.MovRR32(asm.R14, asm.RDI)           // save user_size BEFORE initCheck clobbers rdi
	e.initCheck(asm.R13, asm.RAX)
	e.MovRR32(asm.RDI, asm.R14)           // restore rdi from r14 (init may have clobbered it)
	e.AddRI(asm.RDI, HeapBlockHeaderSize) // rdi = total

	emitComputeClass(e, asm.RDI, asm.RCX, asm.R12)
	emitAllocBlock(e, asm.R13, asm.RBX)

	e.StoreMem32R(asm.RBX, 0, asm.RCX)
	e.StoreMem32R(asm.RBX, 4, asm.R14)

	if zero {
		e.MovRR(asm.RDI, asm.RBX)
		e.AddRI(asm.RDI, HeapBlockHeaderSize)
		e.XorRR32(asm.RAX)
		e.MovRR32(asm.RCX, asm.R14)
		e.RepStosb()
	}

	e.MovRR(asm.RAX, asm.RBX)
	e.AddRI(asm.RAX, HeapBlockHeaderSize)
	e.SubRR(asm.RAX, asm.R15)

	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13, asm.R14}, align)
}

func emitHeapAlloc(e *emitter)    { emitHeapAllocCore(e, true) }
func emitHeapAllocRaw(e *emitter) { emitHeapAllocCore(e, false) }

// emitHeapAllocAligned emits __vertex_memory_heap_alloc_aligned.
// v1: alignment parameter (RSI) is ignored; data is at least 8-byte aligned.
// Tail-calls heap_alloc.
func emitHeapAllocAligned(e *emitter) {
	e.codeLabel("__vertex_memory_heap_alloc_aligned")
	e.jmpSym("__vertex_memory_heap_alloc")
}

// emitHeapFree emits __vertex_memory_heap_free(ptr i32).
//
// Translates the wasm offset to a native block pointer, reads the size class
// from the block header, and pushes the block onto the appropriate Treiber
// free-list via a CAS loop. Large blocks (class 10) are not reclaimed in v1.
//
// Register map:
//
//	rbx = native block pointer (native_user − HeapBlockHeaderSize)
//	r12 = &free_list[class]
//	r13 = &__vertex_alloc_state
//	rcx = size class
func emitHeapFree(e *emitter) {
	e.codeLabel("__vertex_memory_heap_free")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	// native_user = rdi (wasm offset, zero-extended) + r15
	e.AddRR(asm.RDI, asm.R15)
	// block = native_user − HeapBlockHeaderSize
	e.MovRR(asm.RBX, asm.RDI)
	e.AddRI(asm.RBX, -HeapBlockHeaderSize)

	// class = [block+0]  (32-bit, zero-extend)
	e.LoadMem32ZX(asm.RCX, asm.RBX, 0)

	// Large blocks are not reclaimed in v1.
	e.CmpRI(asm.RCX, int64(NumSizeClasses-1))
	done := e.JeRel32()

	// R12 = &free_list[class]
	e.MovRR(asm.R12, asm.R13)
	e.AddRI(asm.R12, StateFreeListBase)
	e.LeaScale(asm.R12, asm.R12, asm.RCX, 8)

	// Treiber-stack push: set block->next = old_head, CAS head from old to block.
	pushTop := e.Pos()
	e.LoadMem64(asm.RAX, asm.R12, 0)   // rax = old head
	e.StoreMem64(asm.RBX, 0, asm.RAX) // block->next = old head
	e.LockCmpxchg(asm.R12, 0, asm.RBX) // if [r12]==rax: [r12]=rbx; else rax=[r12]
	e.JneRel32Back(pushTop)

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)
}

// emitHeapRealloc emits __vertex_memory_heap_realloc(ptr i32, new_size i32) → i32.
//
//   ptr==0             → heap_alloc(new_size)
//   new_size==0        → heap_free(ptr), return 0
//   otherwise          → heap_alloc_raw(new_size), copy min(old,new) bytes,
//                        heap_free(ptr), return new ptr
//
// Register map:
//
//	r14 = old_ptr  (wasm offset, saved from rdi)
//	r12 = new_size (saved from rsi)
//	rbx = new wasm ptr (result of heap_alloc_raw)
func emitHeapRealloc(e *emitter) {
	e.codeLabel("__vertex_memory_heap_realloc")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R14})

	// Save args before any calls.
	e.MovRR32(asm.R14, asm.RDI) // r14 = old_ptr (wasm)
	e.MovRR32(asm.R12, asm.RSI) // r12 = new_size

	// Case 1: old_ptr == 0 → alloc
	e.TestRR64(asm.RDI)
	isNull := e.JzRel32()

	// Case 2: new_size == 0 → free + return 0
	e.TestRR64(asm.R12)
	isZeroSize := e.JzRel32()

	// Case 3: alloc new, copy, free old.
	e.MovRR32(asm.RDI, asm.R12)
	e.callSym("__vertex_memory_heap_alloc_raw")
	e.MovRR(asm.RBX, asm.RAX) // rbx = new wasm ptr

	// RSI = old native user ptr; RDI = new native user ptr.
	e.MovRR32(asm.RSI, asm.R14)
	e.AddRR(asm.RSI, asm.R15)
	e.MovRR(asm.RDI, asm.RBX)
	e.AddRR(asm.RDI, asm.R15)

	// RCX = min(old_user_size, new_size) — copy count.
	e.LoadMem32ZX(asm.RCX, asm.RSI, int64(-HeapBlockHeaderSize+4)) // old_user_size
	e.CmpRR(asm.RCX, asm.R12)
	useOld := e.JbRel32()        // old_size < new_size: rcx is already min
	e.MovRR32(asm.RCX, asm.R12) // else use new_size
	e.Patch32(useOld, e.Pos())
	e.RepMovsb()

	// Free old block.
	e.MovRR32(asm.RDI, asm.R14)
	e.callSym("__vertex_memory_heap_free")

	// Return new wasm ptr.
	e.MovRR(asm.RAX, asm.RBX)
	done := e.JmpRel32()

	// Case 1: alloc(new_size)
	e.Patch32(isNull, e.Pos())
	e.MovRR32(asm.RDI, asm.R12)
	e.callSym("__vertex_memory_heap_alloc")
	done1 := e.JmpRel32()

	// Case 2: free(old_ptr), return 0
	e.Patch32(isZeroSize, e.Pos())
	e.MovRR32(asm.RDI, asm.R14)
	e.callSym("__vertex_memory_heap_free")
	e.XorRR32(asm.RAX)

	e.Patch32(done, e.Pos())
	e.Patch32(done1, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R14}, align)
}