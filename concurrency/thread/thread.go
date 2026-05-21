// Package thread compiles @thread-marked wasm functions into native threads
// using the Linux clone3 syscall. Each thread gets its own stack allocated
// via mmap. R15 (linear memory base) is re-initialised in the child thread
// before the function body runs — all threads share the same linear memory
// mapping so the base address is identical, but R15 must be explicitly set
// because clone3 does not inherit register state.
//
// Join is implemented with a futex on a per-thread word that the thread
// clears on exit via CLONE_CHILD_CLEARTID.
//
// clone_args layout (Linux kernel uapi/linux/sched.h):
//
//	+0   flags       u64  — CLONE_VM|CLONE_FS|CLONE_FILES|CLONE_SIGHAND|
//	                        CLONE_THREAD|CLONE_SYSVSEM|CLONE_PARENT_SETTID|
//	                        CLONE_CHILD_CLEARTID
//	+8   pidfd       u64  — 0 (unused)
//	+16  child_tid   u64  — pointer to tid word (cleared on exit → futex wake)
//	+24  parent_tid  u64  — pointer written with child tid on creation
//	+32  exit_signal u64  — 0 (thread, not process)
//	+40  stack       u64  — base of new stack (lowest address)
//	+48  stack_size  u64  — size of new stack in bytes
//	+56  tls         u64  — 0 (unused; we use R15 for our memory base)
//	+64  set_tid     u64  — 0
//	+72  set_tid_size u64 — 0
//	+80  cgroup      u64  — 0
//
// Total: 88 bytes.
package thread

import "github.com/vertex-language/compiler/object"

// DefaultStackSize is the default thread stack size: 2 MB.
const DefaultStackSize = 2 * 1024 * 1024

// CloneArgsSize is the size of the clone_args struct.
const CloneArgsSize = 88

// Clone flags for a standard thread.
const (
	CloneVM          = uint64(0x00000100)
	CloneFS          = uint64(0x00000200)
	CloneFiles       = uint64(0x00000400)
	CloneSighand     = uint64(0x00000800)
	CloneThread      = uint64(0x00010000)
	CloneSysvsem     = uint64(0x00040000)
	CloneParentSetTID = uint64(0x00100000)
	CloneChildClearTID = uint64(0x00200000)

	ThreadCloneFlags = CloneVM | CloneFS | CloneFiles | CloneSighand |
		CloneThread | CloneSysvsem | CloneParentSetTID | CloneChildClearTID
)

// FuncInfo describes a single @thread function to be compiled.
type FuncInfo struct {
	FuncIdx uint32
	Name    string
	Params  []bool // ptr mask
}

// CompileOptions controls thread compilation behaviour.
type CompileOptions struct {
	StackSize int // 0 → DefaultStackSize
}

// CompileResult holds compiled thread artifacts.
type CompileResult = object.WasmObj