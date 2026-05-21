package x86_64

import (
	"fmt"

	"github.com/vertex-language/compiler/decode"
)

// emitBody is the main instruction dispatch loop.
func (fc *funcCompiler) emitBody(r *decode.Reader) error {
	for !r.EOF() {
		op, err := r.ReadByte()
		if err != nil {
			return err
		}
		if fc.dead {
			if err := fc.skipDeadOp(r, op); err != nil {
				return err
			}
			continue
		}
		if err := fc.dispatchOp(r, op); err != nil {
			return fmt.Errorf("op 0x%02X: %w", op, err)
		}
	}
	return nil
}

// readBlockType reads a block-type immediate and returns (paramArity, resultArity).
func (fc *funcCompiler) readBlockType(r *decode.Reader) (int, int, error) {
	v, err := r.ReadSLEB128()
	if err != nil {
		return 0, 0, err
	}
	switch {
	case v == -64: // 0x40 — empty type
		return 0, 0, nil
	case v < 0: // single value type
		return 0, 1, nil
	default: // type index
		idx := uint32(v)
		if int(idx) >= len(fc.m.Types.Entries) {
			return 0, 0, fmt.Errorf("block type index %d out of range", idx)
		}
		ft := fc.m.Types.Entries[idx]
		return len(ft.Params), len(ft.Results), nil
	}
}

// skipDeadOp advances past one instruction's immediates while in dead-code mode
// and watches for control-flow constructs that can restore liveness.
func (fc *funcCompiler) skipDeadOp(r *decode.Reader, op byte) error {
	switch op {

	case 0x02, 0x03, 0x04: // block, loop, if — enter a dead nested scope
		if _, _, err := fc.readBlockType(r); err != nil {
			return err
		}
		fc.deadDepth++

	case 0x05: // else
		if fc.deadDepth > 0 {
			break // else inside a nested dead block — ignore
		}
		// Else matches the if that started this dead region.
		if len(fc.ctrl) == 0 {
			break
		}
		top := &fc.ctrl[len(fc.ctrl)-1]
		if top.kind == ctrlIf && top.elseJmpOff != 0 {
			fc.Patch32(top.elseJmpOff, fc.Pos())
			top.elseJmpOff = 0
			fc.dead = false
			fc.depth = top.baseDepth
		}

	case 0x0B: // end
		if fc.deadDepth > 0 {
			fc.deadDepth-- // pop a nested dead scope
			break
		}
		// Ending the live block that contained the dead code.
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
			// Function body ended while dead — emit epilogue and stay dead.
			fc.emitEpilogue()
			fc.dead = true
			return nil
		}
		// Resume live code in the enclosing block.
		fc.dead = false
		fc.depth = frame.baseDepth + frame.arity

	// Skip over immediates for all other instructions that carry them:
	case 0x0C, 0x0D: // br, br_if
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x0E: // br_table
		n, err := r.ReadU32()
		if err != nil {
			return err
		}
		for i := uint32(0); i <= n; i++ {
			if _, err := r.ReadU32(); err != nil {
				return err
			}
		}
	case 0x10, 0x12: // call, return_call
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x11, 0x13: // call_indirect, return_call_indirect
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
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
	case 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26: // local/global/table get+set+tee
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35,
		0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E: // loads + stores
		if _, err := r.ReadU32(); err != nil { // align
			return err
		}
		if _, err := r.ReadU32(); err != nil { // offset
			return err
		}
	case 0x3F, 0x40: // memory.size, memory.grow
		if _, err := r.ReadByte(); err != nil {
			return err
		}
	case 0x41:
		if _, err := r.ReadS32(); err != nil {
			return err
		}
	case 0x42:
		if _, err := r.ReadSLEB128(); err != nil {
			return err
		}
	case 0x43:
		if _, err := r.ReadFixedBytes(4); err != nil {
			return err
		}
	case 0x44:
		if _, err := r.ReadFixedBytes(8); err != nil {
			return err
		}
	case 0xD0: // ref.null ht
		if _, err := r.ReadByte(); err != nil {
			return err
		}
	case 0xD2: // ref.func
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0xFC:
		subOp, err := r.ReadU32()
		if err != nil {
			return err
		}
		return skipFCImm(r, subOp)
	}
	return nil
}

// skipFCImm skips the immediates that follow a 0xFC sub-opcode.
func skipFCImm(r *decode.Reader, subOp uint32) error {
	switch subOp {
	case 8: // memory.init dataIdx memIdx
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		_, err := r.ReadByte()
		return err
	case 9, 13: // data.drop, elem.drop — one u32
		_, err := r.ReadU32()
		return err
	case 10: // memory.copy dstMem srcMem — two bytes
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		_, err := r.ReadByte()
		return err
	case 11: // memory.fill memIdx — one byte
		_, err := r.ReadByte()
		return err
	case 12, 14: // table.init, table.copy — two u32s
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		_, err := r.ReadU32()
		return err
	case 15, 16, 17: // table.grow, table.size, table.fill — one u32
		_, err := r.ReadU32()
		return err
	}
	// Sub-ops 0–7 (saturating truncations) carry no extra immediates.
	return nil
}