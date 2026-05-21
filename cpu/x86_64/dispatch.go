package x86_64

import (
	"encoding/binary"
	"fmt"

	"github.com/vertex-language/compiler/decode"
)

// dispatchOp handles a single live instruction.
func (fc *funcCompiler) dispatchOp(r *decode.Reader, op byte) error {
	// Delegate Arithmetic, Comparisons, and Conversions to the math dispatcher
	if op >= 0x45 && op <= 0xC4 {
		return fc.dispatchMath(op)
	}

	switch op {

	// ── Control ───────────────────────────────────────────────────────────────

	case 0x00: // unreachable
		fc.Emit(0x0F, 0x0B) // ud2
		fc.dead = true
		fc.deadDepth = 0

	case 0x01: // nop

	case 0x02: // block bt
		pArity, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind:       ctrlBlock,
			arity:      rArity,
			paramArity: pArity,
			baseDepth:  fc.depth - pArity,
		})

	case 0x03: // loop bt
		pArity, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind:       ctrlLoop,
			arity:      rArity,
			paramArity: pArity,
			baseDepth:  fc.depth - pArity,
			loopTarget: fc.Pos(),
		})

	case 0x04: // if bt
		_, rArity, err := fc.readBlockType(r)
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.Emit(0x85, 0xC0) // test eax, eax
		fc.Emit(0x0F, 0x84) // je rel32 (false branch → else or end)
		elseOff := fc.ZeroRel32()
		fc.ctrl = append(fc.ctrl, ctrlFrame{
			kind:       ctrlIf,
			arity:      rArity,
			baseDepth:  fc.depth,
			elseJmpOff: elseOff,
		})

	case 0x05: // else
		if len(fc.ctrl) == 0 {
			return fmt.Errorf("else without if")
		}
		top := &fc.ctrl[len(fc.ctrl)-1]
		if top.kind != ctrlIf {
			return fmt.Errorf("else must follow if block")
		}
		// True branch: jump to end.
		fc.Emit(0xE9)
		endPatch := fc.ZeroRel32()
		top.endPatches = append(top.endPatches, endPatch)
		// Patch the false-branch je to land at start of else body.
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

	case 0x0C: // br l
		l, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitBr(int(l))
		fc.dead = true
		fc.deadDepth = 0

	case 0x0D: // br_if l
		l, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.Emit(0x85, 0xC0) // test eax, eax
		fc.Emit(0x0F, 0x85) // jne rel32
		patchOff := fc.ZeroRel32()
		fc.addBrPatch(int(l), patchOff)

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

	case 0x10: // call funcIdx
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		return fc.emitCall(int(idx))

	case 0x11: // call_indirect typeIdx tableIdx
		typeIdx, err := r.ReadU32()
		if err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil { // table index (MVP: ignored)
			return err
		}
		return fc.emitCallIndirect(typeIdx)

	// ── Parametric ────────────────────────────────────────────────────────────

	case 0x1A: // drop
		fc.emitAddRSP(8)
		fc.depth--

	case 0x1B: // select
		fc.emitSelect()

	case 0x1C: // select t (typed select)
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

	// ── Variables ─────────────────────────────────────────────────────────────

	case 0x20: // local.get i
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitLoadLocal64(RAX, int(idx))
		fc.emitPushR(RAX)

	case 0x21: // local.set i
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPopR(RAX)
		fc.emitStoreLocal64(int(idx), RAX)

	case 0x22: // local.tee i
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitPeekR(RAX)
		fc.emitStoreLocal64(int(idx), RAX)

	case 0x23: // global.get i
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitGlobalGet(int(idx))

	case 0x24: // global.set i
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.emitGlobalSet(int(idx))

	// ── Memory loads ──────────────────────────────────────────────────────────

	case 0x28: // i32.load
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad32zx(off)
	case 0x29: // i64.load
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad64(off)
	case 0x2A: // f32.load (as i32 bits)
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad32zx(off)
	case 0x2B: // f64.load (as i64 bits)
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoad64(off)
	case 0x2C: // i32.load8_s
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 1, true)
	case 0x2D: // i32.load8_u
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 1, false)
	case 0x2E: // i32.load16_s
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 2, true)
	case 0x2F: // i32.load16_u
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX(off, 2, false)
	case 0x30: // i64.load8_s
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 1, true)
	case 0x31: // i64.load8_u
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 1, false)
	case 0x32: // i64.load16_s
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 2, true)
	case 0x33: // i64.load16_u
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 2, false)
	case 0x34: // i64.load32_s
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 4, true)
	case 0x35: // i64.load32_u
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemLoadSX64(off, 4, false)

	// ── Memory stores ─────────────────────────────────────────────────────────

	case 0x36: // i32.store
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)
	case 0x37: // i64.store
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 8)
	case 0x38: // f32.store
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)
	case 0x39: // f64.store
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 8)
	case 0x3A: // i32.store8
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 1)
	case 0x3B: // i32.store16
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 2)
	case 0x3C: // i64.store8
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 1)
	case 0x3D: // i64.store16
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 2)
	case 0x3E: // i64.store32
		_, off, err := readMemArg(r)
		if err != nil {
			return err
		}
		fc.emitMemStore(off, 4)

	// ── Memory control ────────────────────────────────────────────────────────

	case 0x3F: // memory.size
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		pages := uint32(0)
		if fc.m.Memories.Len() > 0 {
			pages = fc.m.Memories.Entries[0].Lim.Min
		}
		fc.Emit(0x68)
		fc.Emit32(pages)
		fc.depth++

	case 0x40: // memory.grow — stub: always return -1
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.emitPopR(RAX)    // discard requested pages
		fc.Emit(0x6A, 0xFF) // push -1  (net depth unchanged)

	// ── Constants ─────────────────────────────────────────────────────────────

	case 0x41: // i32.const n
		v, err := r.ReadS32()
		if err != nil {
			return err
		}
		fc.Emit(0x68) // push imm32 (sign-extended to 64)
		fc.Emit32(uint32(v))
		fc.depth++

	case 0x42: // i64.const n
		v, err := r.ReadSLEB128()
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8) // mov rax, imm64
		fc.Emit64(uint64(v))
		fc.emitPushR(RAX)

	case 0x43: // f32.const (raw bits)
		raw, err := r.ReadFixedBytes(4)
		if err != nil {
			return err
		}
		fc.Emit(0x68)
		fc.Emit32(binary.LittleEndian.Uint32(raw))
		fc.depth++

	case 0x44: // f64.const (raw bits)
		raw, err := r.ReadFixedBytes(8)
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8)
		fc.Emit64(binary.LittleEndian.Uint64(raw))
		fc.emitPushR(RAX)

	// ── Reference types ───────────────────────────────────────────────────────

	case 0xD0: // ref.null ht
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.Emit(0x6A, 0x00) // push 0  (null reference)
		fc.depth++

	case 0xD1: // ref.is_null
		fc.emitPopR(RAX)
		fc.Emit(0x48, 0x85, 0xC0) // test rax, rax
		fc.Emit(0x0F, 0x94, 0xC0) // sete al
		fc.Emit(0x0F, 0xB6, 0xC0) // movzx eax, al
		fc.emitPushR(RAX)

	case 0xD2: // ref.func idx
		funcIdx, err := r.ReadU32()
		if err != nil {
			return err
		}
		fc.Emit(0x48, 0xB8) // mov rax, imm64
		fc.relocs = append(fc.relocs, funcReloc{
			codeOff: fc.Pos(),
			funcIdx: int(funcIdx),
			isAbs64: true,
		})
		fc.Emit64(0) // patched to absolute native address by applyRelocs/linker
		fc.emitPushR(RAX)

	// ── 0xFC prefix ───────────────────────────────────────────────────────────

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

// dispatchFC handles 0xFC-prefixed instructions.
func (fc *funcCompiler) dispatchFC(r *decode.Reader, subOp uint32) error {
	switch subOp {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		return fmt.Errorf("i32/i64.trunc_sat not implemented (FC %d)", subOp)

	case 8: // memory.init dataIdx memIdx
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		return fmt.Errorf("memory.init not implemented")

	case 9: // data.drop — no-op (data is inlined at startup)
		if _, err := r.ReadU32(); err != nil {
			return err
		}

	case 10: // memory.copy dstMem srcMem
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		return fmt.Errorf("memory.copy not implemented")

	case 11: // memory.fill memIdx
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		fc.emitMemFill()
		return nil

	case 12: // table.init elemIdx tableIdx
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table.init not implemented")

	case 13: // elem.drop — no-op
		if _, err := r.ReadU32(); err != nil {
			return err
		}

	case 14: // table.copy dstTable srcTable
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table.copy not implemented")

	case 15, 16, 17: // table.grow, table.size, table.fill
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		return fmt.Errorf("table op FC %d not implemented", subOp)

	default:
		return fmt.Errorf("unimplemented 0xFC sub-opcode %d", subOp)
	}
	return nil
}