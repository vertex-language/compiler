# memory

The `memory` package emits the native x86-64 allocator stubs that back the
WebAssembly `memory` import module at runtime. When the compiler driver detects
a `memory` import in a Wasm module it calls `memory.Emit`, which generates all
allocator code and data directly into the shared `BuildContext`'s `WasmObj`.

---

## Address space layout

All regions are mapped at fixed offsets from **R15**, the Wasm linear-memory
base register, so every returned pointer is a valid 32-bit Wasm offset.

| Region | Offset from R15 | Size |
|--------|----------------|------|
| Static / Wasm data | `0x0000_0000` | 64 MB |
| Heap | `0x0400_0000` | 256 MB |
| Arena | `0x1400_0000` | 64 MB |

Memory is demand-paged (`MAP_FIXED` anonymous); physical pages are only
committed on first touch.

---

## Allocator state

A single zeroed page (`__vertex_alloc_state`, 4 096 bytes) holds all runtime
bookkeeping. Every stub locates it via a RIP-relative `lea`. The layout is:

| Field | Offset | Type | Description |
|-------|--------|------|-------------|
| `heap_base` | 0 | `int64` | Native address of heap region |
| `heap_cur` | 8 | `int64` | Heap bump pointer |
| `heap_end` | 16 | `int64` | End of heap region |
| `arena_base` | 24 | `int64` | Native address of arena region |
| `arena_cur` | 32 | `int64` | Arena bump pointer |
| `arena_end` | 40 | `int64` | End of arena region |
| `arena_sp` | 48 | `int64` | Arena checkpoint stack depth |
| `arena_stack` | 56 | `[64]int64` | Arena checkpoint stack |
| `free_list[0..10]` | 568 | `[11]int64` | Per-size-class Treiber free-list heads |

---

## Size classes

The heap uses 11 segregated free-list buckets. The class for a given allocation
is derived from `BSR(total_bytes - 1) - 2`, clamped to `[0, 10]`.

| Class | Max bytes | Slot size |
|-------|-----------|-----------|
| 0 | 8 | 8 |
| 1 | 16 | 16 |
| 2 | 32 | 32 |
| 3 | 64 | 64 |
| 4 | 128 | 128 |
| 5 | 256 | 256 |
| 6 | 512 | 512 |
| 7 | 1 024 | 1 024 |
| 8 | 2 048 | 2 048 |
| 9 | 4 096 | 4 096 |
| 10 (large) | unbounded | — (bump only, not reclaimed in v1) |

Freed small/medium blocks are returned to their class's **Treiber stack** via a
lock-free CAS loop. New blocks are bump-allocated from the heap when a free-list
is empty.

---

## Exported symbols

### Initialization

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `__vertex_memory_init` | `() → void` | Maps heap and arena regions, initialises allocator state. Called lazily by every stub on first use via `initCheck`. |

### Heap allocator

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_memory_heap_alloc` | `(size i32) → i32` | Allocates `size` bytes, **zero-initialised**. Returns a Wasm offset. |
| `__vertex_memory_heap_alloc_raw` | `(size i32) → i32` | Same as above but the returned memory is **uninitialised**. |
| `__vertex_memory_heap_alloc_aligned` | `(size i32, align i32) → i32` | v1: `align` is ignored; delegates to `heap_alloc`. |
| `__vertex_memory_heap_free` | `(ptr i32) → void` | Returns the block to its size-class free-list. Large blocks are not reclaimed in v1. |
| `__vertex_memory_heap_realloc` | `(ptr i32, new_size i32) → i32` | Standard realloc semantics: `ptr==0` → alloc; `new_size==0` → free; otherwise alloc-copy-free. |

Every heap block carries an 8-byte header immediately before the user pointer:

```
[0..3]  size_class  uint32
[4..7]  user_size   uint32
```

### Reference-counting allocator

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_memory_ref_alloc` | `(size i32) → i32` | Allocates a ref-counted block with `strong=1`, `weak=0`, `dtor=0`. Memory is zero-initialised. |
| `__vertex_memory_ref_alloc_weak` | `(size i32) → i32` | v1: identical to `ref_alloc`. |
| `__vertex_memory_ref_retain` | `(ptr i32) → void` | Atomically increments `strong_count`. Leaf — no frame. |
| `__vertex_memory_ref_release` | `(ptr i32) → void` | Atomically decrements `strong_count`. At zero: calls destructor (if set), then frees the block if `weak_count` is also zero. |
| `__vertex_memory_ref_set_dtor` | `(ptr i32, fn i32) → void` | Stores a native function pointer as the destructor. Leaf — no frame. |
| `__vertex_memory_ref_weak` | `(ptr i32) → i32` | Increments `weak_count` and returns the same Wasm ptr. Leaf — no frame. |
| `__vertex_memory_ref_upgrade` | `(ptr i32) → i32` | CAS loop: increments `strong_count` only if it is currently `> 0`. Returns Wasm ptr on success, `0` if the object has already been destroyed. |

Every ref-counted block carries a 32-byte RC header immediately before the user
pointer (negative offsets from the user pointer):

```
[user − 32 +  0]  strong_count  int64  atomic
[user − 32 +  8]  weak_count    int64  atomic
[user − 32 + 16]  dtor_fn_ptr   int64  native fn ptr (0 = none)
[user − 32 + 24]  size_class    uint32
[user − 32 + 28]  user_size     uint32
```

### Arena allocator

| Symbol | Wasm signature | Description |
|--------|---------------|-------------|
| `__vertex_memory_arena_push` | `() → void` | Saves the current arena bump pointer onto the checkpoint stack (max depth: 64). Exits 127 on overflow. |
| `__vertex_memory_arena_pop` | `() → void` | Restores the bump pointer from the checkpoint stack, bulk-freeing all arena allocations since the matching push. No-op on underflow. |
| `__vertex_memory_arena_alloc` | `(size i32) → i32` | Bump-allocates from the arena (8-byte aligned). Exits 127 on OOM. Arena memory is never individually freed. |

---

## Integration with the compiler driver

The driver calls `memory.Emit` automatically when `ctx.NeedsMemory` is set by
`driver.Analyze` (triggered by any import whose module field is `"memory"`).
All generated code, data, symbols, and relocations are appended directly into
the shared `BuildContext.Obj` via the internal `emitter`. No separate linking
step is needed.

> **v1 limitations**
> - Memory and concurrency stubs are only ported to `amd64`.
> - Large heap blocks (class 10) are bump-allocated and never reclaimed on free.
> - `heap_alloc_aligned` ignores the alignment argument (minimum is 8 bytes).
> - `ref_alloc_weak` is semantically identical to `ref_alloc`.