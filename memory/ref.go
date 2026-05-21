package memory

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// RC header offsets relative to the native user pointer (i.e. negative offsets):
//
//	[native_user − 32 + 0]  = strong_count  int64 atomic
//	[native_user − 32 + 8]  = weak_count    int64 atomic
//	[native_user − 32 + 16] = dtor_fn_ptr   int64 (native fn ptr, 0 = none)
//	[native_user − 32 + 24] = size_class    uint32
//	[native_user − 32 + 28] = user_size     uint32
const (
	rcStrong = int64(-RCHeaderSize + 0)
	rcWeak   = int64(-RCHeaderSize + 8)
	rcDtor   = int64(-RCHeaderSize + 16)
	rcClass  = int64(-RCHeaderSize + 24)
	rcSize   = int64(-RCHeaderSize + 28)
)

func emitRefAlloc(e *emitter) {
	e.codeLabel("__vertex_memory_ref_alloc")
	align := e.Prologue([]int{asm.RBX, asm.R13, asm.R14})

	e.MovRR32(asm.R14, asm.RDI)           // save user_size BEFORE initCheck clobbers rdi
	e.initCheck(asm.R13, asm.RAX)
	e.MovRR32(asm.RDI, asm.R14)           // restore rdi from r14 (init may have clobbered it)
	e.AddRI(asm.RDI, RCHeaderSize)        // rdi = RCHeaderSize + user_size

	e.callSym("__vertex_memory_heap_alloc_raw")

	e.MovRR(asm.RBX, asm.RAX)
	e.AddRR(asm.RBX, asm.R15)

	e.LoadMem32ZX(asm.RCX, asm.RBX, int64(-HeapBlockHeaderSize))

	e.StoreMem64Imm(asm.RBX, 0, 1)
	e.StoreMem64Zero(asm.RBX, 8)
	e.StoreMem64Zero(asm.RBX, 16)
	e.StoreMem32R(asm.RBX, 24, asm.RCX)
	e.StoreMem32R(asm.RBX, 28, asm.R14)

	e.MovRR(asm.RDI, asm.RBX)
	e.AddRI(asm.RDI, RCHeaderSize)
	e.XorRR32(asm.RAX)
	e.MovRR32(asm.RCX, asm.R14)
	e.RepStosb()

	e.MovRR(asm.RAX, asm.RBX)
	e.AddRI(asm.RAX, RCHeaderSize)
	e.SubRR(asm.RAX, asm.R15)

	e.Epilogue([]int{asm.RBX, asm.R13, asm.R14}, align)
}

// emitRefAllocWeak emits __vertex_memory_ref_alloc_weak.
// Semantically identical to ref_alloc in v1 — same header layout.
func emitRefAllocWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_alloc_weak")
	e.jmpSym("__vertex_memory_ref_alloc")
}

// emitRefRetain emits __vertex_memory_ref_retain(ptr i32).
// Atomically increments the strong reference count. Leaf — no prologue.
func emitRefRetain(e *emitter) {
	e.codeLabel("__vertex_memory_ref_retain")
	// rdi = wasm offset (zero-extended i32); native user ptr = rdi + r15.
	e.AddRR(asm.RDI, asm.R15)
	e.LockIncMem64(asm.RDI, rcStrong)
	e.Ret()
}

// emitRefRelease emits __vertex_memory_ref_release(ptr i32).
//
// Atomically decrements strong count. If it reaches zero:
//  1. Calls the destructor (if set) with the wasm ptr.
//  2. If weak_count is also zero, frees the backing heap block.
func emitRefRelease(e *emitter) {
	e.codeLabel("__vertex_memory_ref_release")
	align := e.Prologue([]int{asm.RBX})

	// rbx = native user ptr
	e.MovRR(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15)

	e.LockDecMem64(asm.RBX, rcStrong)
	done := e.JnzRel32() // still referenced → nothing to do

	// strong == 0: call destructor if present.
	e.LoadMem64(asm.RAX, asm.RBX, rcDtor)
	e.TestRR64(asm.RAX)
	noDtor := e.JzRel32()
	e.MovRR(asm.RDI, asm.RBX)
	e.SubRR(asm.RDI, asm.R15) // pass wasm ptr to dtor
	e.CallReg(asm.RAX)
	e.Patch32(noDtor, e.Pos())

	// If weak_count > 0 the block must stay alive for weak upgrades.
	e.LoadMem64(asm.RAX, asm.RBX, rcWeak)
	e.TestRR64(asm.RAX)
	hasWeak := e.JnzRel32()

	// Free: pass wasm offset of the heap-user area (= wasm_ptr − RCHeaderSize).
	// The RC header is what heap_alloc_raw returned as its user pointer.
	e.MovRR(asm.RDI, asm.RBX)
	e.SubRR(asm.RDI, asm.R15)
	e.AddRI(asm.RDI, -int64(RCHeaderSize))
	e.callSym("__vertex_memory_heap_free")

	e.Patch32(hasWeak, e.Pos())
	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX}, align)
}

// emitRefSetDtor emits __vertex_memory_ref_set_dtor(ptr i32, fn i32).
// Stores fn (a native function pointer stored as i32) into dtor_fn_ptr.
// Leaf — no prologue.
func emitRefSetDtor(e *emitter) {
	e.codeLabel("__vertex_memory_ref_set_dtor")
	// rdi = ptr (wasm), rsi = fn (zero-extended i32 function pointer)
	e.AddRR(asm.RDI, asm.R15)               // native user ptr
	e.StoreMem64(asm.RDI, rcDtor, asm.RSI)  // dtor_fn_ptr = rsi
	e.Ret()
}

// emitRefWeak emits __vertex_memory_ref_weak(ptr i32) → i32.
// Increments the weak count and returns the same wasm ptr.
// Leaf — no prologue.
func emitRefWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_weak")
	e.MovRR(asm.RAX, asm.RDI)             // rax = wasm ptr (return value)
	e.AddRR(asm.RDI, asm.R15)             // native user ptr
	e.LockIncMem64(asm.RDI, rcWeak)
	e.Ret()
}

// emitRefUpgrade emits __vertex_memory_ref_upgrade(ptr i32) → i32.
//
// Attempts to atomically increment strong_count, but only if it is > 0
// (i.e. the object has not been freed). Returns the wasm ptr on success, 0
// if the object has already been destroyed.
func emitRefUpgrade(e *emitter) {
	e.codeLabel("__vertex_memory_ref_upgrade")
	align := e.Prologue([]int{asm.RBX})

	// rbx = native user ptr
	e.MovRR(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15)

	// CAS loop: increment strong_count only if it is currently > 0.
	casTop := e.Pos()
	e.LoadMem64(asm.RAX, asm.RBX, rcStrong) // rax = current strong count
	e.TestRR64(asm.RAX)
	freed := e.JzRel32() // already zero → object freed

	e.MovRR(asm.RCX, asm.RAX)
	e.AddRI(asm.RCX, 1)                           // rcx = expected+1
	e.LockCmpxchg(asm.RBX, rcStrong, asm.RCX)    // CAS [rbx+rcStrong] rax→rcx
	e.JneRel32Back(casTop)                         // retry if lost the race

	// Success: return wasm ptr.
	e.MovRR(asm.RAX, asm.RBX)
	e.SubRR(asm.RAX, asm.R15)
	done := e.JmpRel32()

	// Object freed: return 0.
	e.Patch32(freed, e.Pos())
	e.XorRR32(asm.RAX)

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX}, align)
}