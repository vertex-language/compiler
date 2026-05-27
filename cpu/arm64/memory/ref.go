// cpu/arm64/memory/ref.go
package memory

import (
	"github.com/vertex-language/compiler/cpu/arm64/asm"
)

const (
	rcStrong = int32(-RCHeaderSize + 0)
	rcWeak   = int32(-RCHeaderSize + 8)
	rcDtor   = int32(-RCHeaderSize + 16)
	rcClass  = int32(-RCHeaderSize + 24)
	rcSize   = int32(-RCHeaderSize + 28)
)

func emitRefAlloc(e *emitter) {
	e.codeLabel("__vertex_memory_ref_alloc")
	frameSize := e.Prologue([]int{asm.X19, asm.X20, asm.X21})

	e.MovRR32(asm.X20, asm.X0)
	e.initCheck(asm.X21, asm.X9)
	e.MovRR32(asm.X0, asm.X20)
	e.ADDSI(asm.X0, asm.X0, uint32(RCHeaderSize))

	e.callSym("__vertex_memory_heap_alloc_raw")

	e.MovRR(asm.X19, asm.X0)
	e.ADD(asm.X19, asm.X19, asm.MemBase)

	e.SUBSI(asm.X10, asm.X19, uint32(HeapBlockHeaderSize))
	e.LDR32(asm.X9, asm.X10, 0)

	e.MOVZ(asm.X11, 1, 0)
	e.SUBSI(asm.X10, asm.X19, uint32(-rcStrong))
	e.STR64(asm.X11, asm.X10, 0)
	
	e.SUBSI(asm.X10, asm.X19, uint32(-rcWeak))
	e.STR64(asm.XZR, asm.X10, 0)
	
	e.SUBSI(asm.X10, asm.X19, uint32(-rcDtor))
	e.STR64(asm.XZR, asm.X10, 0)
	
	e.SUBSI(asm.X10, asm.X19, uint32(-rcClass))
	e.STR32(asm.X9, asm.X10, 0)
	
	e.SUBSI(asm.X10, asm.X19, uint32(-rcSize))
	e.STR32(asm.X20, asm.X10, 0)

	e.MovRR(asm.X1, asm.X19)
	e.ADDSI(asm.X1, asm.X1, uint32(RCHeaderSize))
	e.emitMemset(asm.X1, asm.XZR, asm.X20)

	e.MovRR(asm.X0, asm.X19)
	e.ADDSI(asm.X0, asm.X0, uint32(RCHeaderSize))
	e.SUB(asm.X0, asm.X0, asm.MemBase)

	e.Epilogue([]int{asm.X19, asm.X20, asm.X21}, frameSize)
}

func emitRefAllocWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_alloc_weak")
	e.bSym("__vertex_memory_ref_alloc")
}

func emitRefRetain(e *emitter) {
	e.codeLabel("__vertex_memory_ref_retain")
	e.ADD(asm.X0, asm.X0, asm.MemBase)
	e.SUBSI(asm.X10, asm.X0, uint32(-rcStrong))
	e.MOVZ(asm.X9, 1, 0)
	e.LDADD(asm.X9, asm.XZR, asm.X10)
	e.RET()
}

func emitRefRelease(e *emitter) {
	e.codeLabel("__vertex_memory_ref_release")
	frameSize := e.Prologue([]int{asm.X19})

	e.MovRR(asm.X19, asm.X0)
	e.ADD(asm.X19, asm.X19, asm.MemBase)

	e.SUBSI(asm.X10, asm.X19, uint32(-rcStrong))
	// X9 = -1
	e.Emit32(0x92800009) // movn x9, #0
	e.LDADD(asm.X9, asm.X11, asm.X10) // X11 = old value

	// If old value was 1, it's now 0.
	e.CMPI(asm.X11, 1)
	done := e.BCond(asm.CondNE)

	e.SUBSI(asm.X10, asm.X19, uint32(-rcDtor))
	e.LDR64(asm.X9, asm.X10, 0)
	e.CMPI(asm.X9, 0)
	noDtor := e.BCond(asm.CondEQ)
	
	e.MovRR(asm.X0, asm.X19)
	e.SUB(asm.X0, asm.X0, asm.MemBase)
	e.BLR(asm.X9)
	e.PatchCondImm19(noDtor, e.Pos())

	e.SUBSI(asm.X10, asm.X19, uint32(-rcWeak))
	e.LDR64(asm.X9, asm.X10, 0)
	e.CMPI(asm.X9, 0)
	hasWeak := e.BCond(asm.CondNE)

	e.MovRR(asm.X0, asm.X19)
	e.SUB(asm.X0, asm.X0, asm.MemBase)
	e.SUBSI(asm.X0, asm.X0, uint32(RCHeaderSize))
	e.callSym("__vertex_memory_heap_free")

	e.PatchCondImm19(hasWeak, e.Pos())
	e.PatchCondImm19(done, e.Pos())
	e.Epilogue([]int{asm.X19}, frameSize)
}

func emitRefSetDtor(e *emitter) {
	e.codeLabel("__vertex_memory_ref_set_dtor")
	e.ADD(asm.X0, asm.X0, asm.MemBase)
	e.SUBSI(asm.X10, asm.X0, uint32(-rcDtor))
	e.STR64(asm.X1, asm.X10, 0)
	e.RET()
}

func emitRefWeak(e *emitter) {
	e.codeLabel("__vertex_memory_ref_weak")
	e.ADD(asm.X0, asm.X0, asm.MemBase)
	e.SUBSI(asm.X10, asm.X0, uint32(-rcWeak))
	e.MOVZ(asm.X9, 1, 0)
	e.LDADD(asm.X9, asm.XZR, asm.X10)
	e.SUB(asm.X0, asm.X0, asm.MemBase)
	e.RET()
}

func emitRefUpgrade(e *emitter) {
	e.codeLabel("__vertex_memory_ref_upgrade")
	frameSize := e.Prologue([]int{asm.X19})

	e.MovRR(asm.X19, asm.X0)
	e.ADD(asm.X19, asm.X19, asm.MemBase)

	e.SUBSI(asm.X10, asm.X19, uint32(-rcStrong))

	casTop := e.Pos()
	e.LDR64(asm.X9, asm.X10, 0) // expected
	e.CMPI(asm.X9, 0)
	fail := e.BCond(asm.CondEQ)

	e.MovRR(asm.X11, asm.X9)
	e.ADDSI(asm.X11, asm.X11, 1) // desired
	
	e.MovRR(asm.X12, asm.X9) // save expected
	e.CASAL(asm.X9, asm.X11, asm.X10) 
	e.CMP(asm.X9, asm.X12)
	e.BCondBack(asm.CondNE, casTop)

	e.MOVZ32(asm.X0, 1, 0)
	done := e.B()

	e.PatchCondImm19(fail, e.Pos())
	e.MOVZ32(asm.X0, 0, 0)

	e.PatchBranchImm26(done, e.Pos())
	e.Epilogue([]int{asm.X19}, frameSize)
}