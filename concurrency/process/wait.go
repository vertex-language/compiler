package process

import (
	"encoding/binary"

	"github.com/vertex-language/compiler/object"
)

// emitWaitStub emits a wait4-based process join.
//
// Emitted symbol: __process_wait_<name>
//
// Signature (SysV): func(pid i32) i32
//   rdi = child pid (i32, zero-extended)
//   returns exit status in rax
//
// Sequence emitted (x86-64 Linux):
//
//	; wait4(pid, &status, 0, NULL)
//	; rdi already = pid
//	lea  rsi, [rsp - 8]    ; &status — use red zone slot
//	xor  edx, edx          ; options = 0
//	xor  r10d, r10d        ; rusage  = NULL
//	mov  eax, 61           ; SYS_wait4 (amd64)
//	syscall
//	; decode exit status: (status >> 8) & 0xff = exit code
//	mov  eax, [rsp - 8]
//	shr  eax, 8
//	and  eax, 0xff
//	ret
func emitWaitStub(obj *object.WasmObj, f FuncInfo, arch string) error {
	off := len(obj.Code)
	var code []byte

	waitSyscall := uint32(SysWait4AMD64)
	if arch == "arm64" {
		waitSyscall = SysWait4ARM64
	}

	// lea rsi, [rsp - 8]  — 48 8D 74 24 F8
	code = append(code, 0x48, 0x8D, 0x74, 0x24, 0xF8)

	// xor edx, edx  — 31 D2
	code = append(code, 0x31, 0xD2)

	// xor r10d, r10d  — 45 31 D2
	code = append(code, 0x45, 0x31, 0xD2)

	// mov eax, <wait4 syscall>  — B8 imm32
	code = append(code, 0xB8)
	code = appendU32LE(code, waitSyscall)

	// syscall  — 0F 05
	code = append(code, 0x0F, 0x05)

	// mov eax, [rsp - 8]  — 8B 44 24 F8
	code = append(code, 0x8B, 0x44, 0x24, 0xF8)

	// shr eax, 8  — C1 E8 08
	code = append(code, 0xC1, 0xE8, 0x08)

	// and eax, 0xff  — 25 FF 00 00 00
	code = append(code, 0x25, 0xFF, 0x00, 0x00, 0x00)

	// ret
	code = append(code, 0xC3)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__process_wait_" + f.Name,
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