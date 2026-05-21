package x86_64

import (
	"fmt"

	"github.com/vertex-language/compiler/wasm"
)

func (fc *funcCompiler) frameAt(d int) *ctrlFrame {
	return &fc.ctrl[len(fc.ctrl)-1-d]
}

func (fc *funcCompiler) emitBr(d int) {
	frame := fc.frameAt(d)
	targetArity := frame.arity
	if frame.kind == ctrlLoop {
		targetArity = frame.paramArity
	}
	if excess := fc.depth - (frame.baseDepth + targetArity); excess > 0 {
		fc.emitAddRSP(8 * excess)
	}
	if frame.kind == ctrlLoop {
		fc.Emit(0xE9)
		patchOff := fc.ZeroRel32()
		fc.Patch32(patchOff, frame.loopTarget)
	} else {
		fc.Emit(0xE9)
		patch := fc.ZeroRel32()
		frame.endPatches = append(frame.endPatches, patch)
	}
}

func (fc *funcCompiler) addBrPatch(d int, patchOff int) {
	frame := fc.frameAt(d)
	if frame.kind == ctrlLoop {
		fc.Patch32(patchOff, frame.loopTarget)
	} else {
		frame.endPatches = append(frame.endPatches, patchOff)
	}
}

func (fc *funcCompiler) emitBrTable(targets []uint32) {
	fc.emitPopR(RAX)

	fc.Emit(0x3D)
	fc.Emit32(uint32(len(targets) - 1))

	fc.Emit(0x0F, 0x87)
	defaultPatch := fc.ZeroRel32()

	fc.Emit(0x48, 0x8D, 0x0D)
	tableLeaPatch := fc.ZeroRel32()

	fc.Emit(0x48, 0x63, 0x04, 0x81)
	fc.Emit(0x48, 0x01, 0xC8)
	fc.Emit(0xFF, 0xE0)

	fc.Patch32(tableLeaPatch, fc.Pos())

	for _, t := range targets[:len(targets)-1] {
		patchOff := fc.ZeroRel32()
		fc.addBrPatch(int(t), patchOff)
	}

	fc.Patch32(defaultPatch, fc.Pos())
	fc.emitBr(int(targets[len(targets)-1]))
}

func (fc *funcCompiler) emitReturn() {
	if excess := fc.depth - len(fc.ft.Results); excess > 0 {
		fc.emitAddRSP(8 * excess)
	}
	fc.emitEpilogue()
}

func (fc *funcCompiler) emitCall(funcIdx int) error {
	// ── Resolve the function type ─────────────────────────────────────────────

	var ft wasm.FuncType
	if funcIdx < int(fc.ctx.Module.Imports.NumFuncs()) {
		fIdx := 0
		for _, e := range fc.ctx.Module.Imports.Entries {
			if e.Kind == wasm.ImportFunc {
				if fIdx == funcIdx {
					ft = fc.ctx.Module.Types.Entries[e.TypeIdx]
					break
				}
				fIdx++
			}
		}
	} else {
		localIdx := funcIdx - int(fc.ctx.Module.Imports.NumFuncs())
		ftIdx := fc.ctx.Module.Functions.TypeIndices[localIdx]
		ft = fc.ctx.Module.Types.Entries[ftIdx]
	}

	nParams := len(ft.Params)
	if fc.depth < nParams {
		return fmt.Errorf("stack underflow in call")
	}

	bound := nParams
	if bound > 6 {
		bound = 6
	}

	// Pop args into SysV argument registers right-to-left.
	for i := bound - 1; i >= 0; i-- {
		fc.emitPopR(ArgRegs[i])
	}

	// ── Platform inline path (syscall) ────────────────────────────────────────

	if fc.inlinedImports != nil {
		if imp, ok := fc.inlinedImports[funcIdx]; ok {
			return fc.emitPlatformCall(imp, ft, bound)
		}
	}

	// ── Native call path ──────────────────────────────────────────────────────
	//
	// For imports, translate any pointer parameters identified by the @ sig
	// before aligning the stack and issuing the call instruction.

	isImport := uint32(funcIdx) < fc.ctx.Module.Imports.NumFuncs()
	if isImport {
		// Pointer translation: @ sig parsed by driver; mask accessed via context.
		ptrMask := fc.ctx.ImportPtrMasks[funcIdx]
		for i := 0; i < bound; i++ {
			if i < len(ptrMask) && ptrMask[i] {
				fc.emitSafePointerTranslate(ArgRegs[i])
			}
		}

		// Align RSP to 16 bytes (SysV ABI requirement) and zero EAX
		// (variadic calling convention: AL = number of vector registers used).
		fc.emitPushR(R12)
		fc.Emit(0x49, 0x89, 0xE4)       // mov r12, rsp
		fc.Emit(0x48, 0x83, 0xE4, 0xF0) // and rsp, -16
		fc.Emit(0x31, 0xC0)             // xor eax, eax
	}

	fc.Emit(0xE8)
	fc.relocs = append(fc.relocs, funcReloc{
		codeOff: fc.Pos(),
		funcIdx: funcIdx,
	})
	fc.Emit(0, 0, 0, 0)

	if isImport {
		fc.Emit(0x4C, 0x89, 0xE4) // mov rsp, r12
		fc.emitPopR(R12)
	}

	if len(ft.Results) > 0 {
		fc.emitPushR(RAX)
	}
	return nil
}

func (fc *funcCompiler) emitCallIndirect(typeIdx uint32) error {
	return fmt.Errorf("call_indirect not yet supported")
}