package thread

import (
	"encoding/binary"

	"github.com/vertex-language/compiler/object"
)

// emitJoinStub emits a futex-based thread join.
//
// Emitted symbol: __thread_join_<name>
//
// Signature (SysV): func(tidPtr *u32)
//   rdi = pointer to the child_tid word that clone3 was told to clear on exit
//         (CLONE_CHILD_CLEARTID writes 0 and wakes the futex when the thread exits)
//
// Sequence emitted (x86-64 Linux):
//
//	; loop: while (*rdi != 0) futex_wait(rdi, *rdi, NULL)
//	.retry:
//	  mov  eax, [rdi]          ; load current tid value
//	  test eax, eax            ; if already 0, thread already exited
//	  jz   .done
//	  mov  rsi, rax            ; futex val = current tid
//	  xor  edx, edx            ; timeout = NULL
//	  xor  r10d, r10d
//	  xor  r8d,  r8d
//	  xor  r9d,  r9d
//	  xor  eax, eax            ; FUTEX_WAIT = 0
//	  mov  eax, 202            ; SYS_futex
//	  syscall
//	  jmp  .retry
//	.done:
//	  ret
func emitJoinStub(obj *object.WasmObj, f FuncInfo) error {
	off := len(obj.Code)
	var code []byte

	retryOff := len(code)

	// mov eax, [rdi]  — 8B 07
	code = append(code, 0x8B, 0x07)

	// test eax, eax  — 85 C0
	code = append(code, 0x85, 0xC0)

	// jz .done  — 74 <rel8>  (patch below)
	code = append(code, 0x74, 0x00)
	jzOff := len(code) - 1

	// mov esi, eax  (futex val)  — 89 C6
	code = append(code, 0x89, 0xC6)

	// xor edx, edx  — 31 D2
	code = append(code, 0x31, 0xD2)

	// xor r10d, r10d  — 45 31 D2
	code = append(code, 0x45, 0x31, 0xD2)

	// xor r8d, r8d  — 45 31 C0
	code = append(code, 0x45, 0x31, 0xC0)

	// xor r9d, r9d  — 45 31 C9
	code = append(code, 0x45, 0x31, 0xC9)

	// mov eax, 202  (SYS_futex)  — B8 CA 00 00 00
	code = append(code, 0xB8)
	code = appendU32LE(code, 202)

	// syscall  — 0F 05
	code = append(code, 0x0F, 0x05)

	// jmp .retry  — EB <rel8>
	rel := byte(-(len(code) - retryOff) - 2)
	code = append(code, 0xEB, rel)

	// .done:
	code[jzOff] = byte(len(code) - int(jzOff) - 1)

	// ret
	code = append(code, 0xC3)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__thread_join_" + f.Name,
		Kind:   object.SymDefined,
		Offset: off,
	})
	obj.Code = append(obj.Code, code...)
	return nil
}

func appendU32LE(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}