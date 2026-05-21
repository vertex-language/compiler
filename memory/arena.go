package memory

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// emitArenaPush emits __vertex_memory_arena_push().
//
// Saves the current arena bump pointer onto the checkpoint stack.
// Exits with code 127 if the stack is already at maximum depth.
func emitArenaPush(e *emitter) {
	e.codeLabel("__vertex_memory_arena_push")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	// rax = arena_sp
	e.LoadMem64(asm.RAX, asm.R13, StateArenaSP)

	// Depth check: if sp >= MaxArenaDepth, fatal.
	e.CmpRI(asm.RAX, MaxArenaDepth)
	ok := e.JbRel32() // sp < MaxArenaDepth
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())

	// rcx = arena_cur (the value we checkpoint)
	e.LoadMem64(asm.RCX, asm.R13, StateArenaCur)

	// arena_stack[sp] = arena_cur
	// Compute address: rdx = &arena_stack[sp] = r13 + StateArenaStack + sp*8
	e.MovRR(asm.RDX, asm.R13)
	e.AddRI(asm.RDX, StateArenaStack)
	e.LeaScale(asm.RDX, asm.RDX, asm.RAX, 8)
	e.StoreMem64(asm.RDX, 0, asm.RCX)

	// arena_sp++
	e.AddRI(asm.RAX, 1)
	e.StoreMem64(asm.R13, StateArenaSP, asm.RAX)

	e.Epilogue([]int{asm.R13}, align)
}

// emitArenaPop emits __vertex_memory_arena_pop().
//
// Restores the arena bump pointer from the checkpoint stack, reclaiming all
// allocations made since the matching push. No-op if the stack is empty.
func emitArenaPop(e *emitter) {
	e.codeLabel("__vertex_memory_arena_pop")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	// rax = arena_sp
	e.LoadMem64(asm.RAX, asm.R13, StateArenaSP)

	// Underflow guard: if sp == 0, nothing to pop.
	e.TestRR64(asm.RAX)
	done := e.JzRel32()

	// --sp
	e.AddRI(asm.RAX, -1)
	e.StoreMem64(asm.R13, StateArenaSP, asm.RAX)

	// arena_cur = arena_stack[sp]
	e.MovRR(asm.RDX, asm.R13)
	e.AddRI(asm.RDX, StateArenaStack)
	e.LeaScale(asm.RDX, asm.RDX, asm.RAX, 8)
	e.LoadMem64(asm.RCX, asm.RDX, 0)
	e.StoreMem64(asm.R13, StateArenaCur, asm.RCX)

	e.Patch32(done, e.Pos())
	e.Epilogue([]int{asm.R13}, align)
}

// emitArenaAlloc emits __vertex_memory_arena_alloc(size i32) → i32.
//
// Bump-allocates from the arena region. Exits with 127 on OOM.
// Returns the wasm offset of the allocated region.
func emitArenaAlloc(e *emitter) {
	e.codeLabel("__vertex_memory_arena_alloc")
	align := e.Prologue([]int{asm.R13})

	e.initCheck(asm.R13, asm.RAX)

	// Round size up to 8-byte alignment.
	e.AddRI(asm.RDI, 7)
	e.AndRI8(asm.RDI, -8)

	// rax = arena_cur (our block)
	e.LoadMem64(asm.RAX, asm.R13, StateArenaCur)

	// rcx = new_cur = arena_cur + size
	e.MovRR(asm.RCX, asm.RAX)
	e.AddRR(asm.RCX, asm.RDI)

	// OOM check: new_cur > arena_end
	e.LoadMem64(asm.RDX, asm.R13, StateArenaEnd)
	e.CmpRR(asm.RCX, asm.RDX)
	ok := e.JbeRel32()
	e.ExitGroup(127)
	e.Patch32(ok, e.Pos())

	// Commit.
	e.StoreMem64(asm.R13, StateArenaCur, asm.RCX)

	// Return wasm offset = rax − R15.
	e.SubRR(asm.RAX, asm.R15)

	e.Epilogue([]int{asm.R13}, align)
}