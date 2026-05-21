# Vertex ABI Reference

A complete reference for all import namespaces, export conventions, and callable symbols available to a wasm frontend targeting the Vertex compiler.

---

## Import Signature Syntax

All imports that pass pointers carry a `@`-suffix signature on the function name.

```wasm
;; General syntax structure
(import "<module>" "<name>@<type>.<type>.<type>..." (func ...))

```

| Token | Meaning |
| --- | --- |
| `i32` | 32-bit integer — passed as-is |
| `i64` | 64-bit integer — passed as-is |
| `f32` | 32-bit float — passed as-is |
| `f64` | 64-bit float — passed as-is |
| `ptr` | Linear-memory i32 offset — auto-translated to native VA before call |

Functions with no pointer parameters need no `@` suffix.

```wasm
;; fd=i32, buf=ptr, count=i32
(import "linux:kernel/syscalls" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))

;; no pointer params, no suffix needed
(import "linux:kernel/syscalls" "getpid" (func (result i32)))

```

---

## Export Suffix Syntax

Exports destined for a non-CPU backend carry a `@<kind>` suffix, with an optional `:type.type...` list for parameter annotations across the dispatch boundary.

```wasm
;; Syntax structure
(export "<name>@<kind>" (func $name))
(export "<name>@<kind>:<type>.<type>.<type>..." (func $name))

```

| Kind | Backend |
| --- | --- |
| `@cuda` | PTX — NVIDIA, Linux/Windows |
| `@vulkan` | SPIR-V — AMD + CPU fallback, Linux/Windows |
| `@metal` | MSL — Apple, macOS only |
| `@async` | Stackful coroutines |
| `@thread` | OS threads via `clone(2)` |
| `@process` | Child processes via `fork(2)` |

---

## Import Modules

### `linux:kernel/syscalls` — Inlined Linux syscalls

The entire syscall sequence is inlined. No PLT entry, no relocation, no libc.

```wasm
(import "linux:kernel/syscalls" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))
(import "linux:kernel/syscalls" "read@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))
(import "linux:kernel/syscalls" "open@ptr.i32.i32" (func (param i32 i32 i32) (result i32)))
(import "linux:kernel/syscalls" "close@i32" (func (param i32) (result i32)))
(import "linux:kernel/syscalls" "exit_group@i32" (func (param i32)))

;; forbidden; use memory.* instead
;; (import "linux:kernel/syscalls" "mmap@ptr.i64.i32.i32.i32.i64" ...) 

```

Any syscall from the Linux 6.x amd64/arm64 table is valid. `ptr` params have `R15` added before the syscall instruction.

> `mmap` and `malloc` are compile-time errors — use `memory.*` instead.

---

### `windows:kernel32` — Windows system DLL

Emits an IAT entry. Same `@`-suffix signature convention.

```wasm
(import "windows:kernel32" "WriteFile@ptr.i32.ptr.ptr.ptr" (func (param i32 i32 i32 i32 i32) (result i32)))
(import "windows:kernel32" "ReadFile@ptr.ptr.i32.ptr.ptr" (func (param i32 i32 i32 i32 i32) (result i32)))

```

---

### `darwin:libSystem` — macOS system library

Emits an `LC_LOAD_DYLIB` stub.

```wasm
(import "darwin:libSystem" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))

```

---

### `c` — Cross-platform libc

The `lib` prefix is added automatically on Linux/macOS.

```wasm
(import "c" "printf@ptr.i32" (func (param i32 i32) (result i32)))
(import "c" "strlen@ptr" (func (param i32) (result i32)))

```

`malloc` and `free` are compile-time errors — use `memory.*` instead.

---

### `<libname>` — Any shared library

Use the bare library name as the module field.

```wasm
(import "sdl2" "SDL_Init@i32" (func (param i32) (result i32)))

```

---

### `memory` — Vertex allocator

Direct `malloc`/`mmap` imports are a compile-time error. All allocation goes through this module.

#### `memory.heap.*`

| Import | Signature | Description |
| --- | --- | --- |
| `heap.alloc` | `(size i32) → i32` | Zeroed allocation |
| `heap.alloc_raw` | `(size i32) → i32` | Uninitialized — explicit opt-in |
| `heap.alloc_aligned` | `(size i32, align i32) → i32` | v1: alignment ignored; behaves as `heap.alloc` |
| `heap.free` | `(ptr i32)` | Return block to free list. Large blocks not reclaimed in v1. |
| `heap.realloc` | `(ptr i32, new_size i32) → i32` | `ptr==0` → alloc. `new_size==0` → free, return 0. |

#### `memory.ref.*`

