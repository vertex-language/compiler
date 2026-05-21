# Vertex Compiler Framework

A compiler backend framework for language implementers. Write your frontend,
pass it WebAssembly — Vertex handles everything from there: native code
generation, syscall inlining, pointer translation, memory allocation, native
concurrency, and ELF emission, with zero runtime dependencies.

---

## Philosophy

Most compiler frameworks make you choose between portability and performance.
Vertex doesn't. It uses **WebAssembly as its internal IR** — a well-specified,
tooling-rich, validated binary format — and compiles it directly to native
machine code with no interpreter, no JIT warmup, and no libc required.

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
- Full Wasm 2.0 instruction set: i32/i64, memory, globals, sign-extension,
  reference types, bulk memory ops

### Native Code Generation
- Direct AOT compilation — no interpreter, no VM
- Currently targeting **x86-64 (amd64)**; ARM64 in progress
- Stack-machine to register-machine lowering with SysV ABI compliance
- Locals, control flow, loops, branches, `br_table` all fully lowered

### Auto Linker with Import Signatures
Import names carry their full calling convention in a `@`-suffix signature. Your
frontend never needs to know register layouts, pointer sizes, or platform ABIs:

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

### Built-in Memory Allocator
Import from the `memory` module and the driver automatically injects a
full native allocator — no external malloc, no libc heap. Three allocator
strategies are available:

| API | Strategy | Use case |
|-----|----------|----------|
| `memory.heap.*` | Segregated free-list (11 size classes) + bump | General-purpose heap |
| `memory.ref.*` | Reference-counted blocks with optional destructor | Owned, shared data |
| `memory.arena.*` | Checkpoint-stack bump allocator | Scratch / per-request memory |

All returned pointers are valid 32-bit Wasm offsets. Physical pages are
demand-committed. The allocator is injected once by the driver when any
`memory` import is detected — no source changes needed.

### Native Concurrency
Three independent concurrency models are available via export-suffix routing.
Mark a function with a suffix and the driver emits the correct native stubs
automatically. A module that only uses `@thread` pays zero cost for coroutine
or process code.

| Suffix | Model | Kernel primitive |
|--------|-------|-----------------|
| `@async` | Stackful cooperative coroutines | `mmap` + symmetric context switch |
| `@thread` | OS threads | `clone(2)` |
| `@process` | Child processes | `fork(2)` |

R15 (the Wasm linear-memory base) is correctly propagated across all three
models. Stack allocation, context switching, `clone`/`fork`/`wait`, and handle
lifecycle are entirely invisible to the language frontend.

### GPU Kernel Routing
Export a function with a target suffix (e.g. `@cuda`, `@spirv`, `@msl`) and
the driver routes it out of the CPU compilation bucket and into the appropriate
GPU backend. CPU and GPU functions share the same `BuildContext` and link into
a single object.

### Platform Routing
The import module field encodes where the symbol lives:

| Module field | Route |
|---|---|
| `linux:kernel/syscalls` | Inlined syscall — no relocation |
| `windows:kernel32` | IAT entry (PE/COFF) |
| `darwin:libSystem` | LC_LOAD_DYLIB stub |
| `c` | Cross-platform libc (lib prefix auto-added on Linux/macOS) |
| `sdl2` | Any bare library name |

### ELF Output
- Produces a fully linked, executable ELF64 binary
- Entry stub aligns the stack, calls your `main`, and issues `exit_group`
- Supports dynamic linking (PLT/GOT/RELA) for shared library imports
- TLS support (`.tdata` / `.tbss`)
- Archive (`.a`) ingestion with incremental symbol resolution

---

## Quick Start

