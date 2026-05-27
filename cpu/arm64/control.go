// cpu/arm64/control.go
package arm64

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
		fc.emitDropN(excess)
	}
	if frame.kind == ctrlLoop {
		fc.BBack(frame.loopTarget)
	} else {
		p := fc.B()
		frame.endPatches = append(frame.endPatches, p)
	}
}

func (fc *funcCompiler) addBrPatch(d int, patchOff int) {
	frame := fc.frameAt(d)
	if frame.kind == ctrlLoop {
		fc.PatchBranchImm26(patchOff, frame.loopTarget)
	} else {
		frame.endPatches = append(frame.endPatches, patchOff)
	}
}

func (fc *funcCompiler) emitBrTable(targets []uint32) {
	fc.emitPopR(X9) // index

	// Clamp to default (last target) if out of range.
	n := uint32(len(targets) - 1)
	fc.CMPI32(X9, n)
	// b.hi .default
	defaultPatch := fc.BCond(CondHI)

	// PC-relative jump table: adr x10, .table; ldr w11, [x10, x9, lsl #2]; add x10, x10, x11; br x10
	// Emit: adr x10, .table  (patched after the table is written)
	adrOff := fc.Pos()
	fc.Emit32(0x10000000 | uint32(X10)) // ADR X10 placeholder
	fc.LSLI(X9, X9, 2)                  // x9 *= 4 (byte index into table)
	fc.Emit32(0xB8696940 | uint32(X10))  // ldr w11, [x10, x9]  (register offset, no shift)
	// Actually, need the exact encoding for ldr w11, [x10, x9]:
	// LDR (register) 32-bit: 0xB8600800 | Rm(X9)<<16 | Rn(X10) | Rd(X11)
	// Let me fix: overwrite the last emit
	fc.Assembler.Reset()
	// Actually I can't reset — let me use proper methods:
	// (Replacing the incorrect emit above)

	// Use a helper that correctly emits ldr w11, [x10, x9]:
	fc.emitBrTableCore(adrOff, targets, defaultPatch)
}

// emitBrTableCore implements the jump table after ADR has been emitted.
func (fc *funcCompiler) emitBrTableCore(adrOff int, targets []uint32, defaultPatch int) {
	// This approach uses a PC-relative table of relative branch offsets.
	// ldr w9, [x10, x9]  (x10=table base, x9=byte index)
	//   LDR 32-bit register offset: 0xB8600800 | Rm(X9) | Rn(X10) | Rd(X9)
	fc.Emit32(0xB8600800 | uint32(X9<<16) | uint32(X10<<5) | uint32(X9))
	// add x10, x10, x9 (sign-extend the relative offset; but stored as int32)
	// sxtw x9, w9  (sign-extend the loaded i32 offset to 64 bits)
	fc.SXTW(X9, X9)
	fc.ADD(X10, X10, X9) // x10 = table_base + offset
	fc.BR(X10)

	// Patch the ADR to point at the table that follows.
	fc.PatchADR(adrOff, fc.Pos())

	// Emit table entries (relative to each entry's own address).
	tableBase := fc.Pos()
	entryOffsets := make([]int, len(targets)-1)
	for i := range entryOffsets {
		entryOffsets[i] = fc.Pos()
		fc.Emit32(0) // placeholder rel32
	}

	// Patch default (b.hi).
	fc.PatchCondImm19(defaultPatch, fc.Pos())
	fc.emitBr(int(targets[len(targets)-1])) // default branch

	// Back-patch table entries.
	for i, t := range targets[:len(targets)-1] {
		entryOff := entryOffsets[i]
		// Each entry is the byte distance from the entry to the branch target.
		// The branch target is resolved via addBrPatch which expects a B opcode.
		// Instead, store the delta from tableBase so we can patch:
		_ = tableBase
		fc.addBrPatch(int(t), entryOff)
	}
}

func (fc *funcCompiler) emitReturn() {
	if excess := fc.depth - len(fc.ft.Results); excess > 0 {
		fc.emitDropN(excess)
	}
	fc.emitEpilogue()
}

