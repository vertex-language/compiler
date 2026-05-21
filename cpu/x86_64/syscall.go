package x86_64

import (
	"fmt"

	"github.com/vertex-language/compiler/platform"
	"github.com/vertex-language/compiler/wasm"
)

type inlinedImport struct {
	module  string
	name    string
	number  int
	ptrMask []bool
}

func (fc *funcCompiler) emitPlatformCall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	route := platform.Parse(imp.module)
	switch route.Kind {
	case platform.SyscallTrampoline:
		return fc.emitInlineSyscall(imp, ft, nArgs)
	case platform.PlatformLib:
		return fmt.Errorf(
			"platform import %q::%q: PlatformLib inline emission not implemented",
			imp.module, imp.name,
		)
	default:
		return fmt.Errorf(
			"platform import %q::%q: unexpected route kind %d",
			imp.module, imp.name, route.Kind,
		)
	}
}

// emitInlineSyscall emits a direct Linux x86-64 syscall.
//
// At entry, arguments are already in SysV registers [rdi, rsi, rdx, rcx, r8, r9].
// Linux syscall ABI: 4th argument is r10 not rcx (syscall clobbers rcx).
// Pointer parameters have R15 added unconditionally (no NULL guard — wasm
// linear-memory offset 0 is a valid buffer location).
func (fc *funcCompiler) emitInlineSyscall(imp inlinedImport, ft wasm.FuncType, nArgs int) error {
	if imp.number == -1 {
		return fmt.Errorf(
			"platform import %q::%q is not available on this architecture",
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