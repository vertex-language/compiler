# Concurrency Package

Backend machinery for concurrent execution in the Vertex compiler framework.
Detects functions marked with a `@kind` suffix and emits all platform-level
code for them — the frontend marks intent, Vertex owns implementation.

```
Your Language → WebAssembly IR (functions marked @async / @thread / @process)
                    → Coroutine stubs / clone3 wrappers / fork wrappers
                    → ELF Binary
```

---

## Philosophy

The same principle that drives the GPU backend applies here. A frontend
targeting `@cuda` never thinks about PTX register allocation or cubin caching.
A frontend targeting `@async`, `@thread`, or `@process` never thinks about:

- Stack allocation or layout
- Context switch register sequences
- R15 (linear memory base) across a suspend, thread start, or fork
- `clone3` calling conventions or `clone_args` layout
- `futex` semantics for thread join
- `wait4` for process exit codes

The frontend emits a flat wasm function, marks it with a suffix, and Vertex
handles everything from there. The wasm binary remains 100% spec-compliant —
`wasm-validate` passes, `wasm2wat` renders the suffix intact, and any runtime
that does not know about Vertex simply sees an export with an unusual name.

---

## The `@kind` Naming Convention

### Syntax

```
"<name>@<kind>"
"<name>@<kind>:<type>.<type>.<type>..."
```

The kind token routes the function to a backend. The optional type list after
`:` annotates the function's own parameters — same token set as import
signatures.

| Token | Meaning                                                           |
|-------|-------------------------------------------------------------------|
| `i32` | 32-bit integer — passed as-is                                     |
| `i64` | 64-bit integer — passed as-is                                     |
| `f32` | 32-bit float — passed as-is                                       |
| `f64` | 64-bit float — passed as-is                                       |
| `ptr` | Linear-memory i32 offset — auto-translated to native VA           |

| Kind       | Model                        | Platform        |
|------------|------------------------------|-----------------|
| `async`    | Stackful coroutines          | Linux, Windows  |
| `thread`   | Native threads via `clone3`  | Linux           |
| `process`  | Child processes via `fork`   | Linux           |

### Examples

```go
// Coroutine — no pointer params
m.Exports.Add("counter@async",           wasm.ExportFunc, counterIdx)

// Coroutine — first param is a linear-memory buffer
m.Exports.Add("producer@async:ptr.i32",  wasm.ExportFunc, producerIdx)

// Thread entry — receives a data pointer
m.Exports.Add("worker@thread:ptr.i32",   wasm.ExportFunc, workerIdx)

// Process entry — receives an argument
m.Exports.Add("child@process:i32",       wasm.ExportFunc, childIdx)
```

---

## Counterpart Primitives

The `@kind` suffix declares what a function *is*. The module imports below
declare what the *caller does with* them. They are ordinary wasm imports —
the frontend calls them like any other function; Vertex routes them to the
correct inline sequence or syscall.

### `coro` module — used with `@async`

**Inside an `@async` function:**

| Import name       | Behaviour                                      |
|-------------------|------------------------------------------------|
| `coro.yield.i32`  | Suspend and produce a value to the caller      |
| `coro.suspend`    | Suspend without a value                        |

**From the caller:**

| Import name       | Behaviour                                      |
|-------------------|------------------------------------------------|
| `coro.spawn`      | Create a handle from an `@async` function index (i32 → i32 handle) |
| `coro.resume`     | Resume a suspended handle                      |
| `coro.done`       | Poll — returns 1 if the coroutine has finished |
| `coro.result.i32` | Read the return value after done               |

---

### `thread` module — used with `@thread`

**Inside a `@thread` function:**

| Import name     | Behaviour                            |
|-----------------|--------------------------------------|
| `thread.self`   | Returns own thread id (i32)          |
| `thread.exit`   | Terminate this thread with exit code |

**From the caller:**

| Import name     | Behaviour                                               |
|-----------------|---------------------------------------------------------|
| `thread.spawn`  | Launch a `@thread` function, returns handle             |
| `thread.join`   | Block until thread exits, returns exit code             |
| `thread.detach` | Fire and forget — no join possible after this           |

Synchronisation between threads uses the wasm threads proposal atomics
(`atomic.wait32`, `atomic.notify`, `atomic.*`). Vertex lowers these to
`lock`-prefixed instructions and `futex` syscalls. No new primitives needed.

---

### `process` module — used with `@process`

**Inside a `@process` function:**

| Import name        | Behaviour                               |
|--------------------|-----------------------------------------|
| `process.exit.i32` | Exit child with code (also valid from main) |

**From the caller:**

| Import name       | Behaviour                                          |
|-------------------|----------------------------------------------------|
| `process.spawn`   | Fork — runs `@process` function in child, returns pid |
| `process.wait`    | Blocks until child exits, returns exit code        |
| `process.pid`     | Returns pid of a running child handle              |

