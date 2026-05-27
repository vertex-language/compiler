// cmd/sample_syscall/main.go
//
// Build and run:
//
//	go run main.go -o sample_syscall
//	./sample_syscall
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vertex-language/compiler"
	"github.com/vertex-language/compiler/linker/elf"
	"github.com/vertex-language/compiler/wasm"
)

func main() {
	outPath := flag.String("o", "sample_syscall", "output binary path")
	flag.Parse()

	m := buildModule()

	// 1. Compile the WASM module into a native object representation.
	obj, err := compiler.CompileWith(m, compiler.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile: %v\n", err)
		os.Exit(1)
	}

	// 2. Emit the native object bytes (ELF64 ET_REL format).
	objBytes, err := obj.Emit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "emit object: %v\n", err)
		os.Exit(1)
	}

	// 3. Parse the emitted object bytes for the linker.
	parsedObj, err := elf.ParseObject(objBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse object: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("compiled:  %d sections  %d symbols  %d relocations\n",
		len(parsedObj.Sections), len(parsedObj.Symbols), len(parsedObj.Relocs))

	// 4. Setup the ELF Linker for AMD64.
	lnk := elf.NewLinker(elf.EM_X86_64) // 0x3E
	
	// CRITICAL FIX: Use the synthesized _start wrapper as the entry point
	// instead of main, so the process terminates safely via sys_exit.
	lnk.SetEntry("_start")
	
	lnk.AddObject(parsedObj)

	// 5. Run the linker phases (resolution, merging, layout, patching).
	result, err := lnk.Link()
	if err != nil {
		fmt.Fprintf(os.Stderr, "link: %v\n", err)
		os.Exit(1)
	}

	// 6. Build and emit the final executable binary.
	bin, err := result.Builder().Emit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build elf: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outPath, bin, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote:     %s (%d bytes)\n", *outPath, len(bin))
}

const (
	fnWrite = uint32(0) // imported
	fnMain  = uint32(1) // locally defined
)

const (
	msg1    = "Hello from linux/kernel/syscalls!\n"
	msg2    = "No libc. No interpreter. Just the kernel.\n"
	msg1Off = int32(0)
	msg2Off = int32(len(msg1))
)

func buildModule() *wasm.Module {
	m := wasm.NewModule()

	// write(fd i32, buf ptr, count i32) → i32
	// The "@i32.ptr.i32" suffix tells the compiler that param 1 is a
	// linear-memory pointer and needs R15 translation before the syscall.
	tWrite := m.Types.AddFuncType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
		Results: []wasm.ValType{wasm.I32},
	})

	tMain := m.Types.AddFuncType(wasm.FuncType{
		Results: []wasm.ValType{wasm.I32},
	})

	// Note: Updated the namespace to use '/' instead of ':' per abi.go routing rules.
	m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)

	m.Functions.Add(tMain)
	m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
	m.Exports.Add("main", wasm.ExportFunc, fnMain)

	m.Datas.Add(
		wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)},
		[]byte(msg1+msg2),
	)

	m.Codes.Add(codeMain())
	return m
}

func codeMain() *wasm.FunctionBody {
	b := wasm.NewFunctionBody()

	b.I32Const(1)
	b.I32Const(msg1Off)
	b.I32Const(int32(len(msg1)))
	b.Call(fnWrite)
	b.Drop()

	b.I32Const(1)
	b.I32Const(msg2Off)
	b.I32Const(int32(len(msg2)))
	b.Call(fnWrite)
	b.Drop()

	b.I32Const(0)
	b.End()
	return b
}