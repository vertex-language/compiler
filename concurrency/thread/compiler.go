package thread

import (
	"fmt"

	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Compile wraps a @thread function with a clone3-based thread launcher and a
// futex join primitive.
//
// Output placed in the returned WasmObj:
//
//	.text — thread entry wrapper  (reinitialises R15, calls function body)
//	        spawn stub            (allocates stack, fills clone_args, clone3)
//	        join stub             (futex FUTEX_WAIT on child_tid word)
func Compile(m *wasm.Module, f FuncInfo, opts CompileOptions) (*object.WasmObj, error) {
	if opts.StackSize == 0 {
		opts.StackSize = DefaultStackSize
	}

	obj := &object.WasmObj{}

	if err := emitEntryWrapper(obj, f); err != nil {
		return nil, fmt.Errorf("thread: %s: entry wrapper: %w", f.Name, err)
	}
	if err := emitSpawnStub(obj, f, opts.StackSize); err != nil {
		return nil, fmt.Errorf("thread: %s: spawn stub: %w", f.Name, err)
	}
	if err := emitJoinStub(obj, f); err != nil {
		return nil, fmt.Errorf("thread: %s: join stub: %w", f.Name, err)
	}

	return obj, nil
}

// emitEntryWrapper emits the native entry point the kernel jumps to when the
// new thread starts. It reinitialises R15 from the data base relocation and
// then tail-calls the actual function body.
//
// Emitted symbol: __thread_entry_<name>
//
//	; R15 = linear memory base (re-establish — clone3 does not copy registers)
//	lea  r15, [rip + __wasm_data_base]
//	add  r15, 65536
//	; tail-call the compiled function body
//	jmp  <funcBody>
func emitEntryWrapper(obj *object.WasmObj, f FuncInfo) error {
	off := len(obj.Code)

	// lea r15, [rip + __wasm_data_base]  — 4D 8D 3D rel32
	obj.Code = append(obj.Code, 0x4D, 0x8D, 0x3D)
	obj.Relocs = append(obj.Relocs, object.Reloc{
		Offset: len(obj.Code),
		Symbol: "__wasm_data_base",
		Kind:   object.RelocRel32,
	})
	obj.Code = append(obj.Code, 0, 0, 0, 0)

	// add r15, 65536  — 49 81 C7 imm32
	obj.Code = append(obj.Code, 0x49, 0x81, 0xC7)
	obj.Code = append(obj.Code, 0x00, 0x00, 0x01, 0x00) // 65536 LE

	// jmp <funcBody>  — E9 rel32  (linker resolves)
	obj.Code = append(obj.Code, 0xE9)
	obj.Relocs = append(obj.Relocs, object.Reloc{
		Offset: len(obj.Code),
		Symbol: f.Name,
		Kind:   object.RelocRel32,
	})
	obj.Code = append(obj.Code, 0, 0, 0, 0)

	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__thread_entry_" + f.Name,
		Kind:   object.SymDefined,
		Offset: off,
	})
	return nil
}

// emitSpawnStub emits the thread spawn stub.
// TODO: allocate stack via mmap, fill clone_args on the stack, call clone3.
func emitSpawnStub(obj *object.WasmObj, f FuncInfo, stackSize int) error {
	// TODO: emit clone_args setup + clone3 (syscall 435)
	obj.Symbols = append(obj.Symbols, object.Symbol{
		Name:   "__thread_spawn_" + f.Name,
		Kind:   object.SymDefined,
		Offset: len(obj.Code),
	})
	// Placeholder: ud2 (will trap — signals unimplemented)
	obj.Code = append(obj.Code, 0x0F, 0x0B)
	return fmt.Errorf("thread spawn stub: %w", errNotImplemented)
}

var errNotImplemented = fmt.Errorf("not yet implemented")