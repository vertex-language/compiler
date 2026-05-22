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
	// Four callee-saved registers: RBX=new_base, R12=heap_base, R13=state/scratch,
	// R14=scratch for leaRIPSym loads.
	// total = 1(rbp) + 4(regs) = 5 pushes → RSP already 16-byte aligned; align=0.
	align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13, asm.R14})

	// ── Idempotency guard ─────────────────────────────────────────────────────
	// Both the function prologue and initCheck can race to call us; only the
	// first arrival does work.  If __wasm_mem_base is already non-zero, return.
	e.leaRIPSym(asm.R13, "__wasm_mem_base")
	e.LoadMem64(asm.RAX, asm.R13, 0)
	e.TestRR64(asm.RAX)
	alreadyDone := e.JnzRel32()

	// ── Allocate contiguous wasm address space ────────────────────────────────
	// One anonymous mapping covers:
	//   [0 .. HeapOffset)             wasm linear memory (data segments land here)
	//   [HeapOffset .. ArenaOffset)   heap
	//   [ArenaOffset .. total)        arena
	//
	// Using MmapAnon (addr=NULL) means the kernel picks a free VA range; no
	// MAP_FIXED conflict is possible regardless of ASLR or library placement.
	totalSize := uint32(HeapOffset + HeapSize + ArenaSize)
	e.MovRI32(asm.RSI, totalSize)
	e.MmapAnon()
	failM := e.CheckMmapError()
	e.MovRR(asm.RBX, asm.RAX) // RBX = new wasm base (= new R15)

	// ── Copy static data from .data into the new region ───────────────────────
	// Source: __wasm_data_base + 65536  (where emitDataSegments wrote data segs)
	// Dest:   new base                  (= wasm linear-memory byte 0)
	// Count:  __wasm_static_bytes       (written by x86_64 emitDataSegments)
	e.leaRIPSym(asm.RSI, "__wasm_data_base")
	e.AddRI(asm.RSI, 65536)
	e.MovRR(asm.RDI, asm.RBX)
	e.leaRIPSym(asm.R14, "__wasm_static_bytes")
	e.LoadMem32ZX(asm.RCX, asm.R14, 0)
	e.RepMovsb()

	// ── Publish the new base ──────────────────────────────────────────────────
	// R13 still holds &__wasm_mem_base from the idempotency check.
	// Every compiled function prologue reads this to load R15.
	e.StoreMem64(asm.R13, 0, asm.RBX)

	// ── Initialise allocator state ────────────────────────────────────────────
	e.leaRIPSym(asm.R13, "__vertex_alloc_state")

	// Heap: [base + HeapOffset, base + HeapOffset + HeapSize)
	e.MovRR(asm.R12, asm.RBX)
	e.AddRI(asm.R12, HeapOffset)
	e.StoreMem64(asm.R13, StateHeapBase, asm.R12)
	e.StoreMem64(asm.R13, StateHeapCur, asm.R12)
	e.MovRR(asm.RAX, asm.R12)
	e.AddRI(asm.RAX, HeapSize)
	e.StoreMem64(asm.R13, StateHeapEnd, asm.RAX)

	// Arena: [base + ArenaOffset, base + ArenaOffset + ArenaSize)
	e.MovRR(asm.RAX, asm.RBX)
	e.AddRI(asm.RAX, ArenaOffset)
	e.StoreMem64(asm.R13, StateArenaBase, asm.RAX)
	e.StoreMem64(asm.R13, StateArenaCur, asm.RAX)
	e.AddRI(asm.RAX, ArenaSize)
	e.StoreMem64(asm.R13, StateArenaEnd, asm.RAX)
	e.StoreMem64Zero(asm.R13, StateArenaSP)

	// Both the early-return and the initialisation path share one epilogue.
	e.Patch32(alreadyDone, e.Pos())
	e.Epilogue([]int{asm.RBX, asm.R12, asm.R13, asm.R14}, align)

	// ── Fatal: mmap failed ────────────────────────────────────────────────────
	e.Patch32(failM, e.Pos())
	e.ExitGroup(127)
}