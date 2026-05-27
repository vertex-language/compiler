// cpu/amd64/memory/init.go
package memory

import (
	"github.com/vertex-language/compiler/cpu/amd64/asm"
)

func emitInit(e *emitter) {
	e.codeLabel("__vertex_memory_init")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

	e.leaRIPSym(asm.R13, "__wasm_mem_base")
	e.LoadMem64(asm.RAX, asm.R13, 0)
	e.TestRR64(asm.RAX)
	alreadyDone := e.JnzRel32()

	// NOTICE: No more mc. prefix here!
	totalSize := uint32(HeapOffset + HeapSize + ArenaSize)
	e.MovRI32(asm.RSI, totalSize)
	e.MmapAnon()
	failM := e.CheckMmapError()
	e.MovRR(asm.RBX, asm.RAX)

	e.leaRIPSym(asm.RSI, "__wasm_data_base")
	e.AddRI(asm.RSI, 65536)
	e.MovRR(asm.RDI, asm.RBX)
	e.MovRI32(asm.RCX, e.staticBytes)
	e.RepMovsb()

	e.StoreMem64(asm.R13, 0, asm.RBX)

	e.leaRIPSym(asm.R13, "__vertex_alloc_state")

	e.MovRR(asm.R12, asm.RBX)
	
	// NOTICE: No more mc. prefix here!
	e.AddRI(asm.R12, HeapOffset)
	e.StoreMem64(asm.R13, StateHeapBase, asm.R12)
	e.StoreMem64(asm.R13, StateHeapCur, asm.R12)
	e.MovRR(asm.RAX, asm.R12)
	e.AddRI(asm.RAX, HeapSize)
	e.StoreMem64(asm.R13, StateHeapEnd, asm.RAX)

	e.MovRR(asm.RAX, asm.RBX)
	e.AddRI(asm.RAX, ArenaOffset)
	e.StoreMem64(asm.R13, StateArenaBase, asm.RAX)
	e.StoreMem64(asm.R13, StateArenaCur, asm.RAX)
	e.AddRI(asm.RAX, ArenaSize)
	e.StoreMem64(asm.R13, StateArenaEnd, asm.RAX)
	e.StoreMem64Zero(asm.R13, StateArenaSP)

	e.Patch32(alreadyDone, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)

	e.Patch32(failM, e.Pos())
	e.ExitGroup(127)
}