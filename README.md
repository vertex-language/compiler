# Vertex Compiler Framework

A compiler backend framework for language implementers. Write your frontend, pass it WebAssembly — Vertex handles everything from there: native code generation, syscall inlining, pointer translation, memory allocation, native concurrency, and binary emission, with zero runtime dependencies.

---

## Philosophy

Most compiler frameworks make you choose between portability and performance. Vertex doesn't. It uses **WebAssembly as its internal IR** — a well-specified, tooling-rich, validated binary format — and compiles it directly to native machine code with no interpreter, no JIT warmup, and no libc required.

Your frontend emits standard `.wasm`. Vertex turns it into a native binary that calls the kernel directly.

```
Your Language → WebAssembly IR → Native x86-64 / ARM64 → ELF / PE / Mach-O Binary
```

Because the IR is valid WebAssembly, you get the entire wasm ecosystem for free: `wasm2wat`, `wasm-validate`, browser runtimes, WASI hosts — all usable as debug and validation targets without changing a line of your frontend.

---

## Features

### WebAssembly as IR
- Emit **100% spec-compliant WebAssembly** from your frontend
- Validate with standard tooling: `wasm2wat output.wasm -o out.wat`
- Run in any wasm runtime for debugging before compiling to native
- Full Wasm 2.0 instruction set: i32/i64/f32/f64, memory, globals, sign-extension, reference types, bulk memory ops

### Native Code Generation
- Direct AOT compilation — no interpreter, no VM
- **x86-64 (amd64)** and **AArch64 (arm64)** targets, both fully implemented
- Stack-machine to register-machine lowering with SysV ABI compliance
- Locals, control flow, loops, branches, `br_table` all fully lowered

### Import Signature Syntax

Import names carry their full calling convention in a `@`-suffix signature. Your frontend never needs to know register layouts, pointer sizes, or platform ABIs:

```
"write@i32.ptr.i32"        →  fd=i32, buf=ptr (auto-translated), count=i32
"fopen@ptr.ptr:hptr"       →  path=ptr, mode=ptr, returns native handle
"getpid"                   →  no pointer params, no suffix needed
```

**Type tokens:**

| Token | Meaning |
|-------|---------|
| `i32` | 32-bit integer — passed as-is |
| `i64` | 64-bit integer — passed as-is |
| `f32` | 32-bit float — passed as-is |
| `f64` | 64-bit float — passed as-is |
| `ptr` | Linear-memory i32 offset — auto-translated to native VA (`+ R15`) before call |
| `hptr` | Opaque native handle — resolved via Handle Table before call, or registered on return |

### Import Path Routing

The import module field is the single source of truth for how the compiler emits a call:

| Module prefix | Route |
|---|---|
| `linux/kernel/*` | Inline syscall instruction — no libc, no PLT, no relocation |
| `linux/*` | Linked against Linux system library |
| `windows/*` | Windows system DLL — IAT entry |
| `darwin/*` | macOS system library — `LC_LOAD_DYLIB` stub |
| `lib/*` | Third-party library — fetched and compiled via vcpkg |
| `hw/bios/*` | Bare metal BIOS — inline `int 0xNN` instruction |
| `hw/uefi/*` | UEFI firmware services — `EFI_SYSTEM_TABLE` vtable chase |
| `gpu/cuda` | NVIDIA kernel — PTX emission |
| `gpu/msl` | Apple Metal kernel — MSL emission (macOS only) |
| `gpu/vulkan` | Vulkan compute kernel — SPIR-V emission |

### Direct Kernel Syscalls (Linux)

Import from `linux/kernel/syscalls` and the compiler **inlines the syscall sequence directly** — no libc, no PLT, no dynamic linker required:

```go
m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)
```

Compiles to:

```asm
add  rsi, r15        ; translate linear-memory ptr → native VA
mov  eax, 1          ; SYS_write
syscall
```

Full syscall tables for amd64 and arm64 are included, sourced from Linux 6.x.

### Built-in Memory Allocator

Import from the `memory` module and the driver automatically injects a full native allocator — no external malloc, no libc heap. Three strategies are available:

