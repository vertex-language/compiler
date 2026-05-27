// cpu/amd64/control.go
package amd64

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
		p := fc.ZeroRel32()
		fc.Patch32(p, frame.loopTarget)
	} else {
		fc.Emit(0xE9)
		p := fc.ZeroRel32()
		frame.endPatches = append(frame.endPatches, p)
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
		p := fc.ZeroRel32()
		fc.addBrPatch(int(t), p)
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
	var ft wasm.FuncType
	if funcIdx < int(fc.ctx.Module.Imports.NumFuncs()) {
		fi := 0
		for _, e := range fc.ctx.Module.Imports.Entries {
			if e.Kind == wasm.ImportFunc {
				if fi == funcIdx {
					ft = fc.ctx.Module.Types.Entries[e.TypeIdx]
					break
				}
				fi++
			}
		}
	} else {
		localIdx := funcIdx - int(fc.ctx.Module.Imports.NumFuncs())
		ft = fc.ctx.Module.Types.Entries[fc.ctx.Module.Functions.TypeIndices[localIdx]]
	}

	nParams := len(ft.Params)
	if fc.depth < nParams {
		return fmt.Errorf("stack underflow in call")
	}
	bound := nParams
	if bound > 6 {
		bound = 6
	}
	for i := bound - 1; i >= 0; i-- {
		fc.emitPopR(ArgRegs[i])
	}

	if fc.inlinedImports != nil {
		if imp, ok := fc.inlinedImports[funcIdx]; ok {
			return fc.emitPlatformCall(imp, ft, bound)
		}
	}

	isImport := uint32(funcIdx) < fc.ctx.Module.Imports.NumFuncs()
	if isImport {
		ptrMask := fc.ctx.ImportPtrMasks[funcIdx]
		hptrMask := fc.ctx.ImportHptrMasks[funcIdx]
		for i := 0; i < bound; i++ {
			if i < len(ptrMask) && ptrMask[i] {
				fc.emitSafePointerTranslate(ArgRegs[i])
			} else if i < len(hptrMask) && hptrMask[i] {
				fc.emitHandleResolve(ArgRegs[i])
			}
		}
		fc.emitPushR(R12)
		fc.Emit(0x49, 0x89, 0xE4)       // mov r12, rsp
		fc.Emit(0x48, 0x83, 0xE4, 0xF0) // and rsp, -16
		fc.Emit(0x31, 0xC0)              // xor eax, eax
	}

	fc.Emit(0xE8)
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: funcIdx})
	fc.Emit(0, 0, 0, 0)

	if isImport {
		fc.Emit(0x4C, 0x89, 0xE4) // mov rsp, r12
		fc.emitPopR(R12)
	}

	if len(ft.Results) > 0 {
		if isImport && fc.ctx.ReturnHptrMasks[funcIdx] {
			fc.emitHandleRegister()
		}
		fc.emitPushR(RAX)
	}
	return nil
}

func (fc *funcCompiler) emitCallIndirect(_ uint32) error {
	return fmt.Errorf("call_indirect not yet supported")
}

// ── Handle Table macros ───────────────────────────────────────────────────────

// emitHandleResolve translates a 32-bit wasm handle index in targetReg into
// the 64-bit native pointer it represents, via the Handle Table.
func (fc *funcCompiler) emitHandleResolve(targetReg int) {
	rex1 := byte(0x40)
	if targetReg >= 8 {
		rex1 |= 0x01
	}
	if rex1 != 0x40 {
		fc.Emit(rex1)
	}
	fc.Emit(0x89, byte(0xC0|(targetReg&7)))    // mov eax, targetReg32
	fc.Emit(0x48, 0xC1, 0xE0, 0x03)            // shl rax, 3
	fc.Emit(0x48, 0x8D, 0x0D)                  // lea rcx, [rip + __vertex_handle_table]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -3})
	fc.Emit(0, 0, 0, 0)
	fc.Emit(0x48, 0x01, 0xC8) // add rax, rcx
	rex2 := byte(0x48)
	if targetReg >= 8 {
		rex2 |= 0x04
	}
	fc.Emit(rex2, 0x8B, byte(0x00|((targetReg&7)<<3))) // mov targetReg, [rax]
}

// emitHandleRegister interns the native pointer in RAX into the Handle Table
// via a lock xadd bump and leaves the 32-bit handle index in EAX.
func (fc *funcCompiler) emitHandleRegister() {
	fc.Emit(0x50) // push rax (save native pointer)

	fc.Emit(0x48, 0x8D, 0x0D) // lea rcx, [rip + __vertex_handle_count]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -4})
	fc.Emit(0, 0, 0, 0)
	fc.Emit(0x41, 0xB8, 1, 0, 0, 0)       // mov r8d, 1
	fc.Emit(0xF0, 0x44, 0x0F, 0xC1, 0x01) // lock xadd [rcx], r8d

	fc.Emit(0x48, 0x8D, 0x0D) // lea rcx, [rip + __vertex_handle_table]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -3})
	fc.Emit(0, 0, 0, 0)

	fc.Emit(0x58)                    // pop rax (restore native pointer)
	fc.Emit(0x4A, 0x89, 0x04, 0xC1) // mov [rcx + r8*8], rax
	fc.Emit(0x44, 0x89, 0xC0)        // mov eax, r8d
}