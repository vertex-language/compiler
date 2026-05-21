# cpu/x86_64

The `cpu/x86_64` package is the native code-generation backend for the `amd64`
architecture. It translates WebAssembly function bodies into x86-64 machine
code, handles data segment layout, resolves imports (syscall inline, platform
library, and internal Vertex allocator), generates callback trampolines, and
applies all relocations. It implements the `driver.Target` interface and is
registered by the top-level compiler under the `"amd64"` target ID.

---

## Package structure

| File | Responsibility |
|------|---------------|
| `target.go` | `driver.Target` implementation; entry point for the driver pipeline |
| `compile.go` | `moduleCompiler` — full module compilation: data layout, import processing, function compilation loop, export symbols, trampolines, reloc application |
| `func.go` | `funcCompiler` and `compileFuncBody` — per-function state, prologue/epilogue |
| `body.go` | Main instruction dispatch loop and dead-code skipping |
| `dispatch.go` | `dispatchOp` — opcode-level dispatch for control flow, variables, memory, constants, and reference types |
| `math.go` | `dispatchMath` — arithmetic, comparisons, and conversions (`0x45`–`0xC4`) |
| `control.go` | Branch, return, `br_table`, and `call`/`call_indirect` emission |
| `arith.go` | Global variable access, `select`, binary helper shims, CLZ/CTZ |
| `memory.go` | Linear-memory load/store emission and `memory.fill` |
| `emit.go` | Wasm operand-stack push/pop accounting and pointer translation helpers |
| `syscall.go` | Platform import routing and inline syscall emission |
| `registers.go` | Register constant aliases and `ArgRegs` |

---

## Register conventions

| Register | Role |
|----------|------|
| R15 | **Wasm linear-memory base** (`MemBase`). Callee-saved; never written by generated Wasm code. Set in every function prologue via `lea r15, [rip + __wasm_data_base]` + `add r15, 65536`. Also inherited across `clone`/`fork` in concurrency stubs. |
| RBP | Frame pointer. Standard SysV frame layout. |
| RAX | Primary accumulator; return value. |
| RCX | Memory address index in SIB loads/stores; shift count; scratch. |
| ArgRegs | RDI, RSI, RDX, RCX, R8, R9 — SysV integer argument registers. |

Globals are stored at `[R15 − 8*(idx+1)]` — the reserved region immediately
below the linear-memory base.

---

## Memory layout

All offsets are relative to the start of `obj.Data` / the `__wasm_data_base`
symbol.

```
obj.Data[0 .. 65535]              reserved — R15 guard region
                                    globals at [−8], [−16], … from R15
obj.Data[65536 + segment.offset]  active data segments
```

R15 is loaded as `__wasm_data_base + 65536`, so a Wasm linear-memory offset of
`0` maps to `obj.Data[65536]`, not `obj.Data[0]`. The region below 65536 is
used for globals (negative displacements from R15) and as a shadow stack.

The minimum data buffer size is `max(declared_pages × 65536 + 65536,
shadowStackTop + 65536)` where `shadowStackTop = 0x10_0000` (1 MiB).

---

## Function prologue and epilogue

Every compiled function gets a standard SysV frame:

```asm
push  rbp
mov   rbp, rsp
sub   rsp, <frameSize>          ; frameSize = (nParams + nLocals) * 8, 16-byte aligned
mov   [rbp−8],  rdi             ; spill up to 6 params
mov   [rbp−16], rsi
...
xor   eax, eax
mov   [rbp−N],  rax             ; zero-initialise declared locals

lea   r15, [rip + __wasm_data_base]   ; relocatable
add   r15, 65536
```

Epilogue:

```asm
pop   rax                       ; result (if any)
mov   rsp, rbp
pop   rbp
ret
```

The Wasm value stack is the native hardware stack. `emitPushR`/`emitPopR` wrap
`push`/`pop` with a `depth` counter used by the control-flow engine.

---

## Wasm operand stack

The Wasm operand stack is mapped directly onto the x86-64 hardware stack.
Every Wasm value occupies an 8-byte slot regardless of type. `depth` tracks the
current stack depth; control frames record `baseDepth` so branches can trim
excess stack slots with `add rsp, N` before jumping.

---

## Control flow

Control frames are maintained in `fc.ctrl` as a `[]ctrlFrame`. Each frame
carries:

- `kind` — `block`, `loop`, or `if`
- `arity` / `paramArity` — result and parameter counts
- `baseDepth` — stack depth at entry (used to compute excess on branch)
- `loopTarget` — code offset of the loop header (for backward branches)
- `endPatches` — list of forward `rel32` offsets to patch when `end` is reached
- `elseJmpOff` — the `je` placeholder patched when `else` or `end` is reached

Dead-code mode (`fc.dead = true`) is entered after unconditional branches,
`return`, and `unreachable`. The dead-code scanner (`skipDeadOp`) advances past
all immediates and watches for `block`/`loop`/`if`/`else`/`end` to track
nesting depth and restore liveness at the matching `end` or `else`.

`br_table` is compiled to:

```asm
cmp  eax, <n-1>
ja   default
lea  rcx, [rip + table]
movsxd rax, [rcx + rax*4]
add  rax, rcx
jmp  rax
<table: n rel32 entries>
<default branch>
```

---

## Import resolution

Three import kinds are recognised, determined by `platform.Parse(module)`:

**`SyscallTrampoline`** — inlined directly at the call site with no `call`
instruction. The syscall number is looked up via `linuxplatform.SyscallNumber`.
The fourth argument is moved from RCX to R10 (Linux syscall ABI). Pointer
parameters (`ptr` in the `@` sig) have R15 added unconditionally (no NULL
guard — offset 0 is a valid linear-memory address for syscalls).

