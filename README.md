# Vertex Compiler Framework

A modern compiler backend framework for language implementers. Write your frontend,
pass it WebAssembly — Vertex handles everything from there: native code generation,
syscall inlining, pointer translation, linking, and ELF emission, with zero runtime
dependencies.

---

## Philosophy

Most compiler frameworks make you choose between portability and performance.
Vertex doesn't. It uses **WebAssembly as its internal IR** — a well-specified,
tooling-rich, validated binary format — and compiles it directly to native machine
code with no interpreter, no JIT warmup, and no libc required.

Your frontend emits standard `.wasm`. Vertex turns it into a native binary that
calls the kernel directly.

```
Your Language → WebAssembly IR → Native x86-64 / ARM64 / ... → ELF Binary
```

Because the IR is valid WebAssembly, you get the entire wasm ecosystem for free:
`wasm2wat`, `wasm-validate`, browser runtimes, WASI hosts — all usable as debug
and validation targets without changing a line of your frontend.

---

## Features

### WebAssembly as IR
- Emit **100% spec-compliant WebAssembly** from your frontend
- Validate with standard tooling: `wasm2wat output.wasm -o out.wat`
- Run in any wasm runtime for debugging before compiling to native
- Full Wasm 2.0 instruction set: i32/i64/f32/f64, memory, tables, globals,
  sign-extension, reference types, bulk memory ops

### Native Code Generation
- Direct AOT compilation — no interpreter, no VM
- Currently targeting **x86-64 (amd64)**; ARM64 and others in the pipeline
- Stack-machine to register-machine lowering with SysV ABI compliance
- Locals, control flow, loops, branches, br_table all fully lowered

### Auto Linker with Import Signatures
The most distinctive feature. Import names carry their full calling convention
in a `@`-suffix signature. Your frontend never needs to know register layouts,
pointer sizes, or platform ABIs:

```
"write@i32.ptr.i32"       →  fd=i32, buf=ptr (auto-translated), count=i32
"open@ptr.i32.i32"        →  path=ptr, flags=i32, mode=i32
"mmap@ptr.i64.i32.i32.i32.i64"
```

**Type tokens:** `i32`  `i64`  `f32`  `f64`  `ptr`

`ptr` marks a WebAssembly linear-memory offset that the compiler will
automatically translate to a native virtual address before the call. No
boilerplate. No manual `add r15` sequences. The IR is self-describing.

### Direct Kernel Syscalls (Linux)
Import from `linux:kernel/syscalls` and the compiler **inlines the syscall
sequence directly** — no libc, no PLT, no dynamic linker required:

```go
m.Imports.AddFunc("linux:kernel/syscalls", "write@i32.ptr.i32", tWrite)
```

Compiles to:
```asm
add  rsi, r15        ; translate linear-memory ptr → native VA
mov  r10, rcx        ; Linux 4th-arg ABI (not rcx)
mov  eax, 1          ; SYS_write
syscall
```

Full syscall tables for amd64 and arm64 are included, sourced from Linux 6.x.

### Platform Routing
The import module field encodes where the symbol lives:

| Module field            | Route                              |
|-------------------------|------------------------------------|
| `linux:kernel/syscalls` | Inlined syscall — no relocation    |
| `windows:kernel32`      | IAT entry (PE/COFF)                |
| `darwin:libSystem`      | LC_LOAD_DYLIB stub                 |
| `c`                     | Cross-platform libc (libprefix auto-added on Linux/macOS) |
| `sdl2`                  | Any bare library name              |

### ELF Output
- Produces a fully linked, executable ELF64 binary
- Entry stub aligns the stack, calls your `main`, and issues `exit_group`
- Supports dynamic linking (PLT/GOT/RELA) for shared library imports
- TLS support (`.tdata` / `.tbss`)
- Archive (`.a`) ingestion with incremental symbol resolution

### Debug via wasm2wat
Because the IR is real WebAssembly, the debug loop is:

```bash
# Compile your language to wasm
your-frontend source.mylang -o out.wasm

# Inspect the IR in human-readable form
wasm2wat out.wasm -o out.wat

# Validate against the spec
wasm-validate out.wasm

# Run in a wasm runtime (no native compilation needed)
wasmtime out.wasm

# Compile to native when ready
go run ./cmd/sample_syscall -o my_binary
./my_binary
```

---

## Quick Start

### The Minimal Example

