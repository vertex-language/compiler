# cpu/

Native code generation for the Vertex compiler. Translates WebAssembly modules
into executable machine code, with a clean separation between the raw x86-64
assembler primitive layer and the wasm-to-native compiler built on top of it.

---

## Package layout

```
cpu/
└── x86_64/
    ├── asm/            ← raw x86-64 emitter — no wasm, no object files
    ├── compile.go      ← module-level compiler: imports, data, exports, relocs
    ├── func.go         ← per-function compiler state and prologue/epilogue
    ├── body.go         ← wasm instruction dispatch loop
    ├── emit.go         ← wasm operand-stack push/pop, locals, pointer translation
    ├── arith.go        ← arithmetic, comparisons, clz/ctz, select, globals
    ├── memory.go       ← linear-memory loads, stores, memory.fill
    ├── control.go      ← blocks, loops, if/else, br, br_table, calls
    ├── syscall.go      ← inline syscall emission
    └── registers.go    ← wasm-specific constants (MemBase, re-exports from asm/)
```

---

## The asm/ layer

`cpu/x86_64/asm` is a self-contained x86-64 machine-code buffer. It knows
nothing about WebAssembly, object files, symbols, or relocations. Its only job
is to turn method calls into correct byte sequences.

```go
import "github.com/vertex-language/compiler/cpu/x86_64/asm"

var a asm.Assembler

a.Push(asm.RBP)
a.MovRR(asm.RBP, asm.RSP)
a.SubRI(asm.RSP, 32)
// ...
a.Pop(asm.RBP)
a.Ret()

machineCode := a.Bytes()
```

### What asm/ provides

**Register constants** — `asm.RAX` through `asm.R15`, plus `asm.ArgRegs[6]`.

**Encoding helpers** — `asm.REX`, `asm.REXW`, `asm.REXWB`, `asm.ModRM`,
`asm.SIB`, `asm.SIBNoIndex`, `asm.NeedsSIB`, `asm.Fits8`, and the full set of
little-endian `Put*` / `Append*` helpers. These are exported so any package
that needs to hand-encode an unusual instruction can do so without reimplementing
the boilerplate.

**Instruction emitters** — every method returns nothing except the conditional
jumps, which return the offset of their rel32 field so the caller can
back-patch it:

```go
zeroPath := a.JzRel32()          // emit jz, get patch offset
a.MovRI32(asm.RAX, 42)
endPatch  := a.JmpRel32()        // emit jmp, get patch offset
a.Patch32(zeroPath, a.Pos())     // resolve the jz to here
a.MovRI32(asm.RAX, 0)
a.Patch32(endPatch,  a.Pos())    // resolve the jmp to here
```

**Patching** — four targeted methods keep patching explicit and type-safe:

| Method | Use |
|---|---|
| `Patch32(off, target)` | Back-patch a rel32: writes `target − (off+4)` |
| `Patch8(off, target)` | Back-patch a rel8 |
| `Patch32Abs(off, v)` | Write a raw uint32 — for jump tables and inline values |
| `SetByte(off, v)` | Write a raw byte — for short-jump back-patching |

**Frame helpers** — `Prologue(regs []int) int` and `Epilogue(regs []int, align int)`
handle the standard push-rbp / callee-save / RSP alignment sequence and its
mirror. They compute and return the alignment padding so the epilogue always
matches the prologue exactly.

**Linux syscall helpers** — `MmapFixed(length)`, `CheckMmapError() int`,
`ExitGroup(code)`, and `Syscall()` encode the sequences that both the wasm
compiler and the memory allocator stubs need without either package
reimplementing them.

### What asm/ does not do

- It does not track symbols or relocations. A `LeaRIPRel32(dst)` call emits the
  instruction and returns the offset of the zero displacement field. Recording
  `object.Reloc{Offset: off, Symbol: "foo"}` is the caller's job.
- It does not know about the wasm operand stack, control flow, or the R15
  linear-memory base convention. Those concepts live in the `x86_64` package
  that embeds it.

---

## The x86_64 compiler layer

`cpu/x86_64` translates a `wasm.Module` into a `*object.WasmObj`. It embeds
`asm.Assembler` inside `funcCompiler` so all instruction-emission methods are
available without a named field:

```go
type funcCompiler struct {
    asm.Assembler          // Push, Pop, LoadMem64, JzRel32, Patch32, ...

    m              *wasm.Module
    ft             wasm.FuncType
    depth          int     // wasm operand-stack depth
    dead           bool
    ctrl           []ctrlFrame
    relocs         []funcReloc
    // ...
}
```

The wasm-specific stack discipline sits on top of the raw assembler in `emit.go`:

```go
// emitPushR wraps asm.Push and increments the wasm stack depth counter.
func (fc *funcCompiler) emitPushR(reg int) { fc.Push(reg); fc.depth++ }
func (fc *funcCompiler) emitPopR(reg int)  { fc.Pop(reg);  fc.depth-- }
```

Callers inside the `x86_64` package always use `emitPushR`/`emitPopR` (never
`fc.Push`/`fc.Pop` directly) so the depth counter stays consistent with the
actual register stack.

### R15 convention

R15 is permanently reserved as the **linear-memory base register**. It is loaded
once in every function prologue:

```
lea r15, [rip + __wasm_data_base]
add r15, 65536
```

All wasm memory accesses are encoded as `[R15 + RCX + offset]`. Pointer
parameters marked `ptr` in an import signature have R15 added before the call.
The `asm.Assembler` has no knowledge of this convention; it is enforced
entirely by the `x86_64` package.

### Import signature routing

Import names carry their calling convention in a `@`-suffix signature that the
compiler parses at compile time:

```
"write@i32.ptr.i32"   →  fd=i32, buf=ptr (auto-translated), count=i32
"open@ptr.i32.i32"    →  path=ptr, flags=i32, mode=i32
"getpid"              →  no @ suffix → no pointer translation
```

The `ptr` token means: before the call, emit `add <arg_reg>, r15` to translate
the wasm linear-memory offset into a native virtual address.

For `linux:kernel/syscalls` imports the compiler inlines the entire syscall
sequence. No relocation is emitted and no PLT entry is generated:

```asm
add  rsi, r15        ; ptr arg translation
mov  r10, rcx        ; Linux 4th-arg ABI fix (rcx → r10)
mov  eax, <number>   ; syscall number
syscall
```

---

## How memory/ uses asm/

The `memory/` package generates the native allocator stubs
(`__vertex_memory_heap_alloc`, `__vertex_memory_ref_retain`, etc.) that the
linker merges into every binary. These stubs are hand-written x86-64 — they are
not wasm functions run through the compiler — so they need the same raw
encoding primitives.

`memory/` imports `asm/` directly and builds its own `emitter` struct that
wraps an `asm.Assembler` and adds symbol and relocation tracking:

```
memory/
├── emitter.go   ← wraps asm.Assembler; adds syms/relocs, codeLabel, dataLabel
├── init.go      ← __vertex_memory_init  (mmap heap + arena)
├── heap.go      ← __vertex_memory_heap_alloc / _raw / _aligned / _free / _realloc
├── ref.go       ← __vertex_memory_ref_alloc / retain / release / weak / upgrade
├── arena.go     ← __vertex_memory_arena_push / pop / alloc
├── debug.go     ← __vertex_memory_debug_tag
└── emit.go      ← Compile() → *CompileResult{Obj *object.WasmObj}
```

The `emitter` in `memory/` is not a second parallel assembler. It is a thin
accounting wrapper around the shared `asm.Assembler`:

```go
// memory/emitter.go

type emitter struct {
    asm.Assembler                // all x86-64 instruction methods live here
    syms   []object.Symbol
    relocs []object.Reloc
    data   []byte
}

// codeLabel records the current code position as a named symbol.
func (e *emitter) codeLabel(name string) {
    e.syms = append(e.syms, object.Symbol{
        Name:   name,
        Kind:   object.SymDefined,
        Offset: e.Pos(),
    })
}

// rel32Sym emits a zero rel32 placeholder and records the relocation.
func (e *emitter) rel32Sym(sym string) {
    e.relocs = append(e.relocs, object.Reloc{
        Offset: e.Pos(),
        Symbol: sym,
        Kind:   object.RelocRel32,
    })
    e.Emit(0, 0, 0, 0)
}

// leaRIPSym emits: lea dst, [rip + sym]
func (e *emitter) leaRIPSym(dst int, sym string) {
    e.Emit(asm.REXW(dst, -1), 0x8D, byte(0x05|((dst&7)<<3)))
    e.rel32Sym(sym)
}

// callSym emits: call sym
func (e *emitter) callSym(sym string) {
    e.Emit(0xE8)
    e.rel32Sym(sym)
}
```

A stub in `memory/init.go` looks exactly like a stub in `cpu/x86_64/syscall.go`:
both call the same `asm.Assembler` methods, both use `Patch32` for forward
references, and both call `MmapFixed` / `CheckMmapError` without copying a
single byte of encoding logic:

```go
// memory/init.go  (simplified)
func emitInit(e *emitter) {
    e.codeLabel("__vertex_memory_init")
    align := e.Prologue([]int{asm.RBX, asm.R12, asm.R13})

    // Map heap region.
    e.MovRR(asm.RDI, asm.RBX)
    e.AddRI(asm.RDI, HeapOffset)
    e.MmapFixed(HeapSize)               // from asm/
    failH := e.CheckMmapError()         // from asm/
    e.MovRR(asm.R12, asm.RAX)

    // Map arena region.
    e.MovRR(asm.RDI, asm.RBX)
    e.AddRI(asm.RDI, ArenaOffset)
    e.MmapFixed(ArenaSize)
    failA := e.CheckMmapError()

    e.Epilogue([]int{asm.RBX, asm.R12, asm.R13}, align)

    failTarget := e.Pos()
    e.Patch32(failH, failTarget)
    e.Patch32(failA, failTarget)
    e.ExitGroup(127)
}
```

### Integration with the linker

`memory.Compile()` returns a `*object.WasmObj` containing the allocator stubs
in `.text` and the zeroed allocator-state block in `.data`. The top-level
`compiler.CompileWith` merges this object with the CPU object before linking
so the linker sees a single unified symbol table:

```
wasm frontend
     │
     ▼
wasm.Module
     │
     ├──► cpu/x86_64.Compile()  →  cpuObj   ┐
     │                                       ├─► linker.Link() → ELF binary
     └──► memory.Compile()      →  allocObj  ┘
```

Neither the CPU compiler nor the allocator generator duplicates encoding logic.
Both delegate everything below the instruction level to `cpu/x86_64/asm`.

---

## Architecture support

| Architecture | Status         | Syscall table |
|---|---|---|
| x86-64       | ✅ Supported   | Linux 6.x     |
| ARM64        | 🔄 In progress | Linux 6.x     |
| RISC-V 64    | 📋 Planned     |               |

When ARM64 is added, a new `cpu/arm64/asm/` package will follow the same
pattern: register constants, encoding helpers, and an `Assembler` struct with
instruction methods. The `memory/` package will grow a build-tag branch that
swaps its `asm/x86_64` import for `asm/arm64` without changing the stub logic
above the instruction level.