**`PlatformLib`** / **`CrossPlatformLib`** — compiled to a RIP-relative `call`
against an undefined symbol (`module::name` or just `name`). Pointer parameters
use `emitSafePointerTranslate` which emits `test reg,reg; jz +3; add reg,r15`,
preserving NULL (offset 0 → native NULL for library calls). RSP is aligned to
16 bytes and EAX zeroed before the call (SysV variadic ABI). R12 is used to
save and restore the unaligned RSP.

**`memory` module** — intercepted before the platform router. The import name
is rewritten to `__vertex_memory_<sub>_<fn>` (dots replaced with underscores)
and emitted as an undefined symbol for the linker to resolve against the stubs
generated by the `memory` package.

---

## Pointer translation

Two helpers are used depending on the call site:

`emitAddR15To(reg)` — unconditional `add reg, r15`. Used for syscall imports
and internal Vertex allocator calls where offset 0 is a valid buffer.

`emitSafePointerTranslate(reg)` — `test reg,reg; jz +3; add reg,r15`. Used for
platform library imports where the caller may legitimately pass a null pointer.

---

## Callback trampolines

`ref.func` (opcode `0xD2`) pushes the absolute 64-bit native address of a
function so it can be stored and called back later — e.g. as a destructor passed
to `ref_set_dtor`. Because external callers receive a plain native function
pointer, they invoke it without R15 set and with native arguments (no Wasm
offset adjustment).

For each function referenced by a `ref.func`, `generateCallbackTrampolines`
emits a small stub that:

1. Loads R15 from `__wasm_data_base + 65536`.
2. Subtracts R15 from any `i32` parameters (native VA → Wasm offset).
3. Jumps directly to the function body.

The trampoline is emitted once per unique function index and exported as
`__cb_<localIdx>`. The `RelocAbs64` entry for the `ref.func` site is then
pointed at the trampoline symbol instead of the raw function.

---

## Relocation types

| Kind | Use |
|------|-----|
| `RelocRel32` | RIP-relative `call` / `lea` to a function or data symbol |
| `RelocAbs64` | Absolute 64-bit address for `ref.func` and callback trampolines |

Relocations to local functions are resolved immediately by `applyRelocs` (the
target offset is known). Relocations to imports and `__wasm_data_base` are
left as `object.Reloc` entries for the linker.

---

## Supported opcodes

### Control (`dispatch.go`)
`unreachable`, `nop`, `block`, `loop`, `if`, `else`, `end`, `br`, `br_if`,
`br_table`, `return`, `call`, `call_indirect` (stub — not yet implemented),
`drop`, `select`, `select t`

### Variables (`dispatch.go`)
`local.get`, `local.set`, `local.tee`, `global.get`, `global.set`

### Memory (`dispatch.go` + `memory.go`)
All loads: `i32.load`, `i64.load`, `f32.load` (as i32 bits), `f64.load` (as
i64 bits), plus the eight signed/unsigned narrowing variants
(`i32.load8_s/u`, `i32.load16_s/u`, `i64.load8_s/u`, `i64.load16_s/u`,
`i64.load32_s/u`).

All stores: `i32.store`, `i64.store`, `f32.store`, `f64.store`,
`i32.store8`, `i32.store16`, `i64.store8`, `i64.store16`, `i64.store32`.

`memory.size` (returns the declared minimum page count as an immediate),
`memory.grow` (stub — always returns -1), `memory.fill` (via `rep stosb`).

### Constants (`dispatch.go`)
`i32.const`, `i64.const`, `f32.const` (raw bits as i32), `f64.const` (raw
bits as i64).

### Reference types (`dispatch.go`)
`ref.null`, `ref.is_null`, `ref.func`

### Integer arithmetic and comparisons (`math.go`)
Full i32 and i64 coverage: `eqz`, `eq`, `ne`, `lt_s/u`, `gt_s/u`,
`le_s/u`, `ge_s/u`; `clz`, `ctz`, `popcnt`; `add`, `sub`, `mul`,
`div_s/u`, `rem_s/u`, `and`, `or`, `xor`, `shl`, `shr_s/u`, `rotl`, `rotr`.

Sign-extension (Wasm 2.0): `i32.extend8_s`, `i32.extend16_s`,
`i64.extend8_s`, `i64.extend16_s`, `i64.extend32_s`.

Conversions: `i32.wrap_i64`, `i64.extend_i32_s`, `i64.extend_i32_u`.

### `0xFC` prefix (`dispatch.go`)
`data.drop` and `elem.drop` (no-ops — data is inlined at startup),
`memory.fill`. All others return an "not implemented" error.

### Not yet implemented
Floating-point arithmetic, comparisons, and conversions (`0x5B`–`0xA6`,
`0xA8`–`0xBF`), `call_indirect`, `memory.init`, `memory.copy`, all table
operations, and saturating truncations (`0xFC 0`–`7`).

---

## `driver.Target` interface

`Target.Emit` is called by the driver with a list of function indices assigned
to this backend. On the first call it delegates to `emitDataSegments` to
initialise `obj.Data` and define `__wasm_data_base`. It then compiles each
function body via `compileFuncBody`, accumulates the resulting machine code and
relocations directly into `ctx.Obj`, and resolves symbols.

> **v1 limitations**
> - `amd64` only.
> - Non-PIE executables only (native code pointers are truncated to 32 bits for
>   `ref.func` and concurrency spawn).
> - `memory.grow` always returns -1.
> - `call_indirect` is not implemented.
> - Floating-point instructions are not implemented.
> - The `target.go` `Emit` path and the legacy `compile.go` `moduleCompiler`
>   path are currently separate; `inlinedImports` is always `nil` in the driver
>   path.