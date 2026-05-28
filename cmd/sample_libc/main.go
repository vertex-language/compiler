// cmd/sample_libc/main.go
//
// Same output as sample_syscall, but routed through linux/libc instead of
// linux/kernel/syscalls. The only change from the frontend's perspective is
// the import module path and the adjusted @-suffix (no fd parameter for puts).
//
// linker.Build automatically adds libc.so.6 as a DT_NEEDED entry and emits
// the correct PT_INTERP for the target architecture. The frontend does not
// configure any of this.
//
// Build and run:
//
//	go run main.go
//	./sample_libc
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

	const outPath = "sample_libc"
	if err := os.WriteFile(outPath, bin, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s (%d bytes)\n", outPath, len(bin))
}

const (
	// Import order → local order.
	fnPuts   = uint32(0) // import: linux/libc · puts
	fnMain   = uint32(1) // local:  main
)

const (
	msg1 = "Hello from linux/libc!\n"
	msg2 = "libc linked automatically — no linker config needed.\n"

	msg1Off = int32(0)
	msg2Off = int32(len(msg1))
)

func buildModule() *wasm.Module {
	m := wasm.NewModule()

	// ── Types ──────────────────────────────────────────────────────────────

	// puts(s ptr) → i32
	//
	// "ptr" means: this is a linear-memory offset. The compiler adds R15
	// before the call so libc receives a native virtual address.
	tPuts := m.Types.AddFuncType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.I32},
		Results: []wasm.ValType{wasm.I32},
	})

	tMain := m.Types.AddFuncType(wasm.FuncType{
		Results: []wasm.ValType{wasm.I32},
	})

	// ── Imports ────────────────────────────────────────────────────────────

	// "linux/libc" → linked against libc.so.6 via DT_NEEDED.
	// linker.Build resolves the soname from the abi/linux table, stat-walks
	// the candidate paths, and adds the shared object. PT_INTERP is emitted
	// automatically because a dynamic dependency is present.
	m.Imports.AddFunc("linux/libc", "puts@ptr", tPuts)

	// ── Locals, memory, exports ────────────────────────────────────────────

	m.Functions.Add(tMain)
	m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
	m.Exports.Add("main", wasm.ExportFunc, fnMain)

	m.Datas.Add(
		wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)},
		// puts appends its own newline; strip the trailing \n from each
		// string so we don't double-space, or just leave them and enjoy.
		[]byte(msg1+msg2),
	)

	m.Codes.Add(codeMain())
	return m
}

func codeMain() *wasm.FunctionBody {
	b := wasm.NewFunctionBody()

	b.I32Const(msg1Off)
	b.Call(fnPuts)
	b.Drop()

	b.I32Const(msg2Off)
	b.Call(fnPuts)
	b.Drop()

	b.I32Const(0)
	b.End()
	return b
}