```go
package main

import (
    "github.com/vertex-language/compiler"
    "github.com/vertex-language/compiler/linker"
    "github.com/vertex-language/compiler/object"
    "github.com/vertex-language/compiler/wasm"
)

func main() {
    m := wasm.NewModule()

    // Declare types
    tWrite := m.Types.AddFuncType(wasm.FuncType{
        Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
        Results: []wasm.ValType{wasm.I32},
    })
    tMain := m.Types.AddFuncType(wasm.FuncType{
        Results: []wasm.ValType{wasm.I32},
    })

    // Import write(2) directly from the kernel — no libc
    // "@i32.ptr.i32" tells the compiler param 1 is a linear-memory pointer
    m.Imports.AddFunc("linux:kernel/syscalls", "write@i32.ptr.i32", tWrite)

    // Define main
    m.Functions.Add(tMain)
    m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
    m.Exports.Add("main", wasm.ExportFunc, 1)

    msg := "Hello, world!\n"
    m.Datas.Add(wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)}, []byte(msg))

    // Build the function body
    b := wasm.NewFunctionBody()
    b.I32Const(1)                    // fd = stdout
    b.I32Const(0)                    // buf offset in linear memory
    b.I32Const(int32(len(msg)))      // count
    b.Call(0)                        // call write
    b.Drop()
    b.I32Const(0)                    // return 0
    b.End()
    m.Codes.Add(b)

    // Compile
    obj, _ := compiler.CompileWith(m, compiler.Options{})

    // Link to ELF
    bin, _ := linker.Link([]*object.WasmObj{obj}, linker.Options{
        Output: linker.ELF,
        Entry:  "main",
    })

    os.WriteFile("hello", bin, 0o755)
}
```

```bash
go run main.go
./hello
# Hello, world!
```

No libc. No dynamic linker. No interpreter. Just the kernel.

---

## Package Layout

```
vertex-language/compiler/
├── wasm/               # WebAssembly IR
│   ├── module.go       # Module, all section types
│   ├── sections.go     # TypeSection, ImportSection, FunctionSection, ...
│   ├── types.go        # ValType, FuncType, ConstExpr, block types
│   └── body.go         # FunctionBody builder — full Wasm 2.0 instruction set
│
├── encoder/            # wasm.Module → .wasm binary (for debug/validation)
├── decoder/            # .wasm binary → wasm.Module
├── encode/             # LEB128, vector encoding helpers
├── decode/             # Binary reader (LEB128, sub-readers, raw slices)
├── gpu/             
│
├── cpu/           # Architecture dispatch
│   └── x86_64/
│       ├── compile.go  # Module-level compilation, import routing, relocs
│       ├── func.go     # Per-function compiler state
│       ├── body.go     # Instruction dispatch loop (dispatchOp)
│       ├── emit.go     # push/pop/load/store/local encoding helpers
│       ├── arith.go    # Arithmetic, comparisons, clz/ctz, select, globals
│       ├── memory.go   # Linear memory loads, stores, memory.fill
│       ├── control.go  # Blocks, loops, if/else, br, br_table, calls
│       ├── syscall.go  # Inline syscall emission
│       └── registers.go # Register constants, REX/ModRM/SIB encoding
│
├── platform/           # Import module routing (syscall / platform lib / cross-platform)
│   └── linux/
│       ├── linux.go    # SyscallNumber() lookup
│       ├── amd64.go    # x86-64 syscall table (Linux 6.x)
│       └── arm64.go    # AArch64 syscall table (Linux 6.x)
│
├── linker/             # ELF64 linker
│   ├── linker.go       # Symbol resolution, relocation, archive ingestion
│   └── output/         # ELF64 layout and emission, PLT/GOT/RELA/TLS
│
├── object/             # Object file format (WasmObj — code, data, symbols, relocs)
│
└── cmd/
    └── sample_syscall/ # End-to-end example: write(2) via inline syscall
```

---

## Import Signature Reference

### Syntax
```
"<name>@<type>.<type>.<type>..."
```

The types correspond to the Wasm function parameters in order.

| Token | Meaning                                                           |
|-------|-------------------------------------------------------------------|
| `i32` | 32-bit integer — passed as-is                                     |
| `i64` | 64-bit integer — passed as-is                                     |
| `f32` | 32-bit float — passed as-is                                       |
| `f64` | 64-bit float — passed as-is                                       |
| `ptr` | Linear-memory i32 offset — **automatically translated to native VA** before the call |

