package process

import (
	"fmt"

	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Compile wraps a @process function with a fork-based process launcher and
// a wait4 primitive.
//
// Output placed in the returned WasmObj:
//
//	.text — child entry wrapper  (reinitialises R15, calls function body)
//	        spawn stub           (fork/clone, returns pid to parent, enters
//	                              child entry wrapper in child)
//	        wait stub            (wait4 on child pid, returns exit code)
func Compile(m *wasm.Module, f FuncInfo, opts CompileOptions) (*object.WasmObj, error) {
	if opts.Arch == "" {
		opts.Arch = "amd64"
	}

	obj := &object.WasmObj{}

	if err := emitChildEntryWrapper(obj, f); err != nil {
		return nil, fmt.Errorf("process: %s: child entry: %w", f.Name, err)
	}
	if err := emitSpawnStub(obj, f, opts.Arch); err != nil {
		return nil, fmt.Errorf("process: %s: spawn stub: %w", f.Name, err)
	}
	if err := emitWaitStub(obj, f, opts.Arch); err != nil {
		return nil, fmt.Errorf("process: %s: wait stub: %w", f.Name, err)
	}

	return obj, nil
}

// emitChildEntryWrapper emits the entry point that runs in the child process.
// Reinitialises R15 then tail-calls the function body.
//
// Emitted symbol: __process_child_<name>
//
//	lea  r15, [rip + __wasm_data_base]
//	add  r15, 65536
//	jmp  <funcBody>
func emitChildEntryWrapper(obj *object.WasmObj, f FuncInfo) error {
	off := len(obj.Code)

	// lea r15, [rip + __wasm_data_base]  — 4D 8D 3D rel32
	obj.Code = append(obj.Code, 0x4D, 0x8D, 0x3D)
	obj.Relocs = append(obj.Relocs, object.Reloc{
		Offset: len(obj.Code),
		Symbol: "__wasm_data_base",
		Kind:   object.RelocRel32,
	})
	obj.Code = append(obj.Code, 0, 0, 0, 0)

	// add r15, 65536  — 49 81 C7 00 00 01 00
	obj.Code = append(obj.Code, 0x49, 0x81, 0xC7, 0x00, 0x00, 0x01, 0x00)

	// jmp <funcBody>  — E9 rel32
	obj.Code = append(obj.Code, 0xE9)
	obj.Relocs = append(obj.Relocs, object.Reloc{
		Offset: len(obj.Code),
		Symbol: f.Name,
		Kind:   object.RelocRel32,
	})
	obj.Code = append(obj.Code, 0, 0, 0, 0)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__process_child_" + f.Name,
		Kind:   object.SymDefined,
		Offset: off,
	})
	return nil
}

// emitSpawnStub emits the fork/clone spawn stub.
//
// Emitted symbol: __process_spawn_<name>
//
// Sequence (amd64 — SYS_fork = 57):
//
//	mov  eax, 57   ; SYS_fork
//	syscall
//	; rax = 0 in child, rax = child_pid in parent
//	test rax, rax
//	jnz  .parent   ; parent: return pid
//	; child: reinitialise R15 and jump into function body
//	jmp  __process_child_<name>
//	.parent:
//	ret            ; return child pid in rax
func emitSpawnStub(obj *object.WasmObj, f FuncInfo, arch string) error {
	off := len(obj.Code)

	var forkSyscall uint32
	switch arch {
	case "arm64":
		// arm64 has no fork; use clone with SIGCHLD
		forkSyscall = SysCloneARM64
	default:
		forkSyscall = SysForkAMD64
	}

	// mov eax, <syscall>  — B8 imm32
	obj.Code = append(obj.Code, 0xB8)
	obj.Code = appendU32LE(obj.Code, forkSyscall)

	// syscall  — 0F 05
	obj.Code = append(obj.Code, 0x0F, 0x05)

	// test rax, rax  — 48 85 C0
	obj.Code = append(obj.Code, 0x48, 0x85, 0xC0)

	// jnz .parent  — 75 <rel8>  (child path follows, so jump over it)
	obj.Code = append(obj.Code, 0x75, 0x00)
	jnzOff := len(obj.Code) - 1

	// child path: jmp __process_child_<name>  — E9 rel32
	obj.Code = append(obj.Code, 0xE9)
	obj.Relocs = append(obj.Relocs, object.Reloc{
		Offset: len(obj.Code),
		Symbol: "__process_child_" + f.Name,
		Kind:   object.RelocRel32,
	})
	obj.Code = append(obj.Code, 0, 0, 0, 0)

	// .parent:
	obj.Code[jnzOff] = byte(len(obj.Code) - int(jnzOff) - 1)

	// ret
	obj.Code = append(obj.Code, 0xC3)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__process_spawn_" + f.Name,
		Kind:   object.SymDefined,
		Offset: off,
	})
	return nil
}