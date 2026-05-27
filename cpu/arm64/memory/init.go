// cpu/arm64/memory/init.go
package memory

import (
	"github.com/vertex-language/compiler/cpu/arm64/asm"
)

func emitInit(e *emitter) {
	e.codeLabel("__vertex_memory_init")
	frameSize := e.Prologue([]int{asm.X19, asm.X20, asm.X21})

	e.loadSym64(asm.X0, "__wasm_mem_base")
	e.CMPI(asm.X0, 0)
	alreadyDone := e.BCond(asm.CondNE)

	totalSize := uint32(HeapOffset + HeapSize + ArenaSize)
	e.MOVZ32(asm.X1, uint16(totalSize), 0)
	if totalSize > 0xFFFF {
		e.MOVK(asm.X1, uint16(totalSize>>16), 1)
	}
	e.MmapAnon()
	failM := e.CheckMmapError()
	e.MovRR(asm.X19, asm.X0)

	e.loadSymAddr(asm.X1, "__wasm_data_base")
	e.ADDSI(asm.X1, asm.X1, 65536)
	e.MovRR(asm.X0, asm.X19)
	e.MOVZ32(asm.X2, uint16(e.staticBytes), 0)
	if e.staticBytes > 0xFFFF {
		e.MOVK(asm.X2, uint16(e.staticBytes>>16), 1)
	}
	e.emitMemcpy(asm.X0, asm.X1, asm.X2)

	e.loadSymAddr(asm.X1, "__wasm_mem_base")
	e.STR64(asm.X19, asm.X1, 0)

	e.loadSymAddr(asm.X21, "__vertex_alloc_state")

	e.MovRR(asm.X20, asm.X19)
	e.ADDSI(asm.X20, asm.X20, uint32(HeapOffset))
	e.STR64(asm.X20, asm.X21, uint32(StateHeapBase))
	e.STR64(asm.X20, asm.X21, uint32(StateHeapCur))
	e.MovRR(asm.X0, asm.X20)
	e.ADDSI(asm.X0, asm.X0, uint32(HeapSize))
	e.STR64(asm.X0, asm.X21, uint32(StateHeapEnd))

	e.MovRR(asm.X0, asm.X19)
	e.ADDSI(asm.X0, asm.X0, uint32(ArenaOffset))
	e.STR64(asm.X0, asm.X21, uint32(StateArenaBase))
	e.STR64(asm.X0, asm.X21, uint32(StateArenaCur))
	e.ADDSI(asm.X0, asm.X0, uint32(ArenaSize))
	e.STR64(asm.X0, asm.X21, uint32(StateArenaEnd))
	e.STR64(asm.XZR, asm.X21, uint32(StateArenaSP))

	e.PatchCondImm19(alreadyDone, e.Pos())
	e.Epilogue([]int{asm.X19, asm.X20, asm.X21}, frameSize)

	e.PatchCondImm19(failM, e.Pos())
	e.ExitGroup(127)
}