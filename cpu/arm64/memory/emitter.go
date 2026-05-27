// cpu/arm64/memory/emitter.go
package memory

import (
	"fmt"

	"github.com/vertex-language/compiler/cpu/arm64/asm"
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

func (e *emitter) callSym(sym string) {
	off := e.Pos()
	e.Emit32(0x94000000)
	e.relocs = append(e.relocs, object.Reloc{
		Section: ".text", Offset: uint32(e.textBase + off), Symbol: sym, Kind: object.RelocCall32,
	})
}

func (e *emitter) bSym(sym string) {
	off := e.Pos()
	e.Emit32(0x14000000)
	e.relocs = append(e.relocs, object.Reloc{
		Section: ".text", Offset: uint32(e.textBase + off), Symbol: sym, Kind: object.RelocCall32,
	})
}

func (e *emitter) loadSymAddr(reg int, sym string) {
	off := e.Pos()
	e.Emit32(0x90000000 | asm.Rd(reg))
	e.Emit32(0x91000000 | asm.Rn(reg) | asm.Rd(reg))
	e.relocs = append(e.relocs,
		object.Reloc{Section: ".text", Offset: uint32(e.textBase + off), Symbol: sym, Kind: object.RelocADRP},
		object.Reloc{Section: ".text", Offset: uint32(e.textBase + off + 4), Symbol: sym, Kind: object.RelocADRPOff12Add},
	)
}

func (e *emitter) loadSym64(reg int, sym string) {
	off := e.Pos()
	e.Emit32(0x90000000 | asm.Rd(reg))
	e.Emit32(0xF9400000 | asm.Rn(reg) | asm.Rd(reg))
	e.relocs = append(e.relocs,
		object.Reloc{Section: ".text", Offset: uint32(e.textBase + off), Symbol: sym, Kind: object.RelocADRP},
		object.Reloc{Section: ".text", Offset: uint32(e.textBase + off + 4), Symbol: sym, Kind: object.RelocADRPOff12Load},
	)
}

func (e *emitter) initCheck(stateReg, tmp int) {
	e.loadSymAddr(stateReg, "__vertex_alloc_state")
	e.LDR64(tmp, stateReg, uint32(StateHeapBase))
	e.CMPI(tmp, 0)
	p := e.BCond(asm.CondNE)
	e.callSym("__vertex_memory_init")
	e.loadSymAddr(stateReg, "__vertex_alloc_state")
	e.PatchCondImm19(p, e.Pos())
}

func (e *emitter) emitMemcpy(dst, src, countReg int) {
	loop := e.Pos()
	end := e.CBZ(countReg)
	e.LDRB(asm.X9, src, 0)
	e.ADDSI(src, src, 1)
	e.STRB(asm.X9, dst, 0)
	e.ADDSI(dst, dst, 1)
	e.SUBSI(countReg, countReg, 1)
	e.BBack(loop)
	e.PatchCondImm19(end, e.Pos())
}

func (e *emitter) emitMemset(dst, valReg, countReg int) {
	loop := e.Pos()
	end := e.CBZ(countReg)
	e.STRB(valReg, dst, 0)
	e.ADDSI(dst, dst, 1)
	e.SUBSI(countReg, countReg, 1)
	e.BBack(loop)
	e.PatchCondImm19(end, e.Pos())
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