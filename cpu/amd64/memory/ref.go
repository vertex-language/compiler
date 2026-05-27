package memory

import (
	"github.com/vertex-language/compiler/cpu/amd64/asm"
)

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

	e.MovRR32(asm.R14, asm.RDI)
	e.initCheck(asm.R13, asm.RAX)
	e.MovRR32(asm.RDI, asm.R14)
	e.AddRI(asm.RDI, RCHeaderSize)

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

func emitRefAllocWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_alloc_weak")
	e.jmpSym("__vertex_memory_ref_alloc")
}

func emitRefRetain(e *emitter) {
	e.codeLabel("__vertex_memory_ref_retain")
	e.AddRR(asm.RDI, asm.R15)
	e.LockIncMem64(asm.RDI, rcStrong)
	e.Ret()
}

func emitRefRelease(e *emitter) {
	e.codeLabel("__vertex_memory_ref_release")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15)

	e.LockDecMem64(asm.RBX, rcStrong)
	done := e.JnzRel32()

	e.LoadMem64(asm.RAX, asm.RBX, rcDtor)
	e.TestRR64(asm.RAX)
	noDtor := e.JzRel32()
	e.MovRR(asm.RDI, asm.RBX)
	e.SubRR(asm.RDI, asm.R15)
	e.CallReg(asm.RAX)
	e.Patch32(noDtor, e.Pos())

	e.LoadMem64(asm.RAX, asm.RBX, rcWeak)
	e.TestRR64(asm.RAX)
	hasWeak := e.JnzRel32()

	e.MovRR(asm.RDI, asm.RBX)
	e.SubRR(asm.RDI, asm.R15)
	e.AddRI(asm.RDI, -int64(RCHeaderSize))
	e.callSym("__vertex_memory_heap_free")

	e.Patch32(hasWeak, e.Pos())
	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX}, align)
}

func emitRefSetDtor(e *emitter) {
	e.codeLabel("__vertex_memory_ref_set_dtor")
	e.AddRR(asm.RDI, asm.R15)
	e.StoreMem64(asm.RDI, rcDtor, asm.RSI)
	e.Ret()
}

func emitRefWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_weak")
	e.MovRR(asm.RAX, asm.RDI)
	e.AddRR(asm.RDI, asm.R15)
	e.LockIncMem64(asm.RDI, rcWeak)
	e.Ret()
}

func emitRefUpgrade(e *emitter) {
	e.codeLabel("__vertex_memory_ref_upgrade")
	align := e.Prologue([]int{asm.RBX})

	e.MovRR(asm.RBX, asm.RDI)
	e.AddRR(asm.RBX, asm.R15)

	casTop := e.Pos()
	
	// Load current strong count into RAX
	e.LoadMem64(asm.RAX, asm.RBX, rcStrong)
	
	// If strong count == 0, the object is dead. Jump to failure.
	e.TestRR64(asm.RAX)
	fail := e.JzRel32()

	// Calculate RAX + 1 and store it in RCX
	e.MovRR(asm.RCX, asm.RAX)
	e.AddRI(asm.RCX, 1)

	// Atomic Compare-And-Swap: if [RBX+rcStrong] == RAX, set to RCX
	e.LockCmpxchg(asm.RBX, rcStrong, asm.RCX)
	
	// If ZF is clear (not equal), another thread modified the count. Retry.
	e.JneRel32Back(casTop)

	// SUCCESS: Re-calculate the wasm pointer (RBX - R15) and return it in RAX
	e.MovRR(asm.RAX, asm.RBX)
	e.SubRR(asm.RAX, asm.R15)
	done := e.JmpRel32()

	// FAILURE: Return 0 (null)
	e.Patch32(fail, e.Pos())
	e.XorRR32(asm.RAX)

	// Finalize function
	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX}, align)
}