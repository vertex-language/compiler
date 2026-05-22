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
		hptrMask := fc.ctx.ImportHptrMasks[funcIdx]

		for i := 0; i < bound; i++ {
			if i < len(ptrMask) && ptrMask[i] {
				fc.emitSafePointerTranslate(ArgRegs[i])
			} else if i < len(hptrMask) && hptrMask[i] {
				// Translate the wasm 32-bit handle index to the true 64-bit native pointer
				fc.emitHandleResolve(ArgRegs[i])
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
		// If the native function returned a handle (like FILE*), register it
		// securely in the backend table before passing the 32-bit index to wasm.
		if isImport && fc.ctx.ReturnHptrMasks[funcIdx] {
			fc.emitHandleRegister()
		}
		fc.emitPushR(RAX)
	}
	return nil
}

func (fc *funcCompiler) emitCallIndirect(typeIdx uint32) error {
	return fmt.Errorf("call_indirect not yet supported")
}

// ── Handle Table Assembly Macros ──────────────────────────────────────────────

// emitHandleResolve transforms a 32-bit wasm handle inside `targetReg` into
// the true 64-bit native pointer by looking it up in the Handle Table.
func (fc *funcCompiler) emitHandleResolve(targetReg int) {
	// 1. Zero-extend the 32-bit wasm handle index into RAX
	// mov eax, targetReg32
	rex1 := byte(0x40)
	if targetReg >= 8 {
		rex1 |= 0x01 // B bit for source
	}
	if rex1 != 0x40 {
		fc.Emit(rex1)
	}
	modrm1 := byte(0xC0 | (targetReg & 7)) // src=targetReg, dst=RAX
	fc.Emit(0x89, modrm1)

	// 2. Multiply handle index by 8 (pointer size)
	fc.Emit(0x48, 0xC1, 0xE0, 0x03) // shl rax, 3

	// 3. Add the Handle Table base address
	fc.Emit(0x48, 0x8D, 0x0D) // lea rcx, [rip + __vertex_handle_table]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -3})
	fc.Emit(0, 0, 0, 0)
	fc.Emit(0x48, 0x01, 0xC8) // add rax, rcx

	// 4. Load the 64-bit native pointer out of the table and into targetReg
	// mov targetReg64, [rax]
	rex2 := byte(0x48)
	if targetReg >= 8 {
		rex2 |= 0x04 // R bit for dst
	}
	modrm2 := byte(0x00 | ((targetReg & 7) << 3) | 0) // src=[RAX], dst=targetReg
	fc.Emit(rex2, 0x8B, modrm2)
}

// emitHandleRegister catches a native 64-bit pointer returned in RAX, stores
// it securely in the Handle Table, and returns the 32-bit index in RAX for wasm.
func (fc *funcCompiler) emitHandleRegister() {
	fc.Emit(0x50) // push rax (safeguard the native pointer)

	// 1. Thread-safe bump allocate a new handle index via atomic XADD
	fc.Emit(0x48, 0x8D, 0x0D) // lea rcx, [rip + __vertex_handle_count]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -4})
	fc.Emit(0, 0, 0, 0)

	fc.Emit(0x41, 0xB8, 1, 0, 0, 0)       // mov r8d, 1
	fc.Emit(0xF0, 0x44, 0x0F, 0xC1, 0x01) // lock xadd [rcx], r8d (old count -> r8d)

	// 2. Store the native pointer in the table
	fc.Emit(0x48, 0x8D, 0x0D) // lea rcx, [rip + __vertex_handle_table]
	fc.relocs = append(fc.relocs, funcReloc{codeOff: fc.Pos(), funcIdx: -3})
	fc.Emit(0, 0, 0, 0)

	fc.Emit(0x58)                   // pop rax (restore native pointer)
	fc.Emit(0x4A, 0x89, 0x04, 0xC1) // mov [rcx + r8*8], rax (store in table)

	// 3. Return the 32-bit handle index to wasm
	fc.Emit(0x44, 0x89, 0xC0) // mov eax, r8d
}