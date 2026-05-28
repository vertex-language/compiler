// cmd/sample_syscall/main.go
//
// Demonstrates the unified linker.Build API.
// The frontend declares function signatures via the @-suffix ABI syntax and
// calls linker.Build — system library resolution, DT_NEEDED wiring, and
// interpreter path selection are all handled automatically.
//
// Build and run:
//
//	go run main.go
//	./sample_syscall
package main

import (
	"fmt"
	"os"

	"github.com/vertex-language/compiler/linker"
	"github.com/vertex-language/compiler/wasm"
)

func main() {
	bin, err := linker.Build(buildModule(), linker.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	const outPath = "sample_syscall"
	if err := os.WriteFile(outPath, bin, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s (%d bytes)\n", outPath, len(bin))
}

// ────────────────────────────────────────────────────────────────────────────
// Module construction
//
// The frontend's only job is:
//   1. Declare the right wasm types.
//   2. Import functions with the correct @-suffix ABI signature.
//   3. Write the function body.
//
// Everything else — which .so to link, what DT_NEEDED to emit, whether to add
// PT_INTERP, how to translate linear-memory pointers — is inferred from the
// import module path and the @-suffix and handled by linker.Build.
// ────────────────────────────────────────────────────────────────────────────

const (
	// Function indices in the order imports are declared, then locals follow.
	fnWrite = uint32(0) // import: linux/kernel/syscalls · write
	fnMain  = uint32(1) // local:  main
)

const (
	msg1 = "Hello from linux/kernel/syscalls!\n"
	msg2 = "No libc. No interpreter. Just the kernel.\n"

	msg1Off = int32(0)
	msg2Off = int32(len(msg1))
)

func buildModule() *wasm.Module {
	m := wasm.NewModule()

	// ── Types ──────────────────────────────────────────────────────────────

	// write(fd i32, buf ptr, count i32) → i32
	//
	// The "ptr" token in "@i32.ptr.i32" tells the compiler that parameter 1
	// is a linear-memory offset that needs native address translation
	// (+ R15) before the syscall is emitted. The frontend does not need to
	// know the register layout or the syscall number — it only declares the
	// logical type of each parameter.
	tWrite := m.Types.AddFuncType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
		Results: []wasm.ValType{wasm.I32},
	})

	tMain := m.Types.AddFuncType(wasm.FuncType{
		Results: []wasm.ValType{wasm.I32},
	})

	// ── Imports ────────────────────────────────────────────────────────────

	// "linux/kernel/syscalls" → inline syscall, no PLT, no libc.
	// linker.Build sees no system library here, so no DT_NEEDED is emitted
	// and no PT_INTERP is added. The binary has zero dynamic dependencies.
	m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)

	// ── Locals, memory, exports ────────────────────────────────────────────

	m.Functions.Add(tMain)
	m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
	m.Exports.Add("main", wasm.ExportFunc, fnMain)

	// Pack both messages into the active data segment at offset 0.
	m.Datas.Add(
		wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)},
		[]byte(msg1+msg2),
	)

	m.Codes.Add(codeMain())
	return m
}

func codeMain() *wasm.FunctionBody {
	b := wasm.NewFunctionBody()

	// write(1, msg1, len(msg1))
	b.I32Const(1)
	b.I32Const(msg1Off)
	b.I32Const(int32(len(msg1)))
	b.Call(fnWrite)
	b.Drop()

	// write(1, msg2, len(msg2))
	b.I32Const(1)
	b.I32Const(msg2Off)
	b.I32Const(int32(len(msg2)))
	b.Call(fnWrite)
	b.Drop()

	b.I32Const(0) // exit code
	b.End()
	return b
}