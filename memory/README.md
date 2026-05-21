# memory — Vertex Allocator Package

The `memory` package generates all allocator stubs as native x86-64 machine
code and emits them into a `WasmObj` that is merged with the CPU object before
linking. Every allocation in a Vertex-compiled program goes through these
stubs — direct `malloc` or `mmap` imports are a compile-time error.

---

## Import Module

All primitives are imported from the `"memory"` module:

```wasm
(import "memory" "heap.alloc"          (func (param i32) (result i32)))
(import "memory" "heap.alloc_raw"      (func (param i32) (result i32)))
(import "memory" "heap.alloc_aligned"  (func (param i32 i32) (result i32)))
(import "memory" "heap.free"           (func (param i32)))
(import "memory" "heap.realloc"        (func (param i32 i32) (result i32)))

(import "memory" "ref.alloc"           (func (param i32) (result i32)))
(import "memory" "ref.retain"          (func (param i32)))
(import "memory" "ref.release"         (func (param i32)))
(import "memory" "ref.set_dtor"        (func (param i32 i32)))
(import "memory" "ref.alloc_weak"      (func (param i32) (result i32)))
(import "memory" "ref.weak"            (func (param i32) (result i32)))
(import "memory" "ref.upgrade"         (func (param i32) (result i32)))

(import "memory" "arena.push"          (func))
(import "memory" "arena.pop"           (func))
(import "memory" "arena.alloc"         (func (param i32) (result i32)))
```

All parameters and return values are wasm `i32` linear-memory offsets.
Pointer translation (`+ R15`) happens inside each stub — the frontend never
sees native addresses.

---

## Primitive Reference

### `memory.heap.*` — General-purpose allocator

| Import | Signature | Description |
|---|---|---|
| `heap.alloc` | `(size i32) → i32` | Zeroed allocation. Safe default. |
| `heap.alloc_raw` | `(size i32) → i32` | Uninitialized. Explicit opt-in to the unsafe fast path. |
| `heap.alloc_aligned` | `(size i32, align i32) → i32` | **v1: alignment parameter is ignored.** Tail-calls `heap.alloc`. Blocks are at least 8-byte aligned by the size-class scheme. |
| `heap.free` | `(ptr i32)` | Return block to the free list. Large blocks (class 10) are not reclaimed in v1. |
| `heap.realloc` | `(ptr i32, new_size i32) → i32` | `ptr==0` → `heap.alloc`. `new_size==0` → `heap.free`, return 0. Otherwise allocates raw, copies `min(old, new)` bytes, frees the old block. |

`heap.alloc` always zeros the user region with `REP STOSB`. A frontend that
wants uninitialized memory must call `heap.alloc_raw` deliberately.

### `memory.ref.*` — Reference counting

| Import | Signature | Description |
|---|---|---|
| `ref.alloc` | `(size i32) → i32` | Allocate with RC header. strong=1, weak=0, dtor=0. |
| `ref.retain` | `(ptr i32)` | Atomically increment strong count. Leaf — no frame. |
| `ref.release` | `(ptr i32)` | Atomically decrement strong count. At zero: calls destructor if set, then frees if weak count is also zero. |
| `ref.set_dtor` | `(ptr i32, fn i32)` | Store a destructor function pointer into the RC header. Called by `ref.release` when strong count hits zero. Leaf — no frame. |
| `ref.alloc_weak` | `(size i32) → i32` | **v1: identical to `ref.alloc`.** Tail-calls it directly. |
| `ref.weak` | `(ptr i32) → i32` | Atomically increment weak count. Returns the same wasm pointer. Leaf — no frame. |
| `ref.upgrade` | `(ptr i32) → i32` | Attempt to increment strong count via CAS loop, but only if it is currently > 0. Returns the wasm pointer on success, 0 if the object has already been freed. |

Weak references exist because RC cycles are unavoidable once a frontend builds
trees, linked lists, or any parent–child structure. `ref.upgrade` returning 0
on a freed object makes the result an unambiguous null check at the call site.

### `memory.arena.*` — Bump/stack allocator

| Import | Signature | Description |
|---|---|---|
| `arena.push` | `()` | Save current arena bump pointer onto the checkpoint stack (max depth: 64). Exits with code 127 if the stack is full. |
| `arena.pop` | `()` | Restore saved pointer, reclaiming everything allocated since the matching `push`. No-op if the stack is empty. |
| `arena.alloc` | `(size i32) → i32` | Bump-allocate from the arena, rounded up to 8-byte alignment. Exits with code 127 on OOM. No individual free. |

The arena is intended for short-lived scratch allocations scoped to a known
lifetime — a function call, a coroutine frame, a request. `push`/`pop` pairs
replace explicit frees entirely for these cases.

---

## Linear Memory Layout

All regions are placed at deterministic offsets from R15 (the wasm linear
memory base register). R15 is permanently reserved.

```
R15 + 0x0000_0000   static data / wasm data segments
R15 + 0x0400_0000   heap region begins  (HeapOffset  = 64 MB)
                    256 MB, demand-paged anonymous memory
R15 + 0x1400_0000   arena region begins (ArenaOffset = 320 MB)
                    64 MB
```

The heap uses a segregated free-list allocator with 11 size classes (8 B
through 4 KB) and a bump allocator for large allocations. The arena is a pure
bump allocator with a checkpoint stack up to 64 levels deep.

### Allocation block headers

**Heap block** (8 bytes, prepended to every `heap.*` allocation):

```
[0..3]  size_class  uint32
[4..7]  user_size   uint32
```

**RC block** (32 bytes, prepended to every `ref.*` allocation):