```go
package main

import (
    "os"

    "github.com/vertex-language/compiler"
    "github.com/vertex-language/compiler/linker"
    "github.com/vertex-language/compiler/object"
    "github.com/vertex-language/compiler/wasm"
)

func main() {
    m := wasm.NewModule()

    tWrite := m.Types.AddFuncType(wasm.FuncType{
        Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
        Results: []wasm.ValType{wasm.I32},
    })
    tMain := m.Types.AddFuncType(wasm.FuncType{
        Results: []wasm.ValType{wasm.I32},
    })

    // Import write(2) directly from the kernel — no libc
    m.Imports.AddFunc("linux:kernel/syscalls", "write@i32.ptr.i32", tWrite)

    m.Functions.Add(tMain)
    m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
    m.Exports.Add("main", wasm.ExportFunc, 1)

    msg := "Hello, world!\n"
    m.Datas.Add(wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)}, []byte(msg))

    b := wasm.NewFunctionBody()
    b.I32Const(1)
    b.I32Const(0)
    b.I32Const(int32(len(msg)))
    b.Call(0)
    b.Drop()
    b.I32Const(0)
    b.End()
    m.Codes.Add(b)

    obj, _ := compiler.CompileWith(m, compiler.Options{})

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
├── compiler.go         # Top-level Compile / CompileWith / CompileFor entry points
│
├── wasm/               # WebAssembly IR
│   ├── module.go       # Module and all section types
│   ├── sections.go     # TypeSection, ImportSection, FunctionSection, ...
│   ├── types.go        # ValType, FuncType, ConstExpr, block types
│   └── body.go         # FunctionBody builder — full Wasm 2.0 instruction set
│
├── encoder/            # wasm.Module → .wasm binary (for debug/validation)
├── decoder/            # .wasm binary → wasm.Module
├── encode/             # LEB128, vector encoding helpers
├── decode/             # Binary reader (LEB128, sub-readers, raw slices)
│
├── driver/             # Compilation pipeline orchestration
│   ├── driver.go       # Driver — registers targets, sequences the pipeline
│   ├── analyze.go      # Analyze — import/export ABI parsing, routing table
│   └── target.go       # Target interface (ID, Emit)
│
├── context/
│   └── build.go        # BuildContext — shared state flowing through the pipeline
│
├── cpu/
│   └── x86_64/         # amd64 native code generation backend
│       ├── target.go   # driver.Target implementation
│       ├── compile.go  # Module-level compilation, import routing, relocs
│       ├── func.go     # Per-function compiler state, prologue/epilogue
│       ├── body.go     # Instruction dispatch loop
│       ├── dispatch.go # Control flow, variables, memory, constants, ref types
│       ├── math.go     # Arithmetic, comparisons, conversions
│       ├── control.go  # Blocks, loops, if/else, br, br_table, calls
│       ├── arith.go    # Globals, select, CLZ/CTZ, binary helpers
│       ├── memory.go   # Linear memory loads, stores, memory.fill
│       ├── emit.go     # Operand-stack push/pop, pointer translation
│       ├── syscall.go  # Inline syscall emission
│       └── registers.go # Register constants, REX/ModRM/SIB encoding
│
├── gpu/                # GPU backend targets (PTX, SPIR-V, MSL)
│
├── memory/             # Native allocator stubs (heap, ref-count, arena)
├── concurrency/        # Native concurrency stubs (@async, @thread, @process)
│
├── platform/           # Import module routing
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

| Token | Meaning |
|-------|---------|
| `i32` | 32-bit integer — passed as-is |
| `i64` | 64-bit integer — passed as-is |
| `f32` | 32-bit float — passed as-is |
| `f64` | 64-bit float — passed as-is |
| `ptr` | Linear-memory i32 offset — automatically translated to native VA before the call |

For **syscall imports**, pointer translation is unconditional (`add reg, r15`).
Offset 0 is a valid buffer location; no NULL guard is applied.

For **library imports**, a safe translation is used: NULL (offset 0) passes
through as NULL without the R15 addition, matching C convention.

---

## Compiler Pipeline

```
wasm.Module
    │
    ▼
driver.Analyze          ← parse import/export ABI signatures
    │                     build RoutingTable (funcIdx → backend ID)
    │                     set ctx.NeedsMemory / NeedsAsync / NeedsThread / NeedsProcess
    │
    ├── memory.Emit     ← inject heap / ref-count / arena allocator stubs (if needed)
    ├── concurrency.Emit← inject @async / @thread / @process stubs (if needed)
    │
    └── target.Emit     ← per-backend code generation (x86_64, cuda, spirv, ...)
            │             writes machine code + symbols directly into ctx.Obj
            ▼
    object.WasmObj      → linker.Link → ELF binary
```

---

## Debugging Your IR

Because Vertex's IR is spec-compliant WebAssembly, the debug loop requires no
native compilation at all:

```bash
# Install the WebAssembly Binary Toolkit
apt install wabt   # or: brew install wabt

# Inspect the IR in human-readable form
wasm2wat out.wasm -o out.wat && cat out.wat

# Validate against the spec
wasm-validate out.wasm

# Run in a managed sandbox
wasmtime out.wasm

# Compile to native when ready
go run ./cmd/sample_syscall -o my_binary && ./my_binary
```

Get your IR correct first, then compile to native.

---

## Architecture Support

| Architecture | Status | Syscall table |
|---|---|---|
| x86-64 | Supported | Linux 6.x |
| ARM64 | In progress | Linux 6.x |
| RISC-V 64 | Planned | |

---

## v1 Limitations

- All backends (CPU, memory, concurrency) are currently **amd64 only**.
- Non-PIE executables only — native code pointers are truncated to 32 bits for
  `ref.func` and concurrency spawn.
- `memory.grow` always returns -1; `call_indirect` is not yet implemented.
- Floating-point arithmetic, comparisons, and conversions are not yet implemented.
- Large heap blocks (class 10) are bump-allocated and never reclaimed on free.
- `heap_alloc_aligned` ignores the alignment argument.
- Coroutines are not thread-safe; do not share a `CoroHandle` across threads.
- Detached thread stacks are leaked if the thread is still running when its
  handle is released.

---

## License

See [MIT](LICENSE).