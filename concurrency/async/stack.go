package async

import (
	"encoding/binary"

	"github.com/vertex-language/compiler/object"
)

// emitStackAllocStub emits a native stub that allocates a coroutine stack via
// the Linux mmap syscall and returns a pointer to the top of the new stack.
//
// Emitted symbol: __coro_alloc_stack_<name>
//
// Signature (SysV): func() *void
//
// Sequence emitted (x86-64 Linux):
//
//	xor  edi, edi               ; addr   = NULL
//	mov  esi, <stackSize>       ; length = StackSize
//	mov  edx, 3                 ; prot   = PROT_READ | PROT_WRITE
//	mov  r10d, 0x22             ; flags  = MAP_PRIVATE | MAP_ANONYMOUS
//	mov  r8d,  -1               ; fd     = -1
//	xor  r9d,  r9d              ; offset = 0
//	mov  eax,  9                ; SYS_mmap
//	syscall
//	; rax = base of new stack page
//	; return rax + stackSize    ; stack top (stacks grow down)
//	add  rax, <stackSize>
//	ret
func emitStackAllocStub(obj *object.WasmObj, f FuncInfo, stackSize int) error {
	var code []byte

	// xor edi, edi
	code = append(code, 0x31, 0xFF)

	// mov esi, stackSize  (imm32)
	code = append(code, 0xBE)
	code = appendU32LE(code, uint32(stackSize))

	// mov edx, 3  (PROT_READ | PROT_WRITE)
	code = append(code, 0xBA, 0x03, 0x00, 0x00, 0x00)

	// mov r10d, 0x22  (MAP_PRIVATE | MAP_ANONYMOUS)
	// REX.B + mov r10d, imm32 → 41 BA imm32
	code = append(code, 0x41, 0xBA)
	code = appendU32LE(code, 0x22)

	// mov r8d, -1  (fd = -1)
	// REX.B + mov r8d, imm32 → 41 B8 imm32
	code = append(code, 0x41, 0xB8)
	code = appendU32LE(code, 0xFFFFFFFF)

	// xor r9d, r9d  (offset = 0)
	// REX.B + xor r9d, r9d → 45 31 C9
	code = append(code, 0x45, 0x31, 0xC9)

	// mov eax, 9  (SYS_mmap)
	code = append(code, 0xB8, 0x09, 0x00, 0x00, 0x00)

	// syscall
	code = append(code, 0x0F, 0x05)

	// add rax, stackSize  (return stack top — stacks grow down)
	// REX.W + add rax, imm32 → 48 05 imm32
	code = append(code, 0x48, 0x05)
	code = appendU32LE(code, uint32(stackSize))

	// ret
	code = append(code, 0xC3)

	symName := "__coro_alloc_stack_" + f.Name
	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   symName,
		Kind:   object.SymDefined,
		Offset: len(obj.Code),
	})
	obj.Code = append(obj.Code, code...)
	return nil
}

func appendU32LE(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}