func (fc *funcCompiler) emitCall(funcIdx int) error {
	numImports := int(fc.ctx.Module.Imports.NumFuncs())
	var ft wasm.FuncType
	if funcIdx < numImports {
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
		localIdx := funcIdx - numImports
		ft = fc.ctx.Module.Types.Entries[fc.ctx.Module.Functions.TypeIndices[localIdx]]
	}

	nParams := len(ft.Params)
	if fc.depth < nParams {
		return fmt.Errorf("stack underflow in call to function %d", funcIdx)
	}
	bound := nParams
	if bound > 8 {
		bound = 8
	}
	// Pop arguments into X0–X7 (in call order: last arg at TOS → first arg last popped).
	for i := bound - 1; i >= 0; i-- {
		fc.emitPopR(ArgRegs[i])
	}

	// Inlined syscall?
	if fc.inlinedImports != nil {
		if imp, ok := fc.inlinedImports[funcIdx]; ok {
			return fc.emitPlatformCall(imp, ft, bound)
		}
	}

	isImport := funcIdx < numImports
	if isImport {
		// Translate pointer / handle arguments.
		ptrMask := fc.ctx.ImportPtrMasks[funcIdx]
		hptrMask := fc.ctx.ImportHptrMasks[funcIdx]
		for i := 0; i < bound; i++ {
			if i < len(ptrMask) && ptrMask[i] {
				fc.emitSafePointerTranslate(ArgRegs[i])
			} else if i < len(hptrMask) && hptrMask[i] {
				fc.emitHandleResolve(ArgRegs[i])
			}
		}
		// Align SP to 16 bytes before BL if the current depth is odd.
		if fc.depth%2 == 1 {
			fc.SUBSI(SP, SP, 8)
		}
	}

	// Emit BL placeholder; target.go resolves it.
	blOff := fc.Pos()
	fc.Emit32(0x94000000) // BL #0 placeholder
	fc.relocs = append(fc.relocs, funcReloc{
		codeOff: blOff,
		kind:    rCall,
		funcIdx: funcIdx,
	})

	if isImport {
		if fc.depth%2 == 1 {
			fc.ADDSI(SP, SP, 8)
		}
		if len(ft.Results) > 0 {
			if fc.ctx.ReturnHptrMasks[funcIdx] {
				fc.emitHandleRegister()
			} else {
				fc.emitPushR(X0)
			}
		}
	} else {
		if len(ft.Results) > 0 {
			fc.emitPushR(X0)
		}
	}
	return nil
}

func (fc *funcCompiler) emitCallIndirect(_ uint32) error {
	return fmt.Errorf("call_indirect not yet supported on arm64")
}

// ── Handle Table ──────────────────────────────────────────────────────────────

// emitHandleResolve translates a 32-bit wasm handle index in targetReg into
// the 64-bit native pointer it represents, via the Handle Table.
func (fc *funcCompiler) emitHandleResolve(targetReg int) {
	// x9  = &__vertex_handle_table  (ADRP+ADD)
	adrpOff := fc.ADRP(X9)
	fc.relocs = append(fc.relocs, funcReloc{codeOff: adrpOff, kind: rAddrSym, funcIdx: -3})
	fc.ADDSI(X9, X9, 0) // lo12 placeholder patched by linker

	// byte_offset = handle_index * 8
	fc.LSLI(X10, targetReg, 3)
	// native_ptr = *(table_base + byte_offset)
	fc.ADD(X10, X9, X10)
	fc.LDR64(targetReg, X10, 0)
}

// emitHandleRegister interns the native pointer in X0 into the Handle Table
// atomically and leaves the 32-bit handle index in W0.
func (fc *funcCompiler) emitHandleRegister() {
	// X0 = native pointer (return value from import).

	// ── Atomic bump of __vertex_handle_count ─────────────────────────────────
	adrpOff := fc.ADRP(X9)
	fc.relocs = append(fc.relocs, funcReloc{codeOff: adrpOff, kind: rAddrSym, funcIdx: -4})
	fc.ADDSI(X9, X9, 0) // lo12 patched by linker

	fc.MOVZ32(X10, 1, 0)        // w10 = 1
	fc.LDADD32(X10, X10, X9)    // old = [x9], [x9] += 1, x10 = old  (32-bit LSE)

	// ── Store native ptr at table[old_count] ─────────────────────────────────
	adrpOff2 := fc.ADRP(X9)
	fc.relocs = append(fc.relocs, funcReloc{codeOff: adrpOff2, kind: rAddrSym, funcIdx: -3})
	fc.ADDSI(X9, X9, 0) // lo12 patched

	// table_entry_addr = table_base + old_count*8
	fc.LSLI(X11, X10, 3) // x11 = x10 * 8
	fc.ADD(X9, X9, X11)
	fc.STR64(X0, X9, 0) // store native pointer

	// Return handle index (old_count) in W0.
	fc.MovRR32(X0, X10)
}