| API | Strategy | Use case |
|-----|----------|----------|
| `memory.heap.*` | Segregated free-list (11 size classes) + bump | General-purpose heap |
| `memory.ref.*` | Reference-counted blocks with optional destructor | Owned, shared data |
| `memory.arena.*` | Checkpoint-stack bump allocator | Scratch / per-request memory |

All returned pointers are valid 32-bit Wasm offsets. Physical pages are demand-committed. The allocator is injected once by the driver when any `memory` import is detected — no source changes needed.

### Native Concurrency

Three independent concurrency models are available via export-suffix routing. A module that only uses `@thread` pays zero cost for coroutine or process code.

| Suffix | Model | Kernel primitive |
|--------|-------|-----------------|
| `@async` | Stackful cooperative coroutines | `mmap` + symmetric context switch |
| `@thread` | OS threads | `clone(2)` |
| `@process` | Child processes | `fork(2)` |

R15 (the Wasm linear-memory base) is correctly propagated across all three models.

### GPU Kernel Routing

Export a function with a target suffix (`@cuda`, `@vulkan`, `@msl`) and the driver routes it out of the CPU compilation bucket into the appropriate GPU backend. CPU and GPU functions share the same `BuildContext` and link into a single object.

### Binary Emission

The `bin` package serializes sections, symbols, and relocation data into valid native binary files. The linker drives it through the same builder API available to any other caller.

| Package | Format | Platform |
|---|---|---|
| `bin/elf` | ELF64 | Linux, \*BSD |
| `bin/pe` | PE32+ | Windows |
| `bin/macho` | Mach-O 64 | macOS, iOS |
| `bin/bootloader/raw` | Flat binary | Bare metal, stage 1 |
| `bin/bootloader/uefi` | PE32+ | UEFI firmware |

---

## Quick Start

```go
package main

import (
    "fmt"
    "os"

    "github.com/vertex-language/compiler"
    "github.com/vertex-language/compiler/linker/elf"
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
    m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)

    m.Functions.Add(tMain)
    m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
    m.Exports.Add("main", wasm.ExportFunc, 1)

    msg := "Hello, world!\n"
    m.Datas.Add(wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)}, []byte(msg))

    body := wasm.NewFunctionBody()
    body.I32Const(1)
    body.I32Const(0)
    body.I32Const(int32(len(msg)))
    body.Call(0)
    body.Drop()
    body.I32Const(0)
    body.End()
    m.Codes.Add(body)

    // Compile to a native object (ELF64 ET_REL)
    obj, err := compiler.CompileWith(m, compiler.Options{})
    if err != nil {
        fmt.Fprintln(os.Stderr, "compile:", err)
        os.Exit(1)
    }

    objBytes, err := obj.Emit()
    if err != nil {
        fmt.Fprintln(os.Stderr, "emit object:", err)
        os.Exit(1)
    }

    // Link into a final executable
    parsedObj, err := elf.ParseObject(objBytes)
    if err != nil {
        fmt.Fprintln(os.Stderr, "parse object:", err)
        os.Exit(1)
    }

    lnk := elf.NewLinker(elf.EM_X86_64)
    lnk.SetEntry("_start")
    lnk.AddObject(parsedObj)

    result, err := lnk.Link()
    if err != nil {
        fmt.Fprintln(os.Stderr, "link:", err)
        os.Exit(1)
    }

    bin, err := result.Builder().Emit()
    if err != nil {
        fmt.Fprintln(os.Stderr, "build elf:", err)
        os.Exit(1)
    }

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

## Compiler Pipeline

```
wasm.Module
    │
    ▼
driver.Analyze          ← parse import/export ABI signatures
    │                     build RoutingTable (funcIdx → backend ID)
    │                     populate ctx.ImportPtrMasks / HptrMasks / KernelParams
    │                     set ctx.NeedsMemory
    │
    ├── memory.Emit     ← inject heap / ref-count / arena allocator stubs (if needed)
    │
    └── target.Emit     ← per-backend code generation (amd64, arm64, cuda, spirv, …)
            │             writes machine code + symbols + relocs into ctx.Obj
            ▼
    object.Object  (ELF64 ET_REL / COFF .obj / Mach-O MH_OBJECT)
            │
            ▼
    linker/{elf,pe,macho}
            │
            ▼
    bin/{elf,pe,macho}  → executable binary
