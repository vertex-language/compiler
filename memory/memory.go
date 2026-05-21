// memory/memory.go
package memory

// ImportModule is the wasm import module field for all memory primitives.
const ImportModule = "memory"

// ── Layout constants ──────────────────────────────────────────────────────────

const (
	// HeapOffset is the offset from R15 (wasm linear-memory base) at which
	// the bump-allocator heap begins. 64 MB leaves ample room for static data.
	HeapOffset = 64 << 20 // 0x0400_0000

	// HeapSize is the size of the heap region. 256 MB of demand-paged anonymous
	// memory — physical pages are only allocated on first touch.
	HeapSize = 256 << 20 // 0x1000_0000

	// ArenaOffset is the offset from R15 at which the arena region begins
	// (immediately after the heap).
	ArenaOffset = HeapOffset + HeapSize // 0x1400_0000

	// ArenaSize is the size of the arena region (64 MB).
	ArenaSize = 64 << 20 // 0x0400_0000

	// RCHeaderSize is the number of bytes prepended to every ref-counted
	// allocation. The user pointer is always (block_base + RCHeaderSize).
	//
	//   [0..7]   strong_count  int64  atomic
	//   [8..15]  weak_count    int64  atomic
	//   [16..23] dtor_fn_ptr   int64  native function pointer (0 = none)
	//   [24..27] size_class    uint32
	//   [28..31] user_size     uint32
	RCHeaderSize = 32

	// HeapBlockHeaderSize is the header for plain (non-RC) heap blocks.
	//
	//   [0..3]  size_class  uint32
	//   [4..7]  user_size   uint32
	HeapBlockHeaderSize = 8

	// NumSizeClasses is the number of segregated free-list buckets.
	// Class 10 (large) uses the bump allocator directly; freed large blocks
	// are not reclaimed in v1.
	NumSizeClasses = 11

	// MaxArenaDepth is the maximum nesting depth of arena.push calls.
	MaxArenaDepth = 64

	// StateSize is the size of the allocator state block (one page).
	StateSize = 4096
)

// SizeClassMax[k] is the largest allocation that fits in class k.
// Class 10 (large) has no upper bound (value 0 = sentinel).
var SizeClassMax = [NumSizeClasses]uint32{
	8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 0,
}

// SizeClass returns the size class for a total allocation of n bytes
// (including any header bytes already accounted for by the caller).
func SizeClass(n uint32) int {
	for i, max := range SizeClassMax {
		if max != 0 && n <= max {
			return i
		}
	}
	return NumSizeClasses - 1
}

// SlotSize returns the slot size for class k (0 for large).
func SlotSize(k int) uint32 {
	if k >= NumSizeClasses-1 {
		return 0
	}
	return SizeClassMax[k]
}

// ── Allocator state offsets within __vertex_alloc_state ──────────────────────

const (
	StateHeapBase   = 0  // int64 — native base address of heap region
	StateHeapCur    = 8  // int64 — bump pointer (atomic)
	StateHeapEnd    = 16 // int64 — end of heap region
	StateArenaBase  = 24 // int64 — native base address of arena region
	StateArenaCur   = 32 // int64 — arena bump pointer
	StateArenaEnd   = 40 // int64 — end of arena region
	StateArenaSP    = 48 // int64 — arena stack depth (0..MaxArenaDepth)
	StateArenaStack = 56 // [MaxArenaDepth]int64 — checkpoint stack

	// StateFreeListBase is the start of the free-list head array.
	// free_list[k] is at StateFreeListBase + k*8.
	StateFreeListBase = StateArenaStack + MaxArenaDepth*8 // 56 + 512 = 568
)

// ── Import metadata ───────────────────────────────────────────────────────────

// ImportInfo describes a single detected "memory" import.
type ImportInfo struct {
	FuncIdx uint32 // absolute function index in the wasm module
	Sub     string // sub-module: "heap" | "ref" | "arena"
	Fn      string // function:   "alloc" | "retain" | "release" | …
	PtrMask []bool // which parameters are linear-memory pointers
	Symbol  string // linker symbol: "__vertex_memory_<sub>_<fn>"
}

// SymbolName returns the linker symbol for the given sub-module and function.
func SymbolName(sub, fn string) string {
	return "__vertex_memory_" + sub + "_" + fn
}