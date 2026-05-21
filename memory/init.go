package memory

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitInit emits __vertex_memory_init.
//
// Called lazily on the first allocation. Uses MAP_FIXED to place the heap and
// arena regions at deterministic offsets from the wasm linear-memory base (R15
// at runtime), so every returned pointer is a valid wasm i32 offset.
//
// Register map:
//
//	rbx = __wasm_data_base + 65536  (≡ R15 at runtime)
//	r12 = heap native base address
//	r13 = &__vertex_alloc_state
func emitInit(e *emitter) {
	e.codeLabel("__vertex_memory_init")
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

	// rbx = wasm linear-memory base (matches R15 in generated wasm functions)
	e.leaRIPSym(asm.RBX, "__wasm_data_base")
	e.AddRI(asm.RBX, 65536)

	// ── Map heap region ───────────────────────────────────────────────────────
	e.MovRR(asm.RDI, asm.RBX)
	e.AddRI(asm.RDI, HeapOffset)
	e.MmapFixed(HeapSize)
	failH := e.CheckMmapError()
	e.MovRR(asm.R12, asm.RAX) // r12 = heap_base

	e.leaRIPSym(asm.R13, "__vertex_alloc_state")
	e.StoreMem64(asm.R13, StateHeapBase, asm.R12)
	e.StoreMem64(asm.R13, StateHeapCur, asm.R12) // bump pointer starts at base
	e.MovRR(asm.RAX, asm.R12)
	e.AddRI(asm.RAX, HeapSize)
	e.StoreMem64(asm.R13, StateHeapEnd, asm.RAX)

	// ── Map arena region ──────────────────────────────────────────────────────
	e.MovRR(asm.RDI, asm.RBX)
	e.AddRI(asm.RDI, ArenaOffset)
	e.MmapFixed(ArenaSize)
	failA := e.CheckMmapError()

	e.StoreMem64(asm.R13, StateArenaBase, asm.RAX)
	e.StoreMem64(asm.R13, StateArenaCur, asm.RAX) // bump pointer starts at base
	e.AddRI(asm.RAX, ArenaSize)
	e.StoreMem64(asm.R13, StateArenaEnd, asm.RAX)
	e.StoreMem64Zero(asm.R13, StateArenaSP)

	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)

	// ── Fatal: mmap failed ────────────────────────────────────────────────────
	fatal := e.Pos()
	e.Patch32(failH, fatal)
	e.Patch32(failA, fatal)
	e.ExitGroup(127)
}