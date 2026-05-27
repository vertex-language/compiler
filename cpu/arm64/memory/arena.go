// cpu/arm64/memory/arena.go
package memory

import (
	"github.com/vertex-language/compiler/cpu/arm64/asm"
)

func emitArenaPush(e *emitter) {
	e.codeLabel("__vertex_memory_arena_push")
	frameSize := e.Prologue([]int{asm.X19})

	e.initCheck(asm.X19, asm.X9)

	e.LDR64(asm.X0, asm.X19, uint32(StateArenaSP))
	e.CMPI(asm.X0, uint32(MaxArenaDepth))
	ok := e.BCond(asm.CondLS)
	e.ExitGroup(127)
	e.PatchCondImm19(ok, e.Pos())

	e.LDR64(asm.X1, asm.X19, uint32(StateArenaCur))

	e.MovRR(asm.X2, asm.X19)
	e.ADDSI(asm.X2, asm.X2, uint32(StateArenaStack))
	e.LSLI(asm.X9, asm.X0, 3)
	e.ADD(asm.X2, asm.X2, asm.X9)
	e.STR64(asm.X1, asm.X2, 0)

	e.ADDSI(asm.X0, asm.X0, 1)
	e.STR64(asm.X0, asm.X19, uint32(StateArenaSP))

	e.Epilogue([]int{asm.X19}, frameSize)
}

func emitArenaPop(e *emitter) {
	e.codeLabel("__vertex_memory_arena_pop")
	frameSize := e.Prologue([]int{asm.X19})

	e.initCheck(asm.X19, asm.X9)

	e.LDR64(asm.X0, asm.X19, uint32(StateArenaSP))
	e.CMPI(asm.X0, 0)
	done := e.BCond(asm.CondEQ)

	e.SUBSI(asm.X0, asm.X0, 1)
	e.STR64(asm.X0, asm.X19, uint32(StateArenaSP))

	e.MovRR(asm.X2, asm.X19)
	e.ADDSI(asm.X2, asm.X2, uint32(StateArenaStack))
	e.LSLI(asm.X9, asm.X0, 3)
	e.ADD(asm.X2, asm.X2, asm.X9)
	e.LDR64(asm.X1, asm.X2, 0)
	e.STR64(asm.X1, asm.X19, uint32(StateArenaCur))

	e.PatchCondImm19(done, e.Pos())
	e.Epilogue([]int{asm.X19}, frameSize)
}

func emitArenaAlloc(e *emitter) {
	e.codeLabel("__vertex_memory_arena_alloc")
	frameSize := e.Prologue([]int{asm.X19})

	e.initCheck(asm.X19, asm.X9)

	e.ADDSI(asm.X0, asm.X0, 7)
	e.AND32(asm.X0, asm.X0, -8)

	e.LDR64(asm.X1, asm.X19, uint32(StateArenaCur))
	e.MovRR(asm.X2, asm.X1)
	e.ADD(asm.X2, asm.X2, asm.X0)

	e.LDR64(asm.X3, asm.X19, uint32(StateArenaEnd))
	e.CMP(asm.X2, asm.X3)
	ok := e.BCond(asm.CondLS)
	e.ExitGroup(127)
	e.PatchCondImm19(ok, e.Pos())

	e.STR64(asm.X2, asm.X19, uint32(StateArenaCur))

	e.MovRR(asm.X0, asm.X1)
	e.SUB(asm.X0, asm.X0, asm.MemBase)

	e.Epilogue([]int{asm.X19}, frameSize)
}