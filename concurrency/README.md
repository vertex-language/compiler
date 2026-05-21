# concurrency

The `concurrency` package emits the native x86-64 stubs that back the
`@async`, `@thread`, and `@process` export suffixes. When the compiler driver
detects any of those suffixes via `driver.Analyze`, it calls `concurrency.Emit`,
which generates only the backends actually needed and appends everything directly
into the shared `BuildContext`'s `WasmObj`.

---

## Overview

Three independent concurrency models are supported. Each is self-contained —
a module that only uses `@thread` pays zero cost for coroutine or process code.

| Suffix | Model | Kernel primitive | Handle allocation |
|--------|-------|-----------------|-------------------|
| `@async` | Stackful cooperative coroutines | `mmap` + symmetric context switch | `memory.ref.alloc` (RC) |
| `@thread` | OS threads | `clone(2)` | `memory.ref.alloc` (RC) |
| `@process` | Child processes | `fork(2)` | `memory.heap.alloc` (plain) |

The Wasm frontend marks intent with an export suffix; Vertex owns the entire
implementation — stack allocation, context switching, `clone`/`fork`/`wait`,
and R15 propagation are invisible to the language.

---

## Async — stackful coroutines

Coroutines are cooperative and single-threaded. All handle fields use plain
stores (no lock prefix). Do not share a `CoroHandle` across threads.

### Stack geometry

| Constant | Value | Purpose |
|----------|-------|---------|
| `CoroStackSize` | 1 MiB | Usable stack per coroutine |
| `CoroGuardSize` | 4 096 B | `PROT_NONE` guard page below each stack |

### CoroHandle layout (64-byte user area)

| Field | Offset | Type | Description |
|-------|--------|------|-------------|
| `CoroStatus` | 0 | `int32` | `0` = suspended/ready, `2` = done |
| `CoroCoroRSP` | 8 | `int64` | Saved RSP for the coroutine side of the context switch |
| `CoroCallerRSP` | 16 | `int64` | Saved RSP for the caller (`resume`) side |
| `CoroResult` | 24 | `int64` | Value from the most recent `coro.yield`, or the body's return value when done |
| `CoroStackBase` | 32 | `int64` | Native base address of the `mmap`'d region |
| `CoroStackLen` | 40 | `int64` | `mmap` length (`CoroGuardSize + CoroStackSize`) |
| `CoroFuncNative` | 48 | `int64` | Zero-extended 32-bit native code pointer to the body |

The handle is ref-counted (`memory.ref.alloc`). The RC destructor
(`__vertex_coro_dtor`) is stored inline at spawn time by writing directly to
`rcDtorOffset (-16)` in the RC header, bypassing `ref_set_dtor`'s i32-only
interface.

### Context switch — `__vertex_coro_jump`

All switches go through the symmetric leaf `__vertex_coro_jump(save_rsp
*int64, load_rsp *int64)`. It pushes the five callee-saved registers (RBX,
R12–R14, RBP), saves RSP into `*save_rsp`, loads a new RSP from `*load_rsp`,
pops the restored registers, and returns — landing in the new context's call
frame.

Stack layout invariant for a saved context:

```
[saved_rsp +  0]  RBP             (0 on first entry)
[saved_rsp +  8]  R14             (0 on first entry)
[saved_rsp + 16]  R13             (0 on first entry)
[saved_rsp + 24]  R12             (0 on first entry)
[saved_rsp + 32]  RBX             (native_handle_ptr on first entry)
[saved_rsp + 40]  return address  (__vertex_coro_trampoline on first entry)
```

### Spawn stack priming

`__vertex_coro_spawn` writes the invariant above directly into the freshly
`mmap`'d stack so that the very first `coro.resume` lands in
`__vertex_coro_trampoline` with `RBX = native_handle_ptr`.

The trampoline calls the body, stores its return value in `CoroResult`, marks
`CoroStatus = 2`, then switches back to the caller via `coro_jump`. The
coroutine is never resumed after that (`UD2` guard).

