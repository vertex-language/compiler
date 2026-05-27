// cpu/arm64/syscall.go
package arm64

import (
	"fmt"

	"github.com/vertex-language/compiler/wasm"
)

type inlinedImport struct {
	module  string
	name    string
	number  int
	ptrMask []bool
}

// emitPlatformCall dispatches an inlined import.
// On arm64/linux all inlined imports are direct syscalls.
func (fc *funcCompiler) emitPlatformCall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	return fc.emitInlineSyscall(imp, ft, nArgs)
}

// emitInlineSyscall emits a direct Linux AArch64 syscall inline.
//
// Linux arm64 syscall ABI:
//   - Syscall number in X8
//   - Arguments in X0–X5 (max 6; no 4th-arg shuffle needed unlike amd64)
//   - Result in X0
//   - SVC #0 is the trap instruction
func (fc *funcCompiler) emitInlineSyscall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	if imp.number == -1 {
		return fmt.Errorf("syscall %q::%q is not available on arm64", imp.module, imp.name)
	}
	// Translate pointer arguments: add MemBase to convert wasm offset → native addr.
	for i := 0; i < nArgs; i++ {
		if i < len(imp.ptrMask) && imp.ptrMask[i] {
			fc.emitAddMemBaseTo(ArgRegs[i])
		}
	}
	// Load syscall number into X8.
	fc.MOVZ32(X8, uint16(imp.number), 0)
	if imp.number > 0xFFFF {
		fc.MOVK(X8, uint16(imp.number>>16), 1)
	}
	fc.SVC()
	if len(ft.Results) > 0 {
		fc.emitPushR(X0)
	}
	return nil
}