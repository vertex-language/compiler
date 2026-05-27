package memory

import (
	"github.com/vertex-language/compiler/cpu/amd64/asm"
)

func emitComputeClass(e *emitter, totalReg, classReg, slotReg int) {
	e.MovRR32(classReg, totalReg)
	e.Dec32(classReg)
	e.BSR32(classReg, classReg)
	e.SubRI(classReg, 2)
	e.CmpRI(classReg, int64(NumSizeClasses-1))
	clampOK := e.JbeRel32()
	e.MovRI32(classReg, uint32(NumSizeClasses-1))
	e.Patch32(clampOK, e.Pos())

	e.CmpRI(classReg, int64(NumSizeClasses-1))
	isLarge := e.JeRel32()
	e.MovRI32(slotReg, 8)
	e.ShlCL32(slotReg)
	done := e.JmpRel32()
	e.Patch32(isLarge, e.Pos())
	e.XorRR32(slotReg)
	e.Patch32(done, e.Pos())
}

func emitBumpAlloc(e *emitter, stateReg, dstReg, sizeReg int) {
	e.AddRI(sizeReg, 7)
	e.AndRI8(sizeReg, -8)

	e.LoadMem64(dstReg, stateReg, StateHeapCur)
	e.MovRR(asm.RAX, dstReg)
	e.AddRR(asm.RAX, sizeReg)
	e.LoadMem64(asm.RDX, stateReg, StateHeapEnd)
	e.CmpRR(asm.RAX, asm.RDX)
	ok := e.JbeRel32()
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())
	e.StoreMem64(stateReg, StateHeapCur, asm.RAX)
}

func emitAllocBlock(e *emitter, stateReg, dstReg int) {
	e.CmpRI(asm.RCX, int64(NumSizeClasses-1))
	toLarge := e.JeRel32()

	e.MovRR(asm.RDX, stateReg)
	e.AddRI(asm.RDX, StateFreeListBase)
	e.LeaScale(asm.RDX, asm.RDX, asm.RCX, 8)

	casTop := e.Pos()
	e.LoadMem64(asm.RAX, asm.RDX, 0)
	e.TestRR64(asm.RAX)
	toEmpty := e.JzRel32()
	e.LoadMem64(asm.R8, asm.RAX, 0)
	e.LockCmpxchg(asm.RDX, 0, asm.R8)
	e.JneRel32Back(casTop)
	e.MovRR(dstReg, asm.RAX)
	toGotBlock := e.JmpRel32()

	e.Patch32(toEmpty, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.R12)
	toGotBlock2 := e.JmpRel32()

	e.Patch32(toLarge, e.Pos())
	emitBumpAlloc(e, stateReg, dstReg, asm.RDI)

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

	e.MovRR32(asm.R14, asm.RDI)
	e.initCheck(asm.R13, asm.RAX)
	e.MovRR32(asm.RDI, asm.R14)
	e.AddRI(asm.RDI, HeapBlockHeaderSize)

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

func emitHeapAllocAligned(e *emitter) {
	e.codeLabel("__vertex_memory_heap_alloc_aligned")
	e.jmpSym("__vertex_memory_heap_alloc")
}

func emitHeapFree(e *emitter) {
	e.codeLabel("__vertex_memory_heap_free")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	e.AddRR(asm.RDI, asm.R15)
	e.MovRR(asm.RBX, asm.RDI)
	e.AddRI(asm.RBX, -HeapBlockHeaderSize)

	e.LoadMem32ZX(asm.RCX, asm.RBX, 0)

	e.CmpRI(asm.RCX, int64(NumSizeClasses-1))
	done := e.JeRel32()

	e.MovRR(asm.R12, asm.R13)
	e.AddRI(asm.R12, StateFreeListBase)
	e.LeaScale(asm.R12, asm.R12, asm.RCX, 8)

	pushTop := e.Pos()
	e.LoadMem64(asm.RAX, asm.R12, 0)
	e.StoreMem64(asm.RBX, 0, asm.RAX)
	e.LockCmpxchg(asm.R12, 0, asm.RBX)
	e.JneRel32Back(pushTop)

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)
}

func emitHeapRealloc(e *emitter) {
	e.codeLabel("__vertex_memory_heap_realloc")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R14})

	e.MovRR32(asm.R14, asm.RDI)
	e.MovRR32(asm.R12, asm.RSI)

	e.TestRR64(asm.RDI)
	isNull := e.JzRel32()

	e.TestRR64(asm.R12)
	isZeroSize := e.JzRel32()

	e.MovRR32(asm.RDI, asm.R12)
	e.callSym("__vertex_memory_heap_alloc_raw")
	e.MovRR(asm.RBX, asm.RAX)

	e.MovRR32(asm.RSI, asm.R14)
	e.AddRR(asm.RSI, asm.R15)
	e.MovRR(asm.RDI, asm.RBX)
	e.AddRR(asm.RDI, asm.R15)

	e.LoadMem32ZX(asm.RCX, asm.RSI, int64(-HeapBlockHeaderSize+4))
	e.CmpRR(asm.RCX, asm.R12)
	useOld := e.JbRel32()
	e.MovRR32(asm.RCX, asm.R12)
	e.Patch32(useOld, e.Pos())
	e.RepMovsb()

	e.MovRR32(asm.RDI, asm.R14)
	e.callSym("__vertex_memory_heap_free")

	e.MovRR(asm.RAX, asm.RBX)
	done := e.JmpRel32()

	e.Patch32(isNull, e.Pos())
	e.MovRR32(asm.RDI, asm.R12)
	e.callSym("__vertex_memory_heap_alloc")
	done1 := e.JmpRel32()

	e.Patch32(isZeroSize, e.Pos())
	e.MovRR32(asm.RDI, asm.R14)
	e.callSym("__vertex_memory_heap_free")
	e.XorRR32(asm.RAX)

	e.Patch32(done, e.Pos())
	e.Patch32(done1, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R14}, align)
}