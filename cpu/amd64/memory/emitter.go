package memory

import (
	"fmt"

	"github.com/vertex-language/compiler/cpu/amd64/asm"
	"github.com/vertex-language/compiler/object"
)

type emitter struct {
	asm.Assembler

	obj      object.Object
	textBase int
	dataBase int

	staticBytes uint32

	data   []byte
	syms   []object.Symbol
	relocs []object.Reloc
}

func newEmitter(obj object.Object, staticBytes uint32) *emitter {
	return &emitter{
		obj:         obj,
		textBase:    obj.Text().Len(),
		dataBase:    obj.Data().Len(),
		staticBytes: staticBytes,
	}
}

func (e *emitter) codeLabel(name string) {
	e.syms = append(e.syms, object.Symbol{
		Name:       name,
		Section:    ".text",
		Offset:     uint64(e.textBase + e.Pos()),
		Global:     true,
		IsFunction: true,
	})
}

func (e *emitter) dataLabel(name string) {
	e.syms = append(e.syms, object.Symbol{
		Name:    name,
		Section: ".data",
		Offset:  uint64(e.dataBase + len(e.data)),
		Global:  true,
	})
}

func (e *emitter) dataZero(n int) {
	e.data = append(e.data, make([]byte, n)...)
}

func (e *emitter) callRelSym(sym string) {
	e.relocs = append(e.relocs, object.Reloc{
		Section: ".text",
		Offset:  uint32(e.textBase + e.Pos()),
		Symbol:  sym,
		Kind:    object.RelocCall32,
		Addend:  -4,
	})
	e.Emit(0, 0, 0, 0)
}

func (e *emitter) dataRelSym(sym string) {
	e.relocs = append(e.relocs, object.Reloc{
		Section: ".text",
		Offset:  uint32(e.textBase + e.Pos()),
		Symbol:  sym,
		Kind:    object.RelocPCRel32,
		Addend:  -4,
	})
	e.Emit(0, 0, 0, 0)
}

func (e *emitter) leaRIPSym(dst int, sym string) {
	e.Emit(asm.REXW(dst, -1), 0x8D, byte(0x05|((dst&7)<<3)))
	e.dataRelSym(sym)
}

func (e *emitter) callSym(sym string) {
	e.Emit(0xE8)
	e.callRelSym(sym)
}

func (e *emitter) jmpSym(sym string) {
	e.Emit(0xE9)
	e.callRelSym(sym)
}

func (e *emitter) initCheck(stateReg, tmp int) {
	e.leaRIPSym(stateReg, "__vertex_alloc_state")
	e.LoadMem64(tmp, stateReg, StateHeapBase)
	e.TestRR64(tmp)
	p := e.JnzRel32()
	e.callSym("__vertex_memory_init")
	e.leaRIPSym(stateReg, "__vertex_alloc_state")
	e.Patch32(p, e.Pos())
}

func (e *emitter) flush() error {
	if _, err := e.obj.Text().Write(e.Bytes()); err != nil {
		return fmt.Errorf("memory: writing text section: %w", err)
	}
	if _, err := e.obj.Data().Write(e.data); err != nil {
		return fmt.Errorf("memory: writing data section: %w", err)
	}
	for _, s := range e.syms {
		e.obj.AddSymbol(s)
	}
	for _, r := range e.relocs {
		e.obj.AddReloc(r)
	}
	return nil
}