---

## WAT Examples

### `@async` — stackful coroutine

```wat
(module
  (import "coro" "yield.i32"  (func $yield  (param i32)))
  (import "coro" "spawn"      (func $spawn  (param i32) (result i32)))
  (import "coro" "resume"     (func $resume (param i32)))
  (import "coro" "done"       (func $done   (param i32) (result i32)))
  (import "coro" "result.i32" (func $result (param i32) (result i32)))

  ;; coroutine body — yields 1, 2, 3 then returns
  (func $counter
    (call $yield (i32.const 1))
    (call $yield (i32.const 2))
    (call $yield (i32.const 3))
  )
  (export "counter@async" (func $counter))

  (func $main (result i32)
    (local $h i32)
    (local.set $h (call $spawn (i32.const 0)))  ;; 0 = func index of $counter

    (call $resume (local.get $h))               ;; drives to first yield
    (call $resume (local.get $h))               ;; drives to second yield
    (call $resume (local.get $h))               ;; drives to third yield

    (i32.const 0)
  )
  (export "main" (func $main))
)
```

The frontend writes a completely flat function. No stack juggling, no
continuation layout, no scheduler. `coro.yield.i32` is just a call.
Vertex transforms the body — the frontend never sees it.

---

### `@thread` — native thread

```wat
(module
  (import "thread" "spawn" (func $spawn (param i32) (result i32)))
  (import "thread" "join"  (func $join  (param i32) (result i32)))

  (memory (shared 1 1))

  ;; thread entry — writes result into shared memory and returns
  (func $worker (param $data i32) (result i32)
    (i32.atomic.store
      (i32.const 256)
      (i32.add (local.get $data) (i32.const 1)))
    (i32.const 0)
  )
  (export "worker@thread:i32" (func $worker))

  (func $main (result i32)
    (local $h i32)
    (local.set $h (call $spawn (i32.const 1)))  ;; 1 = func index of $worker
    (drop (call $join (local.get $h)))
    (i32.atomic.load (i32.const 256))
  )
  (export "main" (func $main))
)
```

`atomic.*` instructions are already in the wasm threads proposal. Vertex
lowers them to `lock`-prefixed instructions and `futex` syscalls. The
frontend just uses them.

---

### `@process` — child process

```wat
(module
  (import "process" "spawn" (func $spawn (param i32) (result i32)))
  (import "process" "wait"  (func $wait  (param i32) (result i32)))

  ;; child entry — does work and exits
  (func $child (param $arg i32) (result i32)
    (i32.const 0)
  )
  (export "child@process:i32" (func $child))

  (func $main (result i32)
    (local $pid  i32)
    (local $code i32)
    (local.set $pid  (call $spawn (i32.const 1)))
    (local.set $code (call $wait  (local.get $pid)))
    (local.get $code)
  )
  (export "main" (func $main))
)
```

`clone`/`fork`, R15 reinitialisation in the child, and `waitpid` wrapping
are entirely Vertex's problem. The frontend sees `process.spawn` returns a
pid and `process.wait` blocks on it.

---

## Marking Non-Exported Functions

Not every concurrent function needs to be a public export. For internal
functions use the wasm name custom section with the same `@kind` suffix:

```go
import "github.com/vertex-language/compiler/concurrency"

m.Customs = append(m.Customs, concurrency.NameHints(map[uint32]string{
    internalWorkerIdx: "worker@thread:ptr.i32",
    internalGenIdx:    "generator@async",
}))
```

`NameHints` writes a standard wasm `"name"` custom section. The names are
valid name-section entries and will render correctly in `wasm2wat` output.

---

## What the Frontend Never Touches

| Concern                        | `@async`  | `@thread` | `@process` |
|-------------------------------|-----------|-----------|------------|
| Stack allocation (mmap)        | Vertex    | Vertex    | n/a        |
| Context layout (64-byte block) | Vertex    | n/a       | n/a        |
| R15 save/restore across switch | Vertex    | Vertex    | Vertex     |
| `ucontext` / suspend/resume    | Vertex    | n/a       | n/a        |
| `clone3` / `clone_args` layout | n/a       | Vertex    | n/a        |
| `fork` syscall                 | n/a       | n/a       | Vertex     |
| `futex` join                   | n/a       | Vertex    | n/a        |
| `wait4` / exit code decode     | n/a       | n/a       | Vertex     |

---

## Detection

The compiler detects concurrent functions in a single pass over export
entries and the name custom section:

1. **Export name** — check for `@kind` suffix; highest priority.
2. **Name custom section** — check function name entries for `@kind` suffix.

A function with no `@kind` suffix in either place is compiled as a normal
CPU function by the x86-64 emitter.

