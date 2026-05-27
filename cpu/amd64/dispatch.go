// cpu/amd64/dispatch.go
package amd64

import (
	"encoding/binary"
	"fmt"

	"github.com/vertex-language/compiler/decode"
)

func (fc *funcCompiler) dispatchOp(r *decode.Reader, op byte) error {
	if op >= 0x45 && op <= 0xC4 {
		return fc.dispatchMath(op)
	}

	switch op {

	case 0x00: // unreachable
		fc.Emit(0x0F, 0x0B)
		fc.dead = true
		fc.deadDepth = 0

	case 0x01: // nop

	case 0x02: // block
		pArity, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind: ctrlBlock, arity: rArity, paramArity: pArity,
			baseDepth: fc.depth - pArity,
		})

	case 0x03: // loop
		pArity, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind: ctrlLoop, arity: rArity, paramArity: pArity,
			baseDepth: fc.depth - pArity, loopTarget: fc.Pos(),
		})

	case 0x04: // if
		_, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.Emit(0x85, 0xC0)
		fc.Emit(0x0F, 0x84)
		elseOff := fc.ZeroRel32()
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind: ctrlIf, arity: rArity,
			baseDepth: fc.depth, elseJmpOff: elseOff,
		})

	case 0x05: // else
		if len(fc.ctrl) == 0 {
			return fmt.Errorf("else without if")
		}
		top := &fc.ctrl[len(fc.ctrl)-1]
		if top.kind != ctrlIf {
			return fmt.Errorf("else must follow if block")
		}
		fc.Emit(0xE9)
		endPatch := fc.ZeroRel32()
		top.endPatches = append(top.endPatches, endPatch)
		fc.Patch32(top.elseJmpOff, fc.Pos())
		top.elseJmpOff = 0
		fc.depth = top.baseDepth

	case 0x0B: // end
		if len(fc.ctrl) == 0 {
			return fmt.Errorf("end without matching block")
		}
		frame := fc.ctrl[len(fc.ctrl)-1]
		fc.ctrl = fc.ctrl[:len(fc.ctrl)-1]
		if frame.kind == ctrlIf && frame.elseJmpOff != 0 {
			fc.Patch32(frame.elseJmpOff, fc.Pos())
		}
		for _, p := range frame.endPatches {
			fc.Patch32(p, fc.Pos())
		}
		if len(fc.ctrl) == 0 {
			fc.emitEpilogue()
			fc.dead = true
		}

	case 0x0C: // br
		l, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitBr(int(l))
		fc.dead = true
		fc.deadDepth = 0

	case 0x0D: // br_if
		l, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.Emit(0x85, 0xC0)
		fc.Emit(0x0F, 0x85)
		fc.addBrPatch(int(l), fc.ZeroRel32())

	case 0x0E: // br_table
		n, err := r.ReadU32()
		if err != nil {
			return err
		}
		targets := make([]uint32, n+1)
		for i := range targets {
			if targets[i], err = r.ReadU32(); err != nil {
				return err
			}
		}
		fc.emitBrTable(targets)
		fc.dead = true
		fc.deadDepth = 0

	case 0x0F: // return
		fc.emitReturn()
		fc.dead = true
		fc.deadDepth = 0

	case 0x10: // call
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		return fc.emitCall(int(idx))

	case 0x11: // call_indirect
		typeIdx, err := r.ReadU32()
		if err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fc.emitCallIndirect(typeIdx)

	case 0x1A: // drop
		fc.emitAddRSP(8)
		fc.depth--

	case 0x1B: // select
		fc.emitSelect()

	case 0x1C: // select t
		n, err := r.ReadU32()
		if err != nil {
			return err
		}
		for i := uint32(0); i < n; i++ {
			if _, err := r.ReadByte(); err != nil {
				return err
			}
		}
		fc.emitSelect()

	case 0x20: // local.get
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitLoadLocal64(RAX, int(idx))
		fc.emitPushR(RAX)

	case 0x21: // local.set
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.emitStoreLocal64(int(idx), RAX)

	case 0x22: // local.tee
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPeekR(RAX)
		fc.emitStoreLocal64(int(idx), RAX)

	case 0x23: // global.get
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitGlobalGet(int(idx))

	case 0x24: // global.set
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitGlobalSet(int(idx))

	case 0x28:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad32zx(off)
	case 0x29:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad64(off)
	case 0x2A:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad32zx(off)
	case 0x2B:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad64(off)
	case 0x2C:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 1, true)
	case 0x2D:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 1, false)
	case 0x2E:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 2, true)
	case 0x2F:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 2, false)
	case 0x30:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 1, true)
	case 0x31:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 1, false)
	case 0x32:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 2, true)
	case 0x33:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 2, false)
	case 0x34:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 4, true)
	case 0x35:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 4, false)

	case 0x36:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)
	case 0x37:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 8)
	case 0x38:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)
	case 0x39:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 8)
	case 0x3A:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 1)
	case 0x3B:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 2)
	case 0x3C:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 1)
	case 0x3D:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 2)
	case 0x3E:
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)

	case 0x3F: // memory.size
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		pages := uint32(0)
		if fc.ctx.Module.Memories.Len() > 0 {
			pages = fc.ctx.Module.Memories.Entries[0].Lim.Min
		}
		fc.Emit(0x68)
		fc.Emit32(pages)
		fc.depth++

	case 0x40: // memory.grow — stub: always returns -1
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.Emit(0x6A, 0xFF)

	case 0x41: // i32.const
		v, err := r.ReadS32()
		if err != nil {
			return err
		}
		fc.Emit(0x68)
		fc.Emit32(uint32(v))
		fc.depth++

	case 0x42: // i64.const
		v, err := r.ReadSLEB128()
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8)
		fc.Emit64(uint64(v))
		fc.emitPushR(RAX)

	case 0x43: // f32.const
		raw, err := r.ReadFixedBytes(4)
		if err != nil {
			return err
		}
		fc.Emit(0x68)
		fc.Emit32(binary.LittleEndian.Uint32(raw))
		fc.depth++

	case 0x44: // f64.const
		raw, err := r.ReadFixedBytes(8)
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8)
		fc.Emit64(binary.LittleEndian.Uint64(raw))
		fc.emitPushR(RAX)

	case 0xD0: // ref.null
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.Emit(0x6A, 0x00)
		fc.depth++

	case 0xD1: // ref.is_null
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x85, 0xC0)
		fc.Emit(0x0F, 0x94, 0xC0)
		fc.Emit(0x0F, 0xB6, 0xC0)
		fc.emitPushR(RAX)

	case 0xD2: // ref.func
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8) // mov rax, imm64 (patched to abs native addr)
		fc.relocs = append(fc.relocs, funcReloc{
			codeOff: fc.Pos(), funcIdx: int(idx), isAbs64: true,
		})
		fc.Emit64(0)
		fc.emitPushR(RAX)

	case 0xFC:
		subOp, err := r.ReadU32()
		if err != nil {
			return err
		}
		return fc.dispatchFC(r, subOp)

	default:
		return fmt.Errorf("unimplemented opcode 0x%02X", op)
	}
	return nil
}

func (fc *funcCompiler) dispatchFC(r *decode.Reader, subOp uint32) error {
	switch subOp {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		return fmt.Errorf("i32/i64.trunc_sat not yet implemented (FC %d)", subOp)
	case 8:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		return fmt.Errorf("memory.init not yet implemented")
	case 9: // data.drop — no-op (data inlined at startup)
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 10:
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		return fmt.Errorf("memory.copy not yet implemented")
	case 11: // memory.fill
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.emitMemFill()
	case 12:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table.init not yet implemented")
	case 13: // elem.drop — no-op
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 14:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table.copy not yet implemented")
	case 15, 16, 17:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table op FC %d not yet implemented", subOp)
	default:
		return fmt.Errorf("unimplemented 0xFC sub-opcode %d", subOp)
	}
	return nil
}