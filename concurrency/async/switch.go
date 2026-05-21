package async

import "github.com/vertex-language/compiler/object"

// emitSuspendStub emits the suspend trampoline for a coroutine.
//
// Emitted symbol: __coro_suspend_<name>
//
// The stub is called by the coroutine body when it hits a suspend point.
// rdi = pointer to the caller-allocated Context block (64 bytes).
//
// Sequence emitted (x86-64):
//
//	; Save callee-saved registers + R15 (MemBase) into *ctx (rdi)
//	mov  [rdi + CtxRBP], rbp
//	mov  [rdi + CtxRBX], rbx
//	mov  [rdi + CtxR12], r12
//	mov  [rdi + CtxR13], r13
//	mov  [rdi + CtxR14], r14
//	mov  [rdi + CtxR15], r15   ; linear memory base
//	; Save current rsp — points just above the saved return address
//	mov  [rdi + CtxRSP], rsp
//	; Pop return address into ctx.rip so resume knows where to re-enter
//	pop  rax
//	mov  [rdi + CtxRIP], rax
//	; Return to whoever called the coroutine (the scheduler / coro.resume)
//	ret
func emitSuspendStub(obj *object.WasmObj, f FuncInfo) error {
	var code []byte

	// mov [rdi + CtxRBP], rbp  — REX.W 89 ModRM(mod=01, reg=rbp=5, rm=rdi=7) disp8
	code = append(code, 0x48, 0x89, 0x6F, byte(CtxRBP))

	// mov [rdi + CtxRBX], rbx
	code = append(code, 0x48, 0x89, 0x5F, byte(CtxRBX))

	// mov [rdi + CtxR12], r12  — REX.WR 89 ModRM(mod=01, reg=r12=4, rm=rdi=7) disp8
	code = append(code, 0x4C, 0x89, 0x67, byte(CtxR12))

	// mov [rdi + CtxR13], r13
	code = append(code, 0x4C, 0x89, 0x6F, byte(CtxR13))

	// mov [rdi + CtxR14], r14
	code = append(code, 0x4C, 0x89, 0x77, byte(CtxR14))

	// mov [rdi + CtxR15], r15  (MemBase)
	code = append(code, 0x4C, 0x89, 0x7F, byte(CtxR15))

	// mov [rdi + CtxRSP], rsp
	code = append(code, 0x48, 0x89, 0x67, byte(CtxRSP))

	// pop rax  — grab return address from stack
	code = append(code, 0x58)

	// mov [rdi + CtxRIP], rax
	code = append(code, 0x48, 0x89, 0x47, byte(CtxRIP))

	// ret
	code = append(code, 0xC3)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__coro_suspend_" + f.Name,
		Kind:   object.SymDefined,
		Offset: len(obj.Code),
	})
	obj.Code = append(obj.Code, code...)
	return nil
}

// emitResumeStub emits the resume trampoline for a coroutine.
//
// Emitted symbol: __coro_resume_<name>
//
// rdi = pointer to the Context block saved by a previous suspend.
//
// Sequence emitted (x86-64):
//
//	; Restore callee-saved registers from *ctx
//	mov  rbp, [rdi + CtxRBP]
//	mov  rbx, [rdi + CtxRBX]
//	mov  r12, [rdi + CtxR12]
//	mov  r13, [rdi + CtxR13]
//	mov  r14, [rdi + CtxR14]
//	mov  r15, [rdi + CtxR15]  ; restore MemBase
//	; Switch to the coroutine stack
//	mov  rsp, [rdi + CtxRSP]
//	; Jump back into the coroutine body
//	jmp  [rdi + CtxRIP]
func emitResumeStub(obj *object.WasmObj, f FuncInfo) error {
	var code []byte

	// mov rbp, [rdi + CtxRBP]
	code = append(code, 0x48, 0x8B, 0x6F, byte(CtxRBP))

	// mov rbx, [rdi + CtxRBX]
	code = append(code, 0x48, 0x8B, 0x5F, byte(CtxRBX))

	// mov r12, [rdi + CtxR12]  — REX.WR 8B ModRM(mod=01, reg=r12=4, rm=rdi=7) disp8
	code = append(code, 0x4C, 0x8B, 0x67, byte(CtxR12))

	// mov r13, [rdi + CtxR13]
	code = append(code, 0x4C, 0x8B, 0x6F, byte(CtxR13))

	// mov r14, [rdi + CtxR14]
	code = append(code, 0x4C, 0x8B, 0x77, byte(CtxR14))

	// mov r15, [rdi + CtxR15]  (MemBase)
	code = append(code, 0x4C, 0x8B, 0x7F, byte(CtxR15))

	// mov rsp, [rdi + CtxRSP]
	code = append(code, 0x48, 0x8B, 0x67, byte(CtxRSP))

	// jmp [rdi + CtxRIP]  — FF /4  ModRM(mod=01, reg=4, rm=rdi=7) disp8
	code = append(code, 0xFF, 0x67, byte(CtxRIP))

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__coro_resume_" + f.Name,
		Kind:   object.SymDefined,
		Offset: len(obj.Code),
	})
	obj.Code = append(obj.Code, code...)
	return nil
}