```
[0..7]   strong_count  int64  (atomic)
[8..15]  weak_count    int64  (atomic)
[16..23] dtor_fn_ptr   int64  (native function pointer, 0 = none)
[24..27] size_class    uint32
[28..31] user_size     uint32
```

The pointer returned to the frontend always points past the header. The
header is not visible to frontend code.

---

## Inner Flow

### Initialization (`__vertex_memory_init`)

Called lazily on the first allocation. Every stub that touches the heap or
arena opens with an `initCheck`:

```
lea r13, [rip + __vertex_alloc_state]
mov rax, [r13 + StateHeapBase]
test rax, rax
jnz .already_init
call __vertex_memory_init
lea r13, [rip + __vertex_alloc_state]
.already_init:
```

Init uses `mmap(MAP_FIXED)` to place both regions at their deterministic
offsets from R15. On failure it calls `exit_group(127)`.

### Heap allocation (`heap.alloc`)

```
1. initCheck
2. size_class, slot_size = emitComputeClass(user_size + HeapBlockHeaderSize)
   — BSR-based: class = clamp(BSR(total−1) − 2, 0, 10)
3. if class < 10 (small/medium):
     CAS-loop pop from free_list[class]  (Treiber stack)
     if list empty → bump-allocate slot_size bytes
4. if class == 10 (large):
     bump-allocate total bytes directly
5. write block header (size_class, user_size)
6. if zeroing: REP STOSB the user region
7. return wasm offset = native_ptr + HeapBlockHeaderSize − R15
```

The bump allocator is non-atomic in v1 (single-threaded). The free-list CAS
loop is safe for concurrent use.

### RC release (`ref.release`)

```
1. lock dec [native_user − RCHeaderSize + 0]   (strong_count)
2. jnz .done                                    (still referenced)
3. if dtor_fn_ptr != 0:
     call dtor(wasm_ptr)
4. if weak_count == 0:
     heap.free(native_block)   (block = native_user − RCHeaderSize)
   else:
     leave block alive — weak handles may still call ref.upgrade
.done:
```

### Arena alloc / push / pop

```
push:  if arena_sp >= 64: exit_group(127)
       arena_stack[arena_sp++] = arena_cur

alloc: size = align8(size)
       ptr = arena_cur
       arena_cur += size
       if arena_cur > arena_end: exit_group(127)
       return ptr − R15

pop:   if arena_sp == 0: return   (underflow guard)
       arena_cur = arena_stack[--arena_sp]
```

No individual frees. All memory since the last `push` is reclaimed at once
by `pop`.

---

## Allocator State Block (`__vertex_alloc_state`)

One 4 KB zeroed page in `.data`, RIP-addressed by every stub.

```
offset   field            type
  0      heap_base        int64      native address of heap region
  8      heap_cur         int64      bump pointer
 16      heap_end         int64      end of heap region
 24      arena_base       int64      native address of arena region
 32      arena_cur        int64      arena bump pointer
 40      arena_end        int64      end of arena region
 48      arena_sp         int64      checkpoint stack depth (0..64)
 56      arena_stack      [64]int64  checkpoint stack
568      free_list[0..10] [11]int64  Treiber stack heads per size class
```

---

## Size Classes

| Class | Max allocation |
|---|---|
| 0 | 8 B |
| 1 | 16 B |
| 2 | 32 B |
| 3 | 64 B |
| 4 | 128 B |
| 5 | 256 B |
| 6 | 512 B |
| 7 | 1024 B |
| 8 | 2048 B |
| 9 | 4096 B |
| 10 | > 4096 B (large — bump allocated; freed large blocks are not reclaimed in v1) |

---

## Known v1 Limitations

- **`heap.alloc_aligned`** ignores its alignment argument. All blocks are
  naturally aligned to their size class (minimum 8 bytes). True alignment
  support is deferred.
- **`ref.alloc_weak`** is identical to `ref.alloc`. The two allocation paths
  are not yet distinguished.
- **The heap bump pointer** (`heap_cur`) is written non-atomically. The
  allocator is single-threaded in v1. The free-list CAS loop is safe for
  concurrent use, but the bump fallback is not.
- **Large blocks** (class 10) freed via `heap.free` are silently dropped —
  the backing memory is not returned to the OS.

---

## Compile-time Enforcement

Frontends cannot bypass the allocator. The compiler hard-errors on any of
the following:

```
error: direct allocator import not permitted
  function "make_point" imports "c" "malloc"
  use memory.heap.alloc

error: direct mmap syscall not permitted
  function "worker" imports "linux:kernel/syscalls" "mmap@..."
  use memory.arena or memory.heap

error: memory.ref.release called on non-ref-counted pointer
  pointer was allocated with memory.heap.alloc, not memory.ref.alloc

error: memory.ref.retain across fork boundary
  ref-counted pointers inherited from parent have undefined refcount in child
  resolve fork heap strategy before using memory.ref across process.spawn
```

---

## Package Layout

```
memory/
├── memory.go    package doc, constants, layout, SizeClass helpers
├── emit.go      Compile() — assembles all stubs into a WasmObj
├── emitter.go   x86-64 machine code emitter, symbol/relocation accounting
├── detect.go    validate wasm imports against the memory import signature table
├── init.go      __vertex_memory_init  (mmap both regions)
├── heap.go      heap.alloc, heap.alloc_raw, heap.alloc_aligned,
│                heap.free, heap.realloc
├── ref.go       ref.alloc, ref.retain, ref.release, ref.set_dtor,
│                ref.alloc_weak, ref.weak, ref.upgrade
└── arena.go     arena.push, arena.pop, arena.alloc
```