Examples:

```go
// POSIX write(2)
"write@i32.ptr.i32"

// POSIX open(2)  
"open@ptr.i32.i32"

// mmap(2)
"mmap@ptr.i64.i32.i32.i32.i64"

// A function with no pointer params — no @ suffix needed
"getpid"
```

### Pointer Translation

For **syscall imports** (`linux:kernel/syscalls`), pointer translation is
unconditional — `add <reg>, r15` is always emitted. Linear-memory offset 0
is a valid buffer location, so no NULL guard is applied.

For **library imports** (libc, platform libs), a safe translation is used:
NULL (offset 0) is passed through as NULL without the R15 addition, matching
the C convention where NULL means "no pointer."

---

## Platform Routing Reference

```go
// Direct Linux kernel syscall — inlined, no relocation
m.Imports.AddFunc("linux:kernel/syscalls", "write@i32.ptr.i32", t)

// Windows system DLL
m.Imports.AddFunc("windows:kernel32", "WriteFile@...", t)

// macOS system library  
m.Imports.AddFunc("darwin:libSystem", "write@i32.ptr.i32", t)

// Cross-platform libc (lib prefix added automatically on Linux/macOS)
m.Imports.AddFunc("c", "malloc@i32", t)

// Any shared library by name
m.Imports.AddFunc("sdl2", "SDL_Init@i32", t)
```

---

## Validating Your IR

Because Vertex's IR is spec-compliant WebAssembly, you can validate it at any
point without touching the native backend:

```bash
# Install the WebAssembly Binary Toolkit
apt install wabt   # or brew install wabt

# Disassemble to text format
wasm2wat your_output.wasm -o out.wat
cat out.wat        # human-readable, inspectable

# Validate against the Wasm spec
wasm-validate your_output.wasm

# Run in a managed runtime (sandboxed, great for logic testing)
wasmtime your_output.wasm
wasmer your_output.wasm
```

This is the primary debug loop. Get your IR correct first, then compile to
native.

---

## Architecture Support

| Architecture | Status          | Syscall table |
|--------------|-----------------|---------------|
| x86-64       | ✅ Supported    | Linux 6.x     |
| ARM64        | 🔄 In progress  | Linux 6.x     |
| RISC-V 64    | 📋 Planned      |               |

---

## Roadmap

The following are planned features. All will maintain 100% valid Wasm bytecode
as the IR.

### Async / Coroutines
Stackful coroutine support via wasm continuation-passing. Suspend and resume
wasm functions natively without OS threads.

### Threading
Multi-threaded wasm modules using the [threads proposal](https://github.com/WebAssembly/threads):
shared memories, `atomic.*` instructions, and `wait`/`notify`. Compiles to
native `lock`-prefixed instructions and `futex` syscalls on Linux.

### Multi-Processing
`fork`/`clone` wrappers that correctly snapshot and re-initialise the wasm
linear memory base register (R15) in the child process.

### GPU-Accelerated Kernels
Import interface for compute kernels: emit a wasm function body, mark it as
a GPU target, and let Vertex lower it to PTX (NVIDIA) or SPIR-V (Vulkan/AMD)
and dispatch via the appropriate driver API.

---

## Design Notes

### Why WebAssembly as IR?

1. **Validated tooling exists.** `wasm-validate`, `wasm2wat`, browser devtools,
   standalone runtimes — you get a complete debug ecosystem without building one.
2. **Portable and well-specified.** The Wasm spec is unambiguous. Your frontend
   targets a stable, versioned format.
3. **Security sandbox for free.** Running as native wasm gives you a memory-safe
   execution environment for testing before committing to native.
4. **The IR maps cleanly to native.** Wasm's stack machine is straightforward to
   lower to a register machine. The type system is simple. Control flow is
   structured. There are no surprises.

### The R15 Convention

R15 is permanently reserved as the **linear memory base register**. All wasm
memory accesses are encoded as `[R15 + RCX + offset]`. Pointer parameters
marked `ptr` in the import signature have R15 added before the call. This gives
the compiler a uniform, relocatable memory model with a single register
reservation.

### No libc, No Runtime

The goal is to let a language frontend produce a self-contained native binary
with nothing between it and the kernel. The entry stub aligns the stack, calls
your entry point, and issues `exit_group`. That's the entire runtime.

---

## License

See [MIT](LICENSE).