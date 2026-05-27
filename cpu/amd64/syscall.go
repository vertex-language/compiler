// cpu/amd64/syscall.go
package amd64

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

// emitPlatformCall dispatches an already-resolved inlined import.
// All inlined imports on amd64/linux are direct syscalls.
func (fc *funcCompiler) emitPlatformCall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	return fc.emitInlineSyscall(imp, ft, nArgs)
}

// emitInlineSyscall emits a direct Linux x86-64 syscall inline.
//
// Arguments are already in SysV registers [rdi, rsi, rdx, rcx, r8, r9].
// Linux syscall ABI: 4th argument is r10 not rcx (syscall clobbers rcx).
// Pointer parameters have R15 added unconditionally — wasm offset 0 is a
// valid buffer location so no NULL guard is applied.
func (fc *funcCompiler) emitInlineSyscall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	if imp.number == -1 {
		return fmt.Errorf("syscall %q::%q is not available on amd64", imp.module, imp.name)
	}
	for i := 0; i < nArgs; i++ {
		if i < len(imp.ptrMask) && imp.ptrMask[i] {
			fc.emitAddR15To(ArgRegs[i])
		}
	}
	if nArgs >= 4 {
		fc.Emit(0x49, 0x89, 0xCA) // mov r10, rcx
	}
	fc.Emit(0xB8)
	fc.Emit32(uint32(imp.number))
	fc.Syscall()
	if len(ft.Results) > 0 {
		fc.emitPushR(RAX)
	}
	return nil
}