// cpu/arm64/memory/heap.go
package memory

import (
	"github.com/vertex-language/compiler/cpu/arm64/asm"
)

func emitComputeClass(e *emitter, totalReg, classReg, slotReg int) {
	e.MovRR32(classReg, totalReg)
	e.SUBSI32(classReg, classReg, 1)
	e.CLZ32(asm.X9, classReg)
	e.MOVZ32(asm.X10, 31, 0)
	e.SUB32(classReg, asm.X10, asm.X9)
	e.SUBSI32(classReg, classReg, 2)
	e.CMPI32(classReg, uint32(NumSizeClasses-1))
	clampOK := e.BCond(asm.CondLS)
	
	// FIX: Cast to uint16 instead of uint32 for the MOVZ immediate
	e.MOVZ32(classReg, uint16(NumSizeClasses-1), 0) 
	
	e.PatchCondImm19(clampOK, e.Pos())

	e.CMPI32(classReg, uint32(NumSizeClasses-1))
	isLarge := e.BCond(asm.CondEQ)
	e.MOVZ32(slotReg, 8, 0)
	e.LSL32(slotReg, slotReg, classReg)
	done := e.B()
	e.PatchCondImm19(isLarge, e.Pos())
	e.MOVZ32(slotReg, 0, 0)
	e.PatchBranchImm26(done, e.Pos())
}

func emitBumpAlloc(e *emitter, stateReg, dstReg, sizeReg int) {
	e.ADDSI(sizeReg, sizeReg, 7)
	e.AND32(sizeReg, sizeReg, -8)

	e.LDR64(dstReg, stateReg, uint32(StateHeapCur))
	e.MovRR(asm.X9, dstReg)
	e.ADD(asm.X9, asm.X9, sizeReg)
	e.LDR64(asm.X10, stateReg, uint32(StateHeapEnd))
	e.CMP(asm.X9, asm.X10)
	ok := e.BCond(asm.CondLS)
	e.ExitGroup(127)
	e.PatchCondImm19(ok, e.Pos())
	e.STR64(asm.X9, stateReg, uint32(StateHeapCur))
}

func emitAllocBlock(e *emitter, stateReg, dstReg int) {
	e.CMPI(asm.X2, uint32(NumSizeClasses-1))
	toLarge := e.BCond(asm.CondEQ)

	e.MovRR(asm.X3, stateReg)
	e.ADDSI(asm.X3, asm.X3, uint32(StateFreeListBase))
	e.LSLI(asm.X9, asm.X2, 3)
	e.ADD(asm.X3, asm.X3, asm.X9)

	casTop := e.Pos()
	e.LDR64(asm.X0, asm.X3, 0)
	e.CMPI(asm.X0, 0)
	toEmpty := e.BCond(asm.CondEQ)
	e.LDR64(asm.X8, asm.X0, 0)
	e.MovRR(asm.X12, asm.X0)
	e.CASAL(asm.X0, asm.X8, asm.X3)
	e.CMP(asm.X0, asm.X12)
	e.BCondBack(asm.CondNE, casTop)
	e.MovRR(dstReg, asm.X12)
	toGotBlock := e.B()

	e.PatchCondImm19(toEmpty, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.X20)
	toGotBlock2 := e.B()

	e.PatchCondImm19(toLarge, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.X19)

	e.PatchBranchImm26(toGotBlock, e.Pos())
	e.PatchBranchImm26(toGotBlock2, e.Pos())
}

func emitHeapAllocCore(e *emitter, zero bool) {
	sym := "__vertex_memory_heap_alloc"
	if !zero {
		sym = "__vertex_memory_heap_alloc_raw"
	}
	e.codeLabel(sym)
	frameSize := e.Prologue([]int{asm.X19, asm.X20, asm.X21, asm.X22})

	e.MovRR32(asm.X22, asm.X0)
	e.initCheck(asm.X21, asm.X9)
	e.MovRR32(asm.X19, asm.X22)
	e.ADDSI(asm.X19, asm.X19, uint32(HeapBlockHeaderSize))

	emitComputeClass(e, asm.X19, asm.X2, asm.X20)
	emitAllocBlock(e, asm.X21, asm.X23)

	e.STR32(asm.X2, asm.X23, 0)
	e.STR32(asm.X22, asm.X23, 4)

	if zero {
		e.MovRR(asm.X1, asm.X23)
		e.ADDSI(asm.X1, asm.X1, uint32(HeapBlockHeaderSize))
		e.MovRR(asm.X2, asm.X22)
		e.emitMemset(asm.X1, asm.XZR, asm.X2)
	}

	e.MovRR(asm.X0, asm.X23)
	e.ADDSI(asm.X0, asm.X0, uint32(HeapBlockHeaderSize))
	e.SUB(asm.X0, asm.X0, asm.MemBase)

	e.Epilogue([]int{asm.X19, asm.X20, asm.X21, asm.X22}, frameSize)
}