```go
// concurrency/detect.go — simplified
func Detect(m *wasm.Module) ([]FuncInfo, error) {
    var funcs []FuncInfo

    // pass 1: export names
    for _, e := range m.Exports.Entries {
        if e.Kind != wasm.ExportFunc { continue }
        if k, params, ok := parseKindSuffix(e.Name); ok {
            funcs = append(funcs, FuncInfo{
                FuncIdx: e.Idx,
                Name:    stripSuffix(e.Name),
                Kind:    k,
                Params:  params,
                Source:  SourceExport,
            })
        }
    }

    // pass 2: name custom section (catches non-exported functions)
    for _, entry := range parseFuncNamesFromModule(m) {
        if alreadyDetected(funcs, entry.idx) { continue }
        if k, params, ok := parseKindSuffix(entry.name); ok {
            funcs = append(funcs, FuncInfo{
                FuncIdx: entry.idx,
                Name:    stripSuffix(entry.name),
                Kind:    k,
                Params:  params,
                Source:  SourceNameSection,
            })
        }
    }

    return validate(m, funcs)
}
```

---

## Integration with `compiler.go`

```go
// Detect concurrent functions before CPU compilation.
concFuncs, err := concurrency.Detect(m)
if err != nil {
    return nil, fmt.Errorf("compiler: concurrency detection: %w", err)
}
concSet := concurrency.FuncSet(concFuncs)

var concResult *concurrency.CompileResult
if len(concFuncs) > 0 {
    concResult, err = concurrency.Compile(m, concFuncs, concurrency.CompileOptions{})
    if err != nil {
        return nil, fmt.Errorf("compiler: concurrency compilation: %w", err)
    }
}

// CPU compiler skips both GPU and concurrency-marked functions.
cpuObj, err := x86_64.Compile(m, arch, opts.QualifiedSymbols, gpuSet, concSet)
```

---

## Package Layout

```
concurrency/
├── concurrency.go   ← package doc
├── detect.go        ← scan export names + name section for @kind suffix;
│                       validate no mixing, all indices in range
├── emit.go          ← dispatch to async/thread/process backends;
│                       merge artifacts into object.WasmObj
├── mark.go          ← NameHints(map[uint32]string) → "name" custom section
├── namesection.go   ← wasm name custom section parser
│
├── async/
│   ├── async.go     ← types, constants, Context layout (64 bytes)
│   ├── compiler.go  ← Compile: emit stack-alloc + suspend + resume stubs
│   ├── stack.go     ← mmap-based coroutine stack allocation (SYS_mmap = 9)
│   └── switch.go    ← context switch emission; save/restore R15 + callee-saved
│
├── thread/
│   ├── thread.go    ← types, constants, clone_args layout (88 bytes),
│   │                   clone flags (CLONE_VM | CLONE_THREAD | ...)
│   ├── compiler.go  ← Compile: emit entry wrapper + spawn + join stubs
│   ├── join.go      ← futex FUTEX_WAIT loop (SYS_futex = 202)
│   └── tls.go       ← R15 per-thread reinit strategy (documents convention)
│
└── process/
    ├── process.go   ← types, constants, syscall numbers (fork/clone/wait4)
    ├── compiler.go  ← Compile: emit child entry wrapper + spawn + wait stubs
    └── wait.go      ← wait4 emission + exit-status decode (SYS_wait4 = 61)
```

---

## Compiler Errors

```
error: mixed @kind on function index 4
  @async suffix declared on export "counter@async"
  @thread suffix declared in name section entry "counter@thread"
  only one @kind annotation per function is permitted

error: @kind suffix on import not allowed
  "@async" suffix found on import entry "generator@async"
  @kind suffixes are only valid on locally-defined function names

error: @kind suffix references function index 9
  module has 6 locally-defined functions

error: ptr annotation required for buffer parameter in "producer"
  export "producer@async" has i32 at position 0
  if this parameter is a linear-memory pointer use "producer@async:ptr"
  to enable automatic linear-memory translation
```

---

## Architecture Support

| Architecture | `@async` | `@thread` | `@process` |
|--------------|----------|-----------|------------|
| x86-64       | ✅       | ✅        | ✅         |
| ARM64        | 🔄       | 🔄        | 🔄         |

ARM64 note: `fork` is not available on arm64. `@process` uses `clone` with
`SIGCHLD` on that architecture — the generated code is equivalent from the
frontend's perspective.

---

## Roadmap

- **ARM64 emission** — all three backends need arm64 instruction sequences
- **`coro.await`** — suspend current coroutine until a child handle resolves
- **`thread.detach`** — fire and forget without join
- **Windows** — `@thread` via `CreateThread`; `@process` via `CreateProcess`
- **Scheduler** — optional cooperative scheduler for `@async` so multiple
  coroutines multiplex on one thread without OS involvement