// cpu/amd64/memory/memory.go  (AND cpu/arm64/memory/memory.go)
package memory

const ImportModule = "memory"

const (
	HeapOffset  = 64 << 20
	HeapSize    = 256 << 20
	ArenaOffset = HeapOffset + HeapSize
	ArenaSize   = 64 << 20

	RCHeaderSize        = 32
	HeapBlockHeaderSize = 8
	NumSizeClasses      = 11
	MaxArenaDepth       = 64
	StateSize           = 4096
)

var SizeClassMax = [NumSizeClasses]uint32{
	8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 0,
}

func SizeClass(n uint32) int {
	for i, max := range SizeClassMax {
		if max != 0 && n <= max {
			return i
		}
	}
	return NumSizeClasses - 1
}

func SlotSize(k int) uint32 {
	if k >= NumSizeClasses-1 {
		return 0
	}
	return SizeClassMax[k]
}

const (
	StateHeapBase   = int64(0)
	StateHeapCur    = int64(8)
	StateHeapEnd    = int64(16)
	StateArenaBase  = int64(24)
	StateArenaCur   = int64(32)
	StateArenaEnd   = int64(40)
	StateArenaSP    = int64(48)
	StateArenaStack = int64(56)

	StateFreeListBase = StateArenaStack + MaxArenaDepth*8
)

type ImportInfo struct {
	FuncIdx uint32
	Sub     string
	Fn      string
	PtrMask []bool
	Symbol  string
}

func SymbolName(sub, fn string) string {
	return "__vertex_memory_" + sub + "_" + fn
}