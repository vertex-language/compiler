package memory

import (
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
	"github.com/vertex-language/compiler/object"
)

// emitter builds the allocator object and flushes it directly into a shared WasmObj.
type emitter struct {
	asm.Assembler                 // all x86-64 instruction methods live here
	obj        *object.WasmObj
	codeOffset int
	dataOffset int
	syms       []object.Symbol
	relocs     []object.Reloc
	data       []byte
}

func newEmitter(obj *object.WasmObj) *emitter {
	return &emitter{
		obj:        obj,
		codeOffset: len(obj.Code), // Offset against any existing code in the shared obj
		dataOffset: len(obj.Data), // Offset against any existing data in the shared obj
	}
}

// ── Symbol / relocation accounting ───────────────────────────────────────────

// codeLabel records the current code position as a named defined symbol.
func (e *emitter) codeLabel(name string) {
	e.syms = append(e.syms, object.Symbol{
		Name:   name,
		Kind:   object.SymDefined,
		Offset: e.codeOffset + e.Pos(),
	})
}

// dataLabel records the current data offset as a named defined symbol in .data.
func (e *emitter) dataLabel(name string) {
	e.syms = append(e.syms, object.Symbol{
		Name:    name,
		Kind:    object.SymDefined,
		Offset:  e.dataOffset + len(e.data),
		Section: object.SymSecData,
	})
}

// dataZero appends n zeroed bytes to the data section.
func (e *emitter) dataZero(n int) {
	e.data = append(e.data, make([]byte, n)...)
}

// rel32Sym emits a 4-byte zero placeholder and records a RelocRel32 against sym.
func (e *emitter) rel32Sym(sym string) {
	e.relocs = append(e.relocs, object.Reloc{
		Offset: e.codeOffset + e.Pos(),
		Symbol: sym,
		Kind:   object.RelocRel32,
	})
	e.Emit(0, 0, 0, 0)
}

// ── RIP-relative helpers ──────────────────────────────────────────────────────

// leaRIPSym emits: lea dst, [rip + sym]
func (e *emitter) leaRIPSym(dst int, sym string) {
	e.Emit(asm.REXW(dst, -1), 0x8D, byte(0x05|((dst&7)<<3)))
	e.rel32Sym(sym)
}

// callSym emits: call sym  (RIP-relative call to a named symbol)
func (e *emitter) callSym(sym string) {
	e.Emit(0xE8)
	e.rel32Sym(sym)
}

// jmpSym emits: jmp sym  (RIP-relative unconditional jump; useful for tail calls)
func (e *emitter) jmpSym(sym string) {
	e.Emit(0xE9)
	e.rel32Sym(sym)
}

// ── Lazy-init guard ───────────────────────────────────────────────────────────

// initCheck emits the lazy-init guard at the top of every stub that touches
// the heap or arena.
func (e *emitter) initCheck(stateReg, tmp int) {
	e.leaRIPSym(stateReg, "__vertex_alloc_state")
	e.LoadMem64(tmp, stateReg, StateHeapBase)
	e.TestRR64(tmp)
	p := e.JnzRel32()
	e.callSym("__vertex_memory_init")
	e.leaRIPSym(stateReg, "__vertex_alloc_state")
	e.Patch32(p, e.Pos())
}

// ── Object assembly ───────────────────────────────────────────────────────────

// flush appends all accumulated code, data, symbols, and relocations directly 
// into the shared build context object.
func (e *emitter) flush() {
	e.obj.Code = append(e.obj.Code, e.Bytes()...)
	e.obj.Data = append(e.obj.Data, e.data...)
	e.obj.Symbols = append(e.obj.Symbols, e.syms...)
	e.obj.Relocs = append(e.obj.Relocs, e.relocs...)
}