func emitHeapAlloc(e *emitter)    { emitHeapAllocCore(e, true) }
func emitHeapAllocRaw(e *emitter) { emitHeapAllocCore(e, false) }
func emitHeapAllocAligned(e *emitter) {
	e.codeLabel("__vertex_memory_heap_alloc_aligned")
	e.bSym("__vertex_memory_heap_alloc")
}

func emitHeapFree(e *emitter) {
	e.codeLabel("__vertex_memory_heap_free")
	frameSize := e.Prologue([]int{asm.X19, asm.X20, asm.X21})

	e.initCheck(asm.X21, asm.X9)

	e.ADD(asm.X0, asm.X0, asm.MemBase)
	e.MovRR(asm.X19, asm.X0)
	e.SUBSI(asm.X19, asm.X19, uint32(HeapBlockHeaderSize))

	e.LDR32(asm.X2, asm.X19, 0)
	e.CMPI(asm.X2, uint32(NumSizeClasses-1))
	done := e.BCond(asm.CondEQ)

	e.MovRR(asm.X20, asm.X21)
	e.ADDSI(asm.X20, asm.X20, uint32(StateFreeListBase))
	e.LSLI(asm.X9, asm.X2, 3)
	e.ADD(asm.X20, asm.X20, asm.X9)

	pushTop := e.Pos()
	e.LDR64(asm.X0, asm.X20, 0)
	e.STR64(asm.X0, asm.X19, 0)
	e.MovRR(asm.X12, asm.X0)
	e.CASAL(asm.X0, asm.X19, asm.X20)
	e.CMP(asm.X0, asm.X12)
	e.BCondBack(asm.CondNE, pushTop)

	e.PatchCondImm19(done, e.Pos())
	e.Epilogue([]int{asm.X19, asm.X20, asm.X21}, frameSize)
}

func emitHeapRealloc(e *emitter) {
	e.codeLabel("__vertex_memory_heap_realloc")
	frameSize := e.Prologue([]int{asm.X19, asm.X20, asm.X21})

	e.MovRR32(asm.X21, asm.X0)
	e.MovRR32(asm.X20, asm.X1)

	e.CMPI(asm.X21, 0)
	isNull := e.BCond(asm.CondEQ)

	e.CMPI(asm.X20, 0)
	isZeroSize := e.BCond(asm.CondEQ)

	e.MovRR32(asm.X0, asm.X20)
	e.callSym("__vertex_memory_heap_alloc_raw")
	e.MovRR(asm.X19, asm.X0)

	e.MovRR32(asm.X1, asm.X21)
	e.ADD(asm.X1, asm.X1, asm.MemBase)
	e.MovRR(asm.X0, asm.X19)
	e.ADD(asm.X0, asm.X0, asm.MemBase)

	e.SUBSI(asm.X9, asm.X1, uint32(HeapBlockHeaderSize-4))
	e.LDR32(asm.X2, asm.X9, 0)
	e.CMP(asm.X2, asm.X20)
	useOld := e.BCond(asm.CondLS)
	e.MovRR32(asm.X2, asm.X20)
	e.PatchCondImm19(useOld, e.Pos())
	e.emitMemcpy(asm.X0, asm.X1, asm.X2)

	e.MovRR32(asm.X0, asm.X21)
	e.callSym("__vertex_memory_heap_free")

	e.MovRR(asm.X0, asm.X19)
	done := e.B()

	e.PatchCondImm19(isNull, e.Pos())
	e.MovRR32(asm.X0, asm.X20)
	e.callSym("__vertex_memory_heap_alloc")
	done1 := e.B()

	e.PatchCondImm19(isZeroSize, e.Pos())
	e.MovRR32(asm.X0, asm.X21)
	e.callSym("__vertex_memory_heap_free")
	e.MOVZ32(asm.X0, 0, 0)

	e.PatchBranchImm26(done, e.Pos())
	e.PatchBranchImm26(done1, e.Pos())
	e.Epilogue([]int{asm.X19, asm.X20, asm.X21}, frameSize)
}