```

---

## Package Layout

```
vertex-language/compiler/
├── compiler.go             # Compile / CompileWith / CompileFor entry points
│
├── wasm/                   # WebAssembly IR builder and data model
│   ├── module.go           # Module and all section types
│   ├── sections.go         # TypeSection, ImportSection, FunctionSection, …
│   ├── types.go            # ValType, FuncType, ConstExpr, block types
│   └── body.go             # FunctionBody builder — full Wasm 2.0 instruction set
│
├── encoder/                # wasm.Module → .wasm binary (for debug/validation)
├── decoder/                # .wasm binary → wasm.Module
├── encode/                 # LEB128, vector encoding helpers
├── decode/                 # Binary reader (LEB128, sub-readers, raw slices)
│
├── abi/                    # Import path parsing, export suffix parsing, sig parsing
│
├── driver/                 # Compilation pipeline orchestration
│   ├── driver.go           # Driver — registers targets, sequences the pipeline
│   ├── analyze.go          # Analyze — ABI parsing, routing table construction
│   └── target.go           # Target interface (ID, Emit)
│
├── context/
│   └── build.go            # BuildContext — shared state flowing through the pipeline
│
├── cpu/
│   ├── amd64/              # x86-64 native code generation backend
│   │   ├── target.go
│   │   ├── body.go
│   │   ├── control.go
│   │   ├── dispatch.go
│   │   ├── arith.go
│   │   ├── math.go
│   │   ├── emit.go
│   │   ├── func.go
│   │   ├── memory.go
│   │   ├── registers.go
│   │   ├── syscall.go
│   │   ├── asm/            # Hand-written amd64 assembly fragments
│   │   └── memory/         # amd64-specific allocator stubs
│   │
│   └── arm64/              # AArch64 native code generation backend
│       ├── target.go
│       ├── body.go
│       ├── control.go
│       ├── dispatch.go
│       ├── arith.go
│       ├── math.go
│       ├── emit.go
│       ├── func.go
│       ├── memory.go
│       ├── registers.go
│       ├── syscall.go
│       ├── reloc.go
│       ├── asm/            # Hand-written arm64 assembly fragments
│       └── memory/         # arm64-specific allocator stubs
│
├── gpu/                    # GPU backend targets (PTX, SPIR-V, MSL)
│
├── object/                 # Platform-neutral object interface
│   ├── object.go           # Object, Section, BSSSection, Symbol, Reloc
│   ├── elf/                # ELF64 ET_REL adapter
│   ├── pe/                 # COFF PE32+ .obj adapter
│   └── macho/              # Mach-O MH_OBJECT adapter
│
├── linker/                 # Static linkers
│   ├── elf/                # ELF64 linker (Linux / *BSD)
│   ├── pe/                 # PE32+ linker (Windows)
│   └── macho/              # Mach-O linker (macOS)
│
├── bin/                    # Binary file emitters
│   ├── elf/                # ELF64 builder
│   ├── pe/                 # PE32+ builder
│   ├── macho/              # Mach-O 64 builder
│   └── bootloader/
│       ├── raw/            # Flat binary (stage 1 bootloaders)
│       └── uefi/           # UEFI PE32+ images
│
├── docs/                   # Extended reference documentation
│
└── cmd/
    └── sample_syscall/     # End-to-end example: write(2) via inline syscall
```

---

## Debugging Your IR

Because Vertex's IR is spec-compliant WebAssembly, the debug loop requires no native compilation at all:

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
| x86-64 (amd64) | Supported | Linux 6.x |
| AArch64 (arm64) | Supported | Linux 6.x |
| RISC-V 64 | Planned | — |

---

## v1 Limitations

- Non-PIE executables only — native code pointers are truncated to 32 bits for `ref.func` and concurrency spawn.
- `memory.grow` always returns -1; `call_indirect` is not yet implemented.
- Floating-point arithmetic, comparisons, and conversions are not yet implemented.
- Large heap blocks (class 10) are bump-allocated and never reclaimed on free.
- `heap.alloc_aligned` ignores the alignment argument.
- Coroutines are not thread-safe; do not share a `CoroHandle` across threads.
- Detached thread stacks are leaked if the thread is still running when its handle is released.

---

## License

MIT