### Exported symbols

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_coro_spawn` | `(func_native_ptr i32) → i32` | Allocates handle, maps stack, primes context, stores destructor, returns wasm handle |
| `__vertex_coro_resume` | `(handle i32) → void` | Transfers control into the coroutine; no-op if already done |
| `__vertex_coro_yield` | `(handle i32, value i32) → void` | Suspends the coroutine, stores `value` in `CoroResult`, resumes the caller |
| `__vertex_coro_done` | `(handle i32) → i32` | Returns `1` if `CoroStatus == 2`, else `0`. Leaf |
| `__vertex_coro_result` | `(handle i32) → i32` | Reads low 32 bits of `CoroResult`. Leaf |

Internal symbols (not imported by Wasm):

| Symbol | Description |
|--------|-------------|
| `__vertex_coro_jump` | Symmetric context-switch leaf |
| `__vertex_coro_trampoline` | Entry point for every new coroutine; calls body, marks done, switches back |
| `__vertex_coro_dtor` | RC destructor; `munmap`s the coroutine stack. Leaf |

---

## Thread — OS threads via `clone(2)`

### Stack geometry

| Constant | Value | Purpose |
|----------|-------|---------|
| `ThreadStackSize` | 2 MiB | Usable stack per thread |
| `ThreadGuardSize` | 4 096 B | `PROT_NONE` guard page below each stack |

### Clone flags

```
CLONE_VM             0x00000100  share address space
CLONE_FS             0x00000200  share filesystem root / cwd / umask
CLONE_FILES          0x00000400  share file-descriptor table
CLONE_SIGHAND        0x00000800  share signal handlers
CLONE_CHILD_SETTID   0x01000000  kernel writes child TID into ctid at start
CLONE_CHILD_CLEARTID 0x00200000  kernel zeros ctid and wakes futex on exit
```

`CLONE_CHILD_CLEARTID` is the key to `thread.join`: the kernel automatically
clears `ThreadTID` and issues a futex wake when the child calls `SYS_exit`,
unblocking any waiter without any explicit signal from the child.

### ThreadHandle layout (64-byte user area)

| Field | Offset | Type | Description |
|-------|--------|------|-------------|
| `ThreadTID` | 0 | `int32` | Kernel-managed TID / futex word. **Must be at offset 0** — doubles as the `ctid` argument to `clone` |
| `ThreadExitCode` | 8 | `int64` | Written by the child thread just before `SYS_exit` |
| `ThreadStackBase` | 16 | `int64` | Native `mmap` base address |
| `ThreadStackLen` | 24 | `int64` | `mmap` length (`ThreadGuardSize + ThreadStackSize`) |
| `ThreadFuncNative` | 32 | `int64` | Zero-extended 32-bit native code pointer |
| `ThreadDetached` | 40 | `int32` | Set to `1` by `thread.detach` |

The handle is ref-counted. The RC destructor (`__vertex_thread_dtor`) is
stored inline at spawn time. The destructor only `munmap`s the stack if
`ThreadTID == 0` (thread has exited); stacks of still-running detached threads
are leaked in v1.

### `thread.join` protocol

The child stores its exit code in `ThreadExitCode` before calling `SYS_exit`.
x86-64 TSO guarantees this store is globally visible before the kernel
processes the syscall, so the join side always reads the correct value. The
join loop uses `futex(FUTEX_WAIT)` on `ThreadTID`, retrying on `EINTR` and
`EAGAIN`.

### Exported symbols

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_thread_spawn` | `(func_native_ptr i32) → i32` | Allocates handle, maps + guards stack, `clone`s, stores dtor, returns wasm handle |
| `__vertex_thread_join` | `(handle i32) → i32` | Blocks via `futex(FUTEX_WAIT)` on `ThreadTID`; returns `ThreadExitCode` |
| `__vertex_thread_detach` | `(handle i32) → void` | Sets `ThreadDetached = 1`. Leaf |
| `__vertex_thread_self` | `() → i32` | Returns the calling thread's TID via `gettid(2)`. Leaf |
| `__vertex_thread_exit` | `(code i32) → void` | Terminates the calling thread via `SYS_exit`. Join returns `0` (body return bypassed). Leaf |

Internal symbols:

| Symbol | Description |
|--------|-------------|
| `__vertex_thread_dtor` | RC destructor; `munmap`s the stack if `ThreadTID == 0`. Leaf |

---

## Process — child processes via `fork(2)`

### ProcessHandle layout (32-byte user area)

| Field | Offset | Type | Description |
|-------|--------|------|-------------|
| `ProcPID` | 0 | `int64` | Child PID returned by `fork` |
| `ProcRawStatus` | 8 | `int64` | Raw status word filled by `wait4` |
| `ProcWaited` | 16 | `int32` | Set to `1` after the first successful `process.wait` |
| `ProcFuncNative` | 24 | `int64` | Native code pointer; copied before `fork` so the child can read it from its COW mapping |

The handle is a **plain** `memory.heap.alloc` (not ref-counted). The caller
owns it and must call `process.wait` exactly once.

R15 (the Wasm linear-memory base) is valid in both parent and child after
`fork` because the child gets a COW copy of the same address space. The child
reads `ProcFuncNative` from its own copy of the handle, calls the function,
and exits with the return value via `exit_group(2)`.

### Cached wait result

`__vertex_process_wait` stores the raw `wait4` status in `ProcRawStatus` and
sets `ProcWaited = 1` on the first call. Subsequent calls return
`WEXITSTATUS(ProcRawStatus)` directly without issuing another syscall. In v1
the `& 0xFF` mask in `WEXITSTATUS` is omitted (exit codes are 0–255 in
practice).

### Exported symbols

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_process_spawn` | `(func_native_ptr i32) → i32` | Allocates handle, stores func ptr, `fork`s; parent stores PID and returns handle; child calls function and `exit_group`s |
| `__vertex_process_wait` | `(handle i32) → i32` | Blocks via `wait4`; returns `WEXITSTATUS`. Caches result — safe to call multiple times |
| `__vertex_process_pid` | `(handle i32) → i32` | Reads `ProcPID`. Leaf |
| `__vertex_process_exit` | `(code i32) → void` | Terminates the entire process via `exit_group(2)`. Valid from any context. Leaf |

---

## Wasm import module names

| Model | Import module |
|-------|--------------|
| Async / coroutines | `coro` |
| Threads | `thread` |
| Processes | `process` |

---

## Integration with the compiler driver

`concurrency.Emit` is called automatically by the driver when any of
`ctx.NeedsAsync`, `ctx.NeedsThread`, or `ctx.NeedsProcess` is set (populated
by `driver.Analyze` when it encounters an `@async`, `@thread`, or `@process`
export suffix). Only the flagged backends are emitted — unused stubs produce
no code. All output is appended directly into the shared `BuildContext.Obj`
via the internal `emitter`.

> **v1 limitations**
> - All three backends are only ported to `amd64`.
> - `coro_spawn` and `thread_spawn` require a non-PIE executable (native code
>   pointers are truncated to 32 bits).
> - Detached thread stacks are leaked if the thread is still running when its
>   handle is released.
> - `process.wait`'s `WEXITSTATUS` omits the `& 0xFF` mask.
> - Coroutines are not thread-safe; do not share a `CoroHandle` across threads.