| Import | Signature | Description |
| --- | --- | --- |
| `ref.alloc` | `(size i32) → i32` | Allocate with RC header. strong=1, weak=0, dtor=0. |
| `ref.retain` | `(ptr i32)` | Atomically increment strong count |
| `ref.release` | `(ptr i32)` | Decrement strong count; calls destructor and frees at zero |
| `ref.set_dtor` | `(ptr i32, fn i32)` | Store destructor function pointer into RC header |
| `ref.alloc_weak` | `(size i32) → i32` | v1: identical to `ref.alloc` |
| `ref.weak` | `(ptr i32) → i32` | Atomically increment weak count; returns same pointer |
| `ref.upgrade` | `(ptr i32) → i32` | Increment strong count if > 0; returns pointer or 0 if freed |

#### `memory.arena.*`

| Import | Signature | Description |
| --- | --- | --- |
| `arena.push` | `()` | Save bump pointer checkpoint (max depth: 64) |
| `arena.pop` | `()` | Restore checkpoint, reclaiming all allocations since `push` |
| `arena.alloc` | `(size i32) → i32` | Bump-allocate, 8-byte aligned. OOM exits with code 127. |

---

### `gpu` — GPU intrinsics

Imported from the `"gpu"` module. Used inside functions marked with a `@cuda`, `@vulkan`, or `@metal` export suffix.

#### CUDA (`cuda.` prefix)

All CUDA intrinsics are imported under the `cuda.*` namespace. The available symbols map to PTX instructions and may expand across compiler versions.

```wasm
(import "gpu" "cuda.threadIdx.x" (func (result i32)))

```

#### Metal (`metal.` prefix)

All Metal intrinsics are imported under the `metal.*` namespace. The available symbols map to MSL built-ins and may expand across compiler versions.

```wasm
(import "gpu" "metal.thread_position_in_grid.x" (func (result i32)))

```

#### Vulkan (`vulkan.` prefix)

All Vulkan intrinsics are imported under the `vulkan.*` namespace. The available symbols map to SPIR-V opcodes and may expand across compiler versions.

```wasm
(import "gpu" "vulkan.GlobalInvocationId.x" (func (result i32)))

```

> Consult the Vertex intrinsic reference for the current symbol listing under each prefix. A function body may only import intrinsics matching its declared vendor — mixing vendors across a single function's call tree is a compile error.

---

## Concurrency Exports

Mark an export with `@async`, `@thread`, or `@process` to opt into a concurrency backend. An optional `:type...` list describes parameter passing across the spawn boundary.

```wasm
(export "worker@thread:ptr.i32" (func $worker))
(export "handler@async" (func $handler))
(export "task@process:i32" (func $task))

```

Once spawned, each model exposes a set of callable wasm imports. Import these from the `"concurrency"` module.

### `@async` — Coroutines

| Import | Signature | Description |
| --- | --- | --- |
| `coro.spawn` | `(fn i32) → i32` | Allocate handle + stack; return wasm handle |
| `coro.resume` | `(handle i32)` | Transfer control into coroutine; no-op if done |
| `coro.yield` | `(handle i32, value i32)` | Suspend, store value, return to caller |
| `coro.done` | `(handle i32) → i32` | 1 if finished, 0 if suspended |
| `coro.result` | `(handle i32) → i32` | Last yielded value or final return value |

Handle lifetime is managed by `memory.ref.*`. `ref.release` munmaps the coroutine stack.

### `@thread` — OS Threads

| Import | Signature | Description |
| --- | --- | --- |
| `thread.spawn` | `(fn i32) → i32` | `clone(2)`, return handle |
| `thread.join` | `(handle i32) → i32` | Block until thread exits; returns exit code |
| `thread.detach` | `(handle i32)` | Mark as detached |
| `thread.self` | `() → i32` | `gettid(2)` — calling thread's TID |
| `thread.exit` | `(code i32)` | `SYS_exit` for the calling thread |

Handle lifetime is managed by `memory.ref.*`.

### `@process` — Child Processes

| Import | Signature | Description |
| --- | --- | --- |
| `process.spawn` | `(fn i32) → i32` | `fork(2)`, return handle |
| `process.wait` | `(handle i32) → i32` | `wait4(2)`; returns `WEXITSTATUS`; result cached |
| `process.pid` | `(handle i32) → i32` | Child PID |
| `process.exit` | `(code i32)` | `exit_group(2)` — valid from parent or child |

Handle must be freed by the caller with `memory.heap.free`. No RC destructor.

---

## GPU Kernel Exports

Mark a function export (or name-section entry for non-exported kernels) with `@cuda`, `@vulkan`, or `@metal`. The optional `:type...` list describes the kernel's own parameters.

```wasm
;; no pointer params
(export "warpReduce@cuda" (func $warpReduce))

;; two buffer pointers + element count
(export "vectorAdd@cuda:ptr.ptr.i32" (func $vectorAdd))

(export "tileConv@metal:ptr.ptr.i32" (func $tileConv))

(export "histogram@vulkan:ptr.i32" (func $histogram))

```

For non-exported kernels, attach the hint via the name custom section instead of the export table. The syntax is identical.

| Vendor | Platform | Output |
| --- | --- | --- |
| `cuda` | Linux, Windows | PTX text |
| `vulkan` | Linux, Windows | SPIR-V binary |
| `metal` | macOS only | MSL text |