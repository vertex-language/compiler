// cmd/memory/main.go
//
// Demonstrates the memory package allocator stubs end-to-end.
//
// Build and run:
//
//	go run main.go -o memory_demo -wasm memory_demo.wasm
//	./memory_demo ; echo "exit $?"
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vertex-language/compiler"
	"github.com/vertex-language/compiler/encoder"
	"github.com/vertex-language/compiler/linker"
	"github.com/vertex-language/compiler/memory"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

func main() {
	outPath := flag.String("o", "memory_demo", "output binary path")
	wasmPath := flag.String("wasm", "memory_demo.wasm", "output wasm binary path")
	flag.Parse()

	m := buildModule()

	// ── Debug: Dump Wasm Binary ───────────────────────────────────────────────

	if binWasm, err := encoder.Encode(m); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to encode wasm: %v\n", err)
	} else if err := os.WriteFile(*wasmPath, binWasm, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to write wasm file: %v\n", err)
	} else {
		fmt.Printf("wrote  %s\n", *wasmPath)
	}

	// ──────────────────────────────────────────────────────────────────────────

	// Compile the wasm module. 
	// Under the new architecture, the compilation driver automatically detects 
	// "memory" imports and injects the allocator stubs directly into the shared object.
	obj, err := compiler.CompileWith(m, compiler.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("compiled %5d B  %d syms  %d relocs\n",
		len(obj.Code), len(obj.Symbols), len(obj.Relocs))

	// Link the single unified object into a Linux ELF binary.
	bin, err := linker.Link(
		[]*object.WasmObj{obj},
		linker.Options{Output: linker.ELF, Entry: "main"},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "link: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outPath, bin, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote  %s (%d bytes)\n", *outPath, len(bin))
}

// ── Function-index constants ──────────────────────────────────────────────────
//
// Import order defines the function index space; locals follow.

const (
	fnWrite      = uint32(0) // linux:kernel/syscalls  write
	fnHeapAlloc  = uint32(1) // memory  heap.alloc
	fnHeapFree   = uint32(2) // memory  heap.free
	fnArenaPush  = uint32(3) // memory  arena.push
	fnArenaPop   = uint32(4) // memory  arena.pop
	fnArenaAlloc = uint32(5) // memory  arena.alloc
	fnMain       = uint32(6) // local
)

const (
	msg    = "memory OK\n"
	msgOff = int32(0)
)

func buildModule() *wasm.Module {
	m := wasm.NewModule()

	// ── Types ─────────────────────────────────────────────────────────────────

	tWrite := m.Types.AddFuncType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
		Results: []wasm.ValType{wasm.I32},
	})
	tAlloc := m.Types.AddFuncType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.I32},
		Results: []wasm.ValType{wasm.I32},
	})
	tFree := m.Types.AddFuncType(wasm.FuncType{
		Params: []wasm.ValType{wasm.I32},
	})
	tVoid := m.Types.AddFuncType(wasm.FuncType{})
	tMain := m.Types.AddFuncType(wasm.FuncType{
		Results: []wasm.ValType{wasm.I32},
	})

	// ── Imports ───────────────────────────────────────────────────────────────

	// Syscall: write(fd, buf ptr, count) → bytes_written
	m.Imports.AddFunc("linux:kernel/syscalls", "write@i32.ptr.i32", tWrite)

	// Heap allocator.
	m.Imports.AddFunc(memory.ImportModule, "heap.alloc", tAlloc)
	m.Imports.AddFunc(memory.ImportModule, "heap.free",  tFree)

	// Arena (bump / stack) allocator.
	m.Imports.AddFunc(memory.ImportModule, "arena.push",  tVoid)
	m.Imports.AddFunc(memory.ImportModule, "arena.pop",   tVoid)
	m.Imports.AddFunc(memory.ImportModule, "arena.alloc", tAlloc)

	// ── Local function, memory, exports ───────────────────────────────────────

	m.Functions.Add(tMain)
	m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
	m.Exports.Add("main", wasm.ExportFunc, fnMain)

	// Data segment: output message at linear-memory offset 0.
	m.Datas.Add(
		wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)},
		[]byte(msg),
	)

	m.Codes.Add(codeMain())
	return m
}

// codeMain demonstrates heap.alloc/free and arena.push/alloc/pop.
//
// Logic (wasm stack machine):
//
//  1. heap.alloc(8)          — allocate 8 bytes; ptr stays on stack
//  2. heap.free(ptr)         — ptr consumed; block returned to free-list
//  3. arena.push             — checkpoint arena bump pointer
//  4. arena.alloc(32) → drop — bump-allocate scratch space, ignore ptr
//  5. arena.pop              — reclaim everything since the push
//  6. write(1, msgOff, len)  — print "memory OK\n" to stdout
//  7. return 0               — success exit code
func codeMain() *wasm.FunctionBody {
	b := wasm.NewFunctionBody()

	// ── heap ──────────────────────────────────────────────────────────────────

	b.I32Const(8)
	b.Call(fnHeapAlloc) // → ptr

	b.Call(fnHeapFree)  // ptr consumed

	// ── arena ─────────────────────────────────────────────────────────────────

	b.Call(fnArenaPush)

	b.I32Const(32)
	b.Call(fnArenaAlloc) // → scratch ptr
	b.Drop()

	b.Call(fnArenaPop)

	// ── write ─────────────────────────────────────────────────────────────────

	b.I32Const(1)                    // fd = stdout
	b.I32Const(msgOff)               // buf offset in linear memory
	b.I32Const(int32(len(msg)))      // byte count
	b.Call(fnWrite)
	b.Drop()                         // discard return value

	// ── return ────────────────────────────────────────────────────────────────

	b.I32Const(0)
	b.End()
	return b
}