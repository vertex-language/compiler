// cpu/amd64/memory.go
package amd64

import (
	"github.com/vertex-language/compiler/cpu/amd64/asm"
	"github.com/vertex-language/compiler/decode"
)

func readMemArg(r *decode.Reader) (align, offset uint32, err error) {
	if align, err = r.ReadU32(); err != nil {
		return
	}
	offset, err = r.ReadU32()
	return
}

// memEncoding returns (ModRM, SIB) for [R15 + RCX + disp32] with given reg.
//   ModRM: mod=10(disp32), reg=reg&7, rm=4(SIB follows)
//   SIB:   scale=0, index=RCX(1), base=R15&7(7)
func memEncoding(reg byte) (byte, byte) {
	return asm.ModRM(0b10, reg&7, 4), asm.SIB(0, 1, 7)
}

func (fc *funcCompiler) emitMemAddr() { fc.emitPopR(RCX) }

// ── Loads ─────────────────────────────────────────────────────────────────────

func (fc *funcCompiler) emitMemLoad32zx(offset uint32) {
	fc.emitMemAddr()
	mr, sib := memEncoding(RAX)
	fc.Emit(0x41, 0x8B, mr, sib)
	fc.Emit32(offset)
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitMemLoad64(offset uint32) {
	fc.emitMemAddr()
	mr, sib := memEncoding(RAX)
	fc.Emit(0x49, 0x8B, mr, sib)
	fc.Emit32(offset)
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitMemLoadSX(offset uint32, byteWidth int, signed bool) {
	fc.emitMemAddr()
	mr, sib := memEncoding(RAX)
	var extOp byte
	switch {
	case byteWidth == 1 && signed:
		extOp = 0xBE
	case byteWidth == 1 && !signed:
		extOp = 0xB6
	case byteWidth == 2 && signed:
		extOp = 0xBF
	default:
		extOp = 0xB7
	}
	fc.Emit(0x41, 0x0F, extOp, mr, sib)
	fc.Emit32(offset)
	fc.emitPushR(RAX)
}

func (fc *funcCompiler) emitMemLoadSX64(offset uint32, byteWidth int, signed bool) {
	fc.emitMemAddr()
	mr, sib := memEncoding(RAX)
	switch {
	case byteWidth == 1 && signed:
		fc.Emit(0x49, 0x0F, 0xBE, mr, sib)
	case byteWidth == 1 && !signed:
		fc.Emit(0x41, 0x0F, 0xB6, mr, sib)
	case byteWidth == 2 && signed:
		fc.Emit(0x49, 0x0F, 0xBF, mr, sib)
	case byteWidth == 2 && !signed:
		fc.Emit(0x41, 0x0F, 0xB7, mr, sib)
	case byteWidth == 4 && signed:
		fc.Emit(0x49, 0x63, mr, sib)
	default:
		fc.Emit(0x41, 0x8B, mr, sib)
	}
	fc.Emit32(offset)
	fc.emitPushR(RAX)
}

// ── Stores ────────────────────────────────────────────────────────────────────

func (fc *funcCompiler) emitMemStore(offset uint32, byteWidth int) {
	fc.emitPopR(RAX) // val  (TOS)
	fc.emitPopR(RCX) // addr (SIB index)
	mr, sib := memEncoding(RAX)
	switch byteWidth {
	case 1:
		fc.Emit(0x41, 0x88, mr, sib)
	case 2:
		fc.Emit(0x66, 0x41, 0x89, mr, sib)
	case 4:
		fc.Emit(0x41, 0x89, mr, sib)
	case 8:
		fc.Emit(0x49, 0x89, mr, sib)
	}
	fc.Emit32(offset)
}

// emitMemFill implements memory.fill via rep stosb.
// Stack before: [..., d, val, n]  n = TOS
func (fc *funcCompiler) emitMemFill() {
	fc.emitPopR(RCX)              // n
	fc.emitPopR(RAX)              // val (byte → AL)
	fc.emitPopR(RDI)              // d (destination offset)
	fc.Emit(0x89, 0xC9)           // mov ecx, ecx (zero-extend)
	fc.Emit(0x89, 0xFF)           // mov edi, edi (zero-extend)
	fc.Emit(0x4C, 0x01, 0xFF)     // add rdi, r15
	fc.RepStosb()
}