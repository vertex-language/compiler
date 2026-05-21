package concurrency

import (
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
	"github.com/vertex-language/compiler/object"
)

// emitter wraps asm.Assembler with symbol and relocation tracking, 
// flushing directly into a shared WasmObj.
type emitter struct {
	asm.Assembler
	obj        *object.WasmObj
	codeOffset int
	syms       []object.Symbol
	relocs     []object.Reloc
}

func newEmitter(obj *object.WasmObj) *emitter {
	return &emitter{
		obj:        obj,
		codeOffset: len(obj.Code), // Offset against existing code in the shared obj
	}
}

// codeLabel records the current code position as a named defined symbol.
func (e *emitter) codeLabel(name string) {
	e.syms = append(e.syms, object.Symbol{
		Name:   name,
		Kind:   object.SymDefined,
		Offset: e.codeOffset + e.Pos(),
	})
}

// rel32Sym appends a 4-byte zero placeholder and records a RelocRel32.
func (e *emitter) rel32Sym(sym string) {
	e.relocs = append(e.relocs, object.Reloc{
		Offset: e.codeOffset + e.Pos(),
		Symbol: sym,
		Kind:   object.RelocRel32,
	})
	e.Emit(0, 0, 0, 0)
}

// leaRIPSym emits: lea dst, [rip + sym]
func (e *emitter) leaRIPSym(dst int, sym string) {
	e.Emit(asm.REXW(dst, -1), 0x8D, byte(0x05|((dst&7)<<3)))
	e.rel32Sym(sym)
}

// callSym emits: call sym  (RIP-relative)
func (e *emitter) callSym(sym string) {
	e.Emit(0xE8)
	e.rel32Sym(sym)
}

// jmpSym emits: jmp sym  (RIP-relative; used for tail calls)
func (e *emitter) jmpSym(sym string) {
	e.Emit(0xE9)
	e.rel32Sym(sym)
}

// flush appends all accumulated code, symbols, and relocs into the shared obj.
func (e *emitter) flush() {
	e.obj.Code = append(e.obj.Code, e.Bytes()...)
	e.obj.Symbols = append(e.obj.Symbols, e.syms...)
	e.obj.Relocs = append(e.obj.Relocs, e.relocs...)
}