// cpu/amd64/body.go
package amd64

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
	case v == -64: // 0x40 — empty
		return 0, 0, nil
	case v < 0: // single value type
		return 0, 1, nil
	default: // type index
		idx := uint32(v)
		if int(idx) >= len(fc.ctx.Module.Types.Entries) {
			return 0, 0, fmt.Errorf("block type index %d out of range", idx)
		}
		ft := fc.ctx.Module.Types.Entries[idx]
		return len(ft.Params), len(ft.Results), nil
	}
}

// skipDeadOp advances past one instruction's immediates while in dead-code
// mode and watches for control-flow constructs that can restore liveness.
func (fc *funcCompiler) skipDeadOp(r *decode.Reader, op byte) error {
	switch op {
	case 0x02, 0x03, 0x04: // block, loop, if
		if _, _, err := fc.readBlockType(r); err != nil {
			return err
		}
		fc.deadDepth++

	case 0x05: // else
		if fc.deadDepth > 0 {
			break
		}
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
			fc.deadDepth--
			break
		}
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
			return nil
		}
		fc.dead = false
		fc.depth = frame.baseDepth + frame.arity

	case 0x0C, 0x0D:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x0E:
		n, err := r.ReadU32()
		if err != nil {
			return err
		}
		for i := uint32(0); i <= n; i++ {
			if _, err := r.ReadU32(); err != nil {
				return err
			}
		}
	case 0x10, 0x12:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x11, 0x13:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x1C:
		n, err := r.ReadU32()
		if err != nil {
			return err
		}
		for i := uint32(0); i < n; i++ {
			if _, err := r.ReadByte(); err != nil {
				return err
			}
		}
	case 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35,
		0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		if _, err := r.ReadU32(); err != nil {
			return err
		}
	case 0x3F, 0x40:
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
	case 0xD0:
		if _, err := r.ReadByte(); err != nil {
			return err
		}
	case 0xD2:
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

func skipFCImm(r *decode.Reader, subOp uint32) error {
	switch subOp {
	case 8:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		_, err := r.ReadByte()
		return err
	case 9, 13:
		_, err := r.ReadU32()
		return err
	case 10:
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		_, err := r.ReadByte()
		return err
	case 11:
		_, err := r.ReadByte()
		return err
	case 12, 14:
		if _, err := r.ReadU32(); err != nil {
			return err
		}
		_, err := r.ReadU32()
		return err
	case 15, 16, 17:
		_, err := r.ReadU32()
		return err
	}
	return nil // sub-ops 0–7 carry no extra immediates
}