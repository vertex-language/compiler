package x86_64

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
// By the time this is called, buildImportInfo has already determined the
// route — everything in inlinedImports is a Linux syscall.
func (fc *funcCompiler) emitPlatformCall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	return fc.emitInlineSyscall(imp, ft, nArgs)
}

// emitInlineSyscall emits a direct Linux x86-64 syscall.
//
// At entry, arguments are already in SysV registers [rdi, rsi, rdx, rcx, r8, r9].
// Linux syscall ABI: 4th argument is r10 not rcx (syscall clobbers rcx).
// Pointer parameters have R15 added unconditionally — wasm linear-memory
// offset 0 is a valid buffer location so no NULL guard is applied.
func (fc *funcCompiler) emitInlineSyscall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	if imp.number == -1 {
		return fmt.Errorf(
			"syscall %q::%q is not available on this architecture",
			imp.module, imp.name,
		)
	}

	// 1. Translate pointer arguments: wasm linear-memory offset → native VA.
	for i := 0; i < nArgs; i++ {
		if i < len(imp.ptrMask) && imp.ptrMask[i] {
			fc.emitAddR15To(ArgRegs[i])
		}
	}

	// 2. Linux 4th-argument fix: mov r10, rcx
	if nArgs >= 4 {
		fc.Emit(0x49, 0x89, 0xCA)
	}

	// 3. Load syscall number into eax (zero-extends to rax).
	fc.Emit(0xB8)
	fc.Emit32(uint32(imp.number))

	// 4. Trap into the kernel.
	fc.Syscall()

	// 5. Push return value if the wasm type declares one.
	if len(ft.Results) > 0 {
		fc.emitPushR(RAX)
	}
	return nil
}