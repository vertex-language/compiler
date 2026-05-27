package memory

import (
	"github.com/vertex-language/compiler/cpu/amd64/asm"
)

func emitArenaPush(e *emitter) {
	e.codeLabel("__vertex_memory_arena_push")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	e.LoadMem64(asm.RAX, asm.R13, StateArenaSP)

	e.CmpRI(asm.RAX, MaxArenaDepth)
	ok := e.JbRel32()
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())

	e.LoadMem64(asm.RCX, asm.R13, StateArenaCur)

	e.MovRR(asm.RDX, asm.R13)
	e.AddRI(asm.RDX, StateArenaStack)
	e.LeaScale(asm.RDX, asm.RDX, asm.RAX, 8)
	e.StoreMem64(asm.RDX, 0, asm.RCX)

	e.AddRI(asm.RAX, 1)
	e.StoreMem64(asm.R13, StateArenaSP, asm.RAX)

	e.Epilogue([]int{asm.R13}, align)
}

func emitArenaPop(e *emitter) {
	e.codeLabel("__vertex_memory_arena_pop")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	e.LoadMem64(asm.RAX, asm.R13, StateArenaSP)

	e.TestRR64(asm.RAX)
	done := e.JzRel32()

	e.AddRI(asm.RAX, -1)
	e.StoreMem64(asm.R13, StateArenaSP, asm.RAX)

	e.MovRR(asm.RDX, asm.R13)
	e.AddRI(asm.RDX, StateArenaStack)
	e.LeaScale(asm.RDX, asm.RDX, asm.RAX, 8)
	e.LoadMem64(asm.RCX, asm.RDX, 0)
	e.StoreMem64(asm.R13, StateArenaCur, asm.RCX)

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.R13}, align)
}

func emitArenaAlloc(e *emitter) {
	e.codeLabel("__vertex_memory_arena_alloc")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	e.AddRI(asm.RDI, 7)
	e.AndRI8(asm.RDI, -8)

	e.LoadMem64(asm.RAX, asm.R13, StateArenaCur)

	e.MovRR(asm.RCX, asm.RAX)
	e.AddRR(asm.RCX, asm.RDI)

	e.LoadMem64(asm.RDX, asm.R13, StateArenaEnd)
	e.CmpRR(asm.RCX, asm.RDX)
	ok := e.JbeRel32()
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())

	e.StoreMem64(asm.R13, StateArenaCur, asm.RCX)

	e.SubRR(asm.RAX, asm.R15)

	e.Epilogue([]int{asm.R13}, align)
}