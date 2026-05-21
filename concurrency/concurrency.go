// Package concurrency generates all platform-level stubs for functions that
// carry a @async, @thread, or @process export suffix.  The frontend marks
// intent; Vertex owns the implementation — stack allocation, context switching,
// clone/fork/wait, and R15 propagation are invisible to the language frontend.
package concurrency

// Import module names — match the wasm import module fields used by frontends.
const (
	ImportModule  = "coro"
	ThreadModule  = "thread"
	ProcessModule = "process"
)

// ── Stack geometry ────────────────────────────────────────────────────────────

const (
	CoroStackSize   = 1 << 20 // 1 MiB per coroutine
	CoroGuardSize   = 4096    // PROT_NONE guard page below each coro stack
	ThreadStackSize = 2 << 20 // 2 MiB per OS thread
	ThreadGuardSize = 4096    // PROT_NONE guard page below each thread stack
)

// ── CoroHandle layout ─────────────────────────────────────────────────────────
//
// User area: 64 bytes.  Allocated via memory.ref.alloc — RC header prepended,
// user area zeroed on allocation.
//
// Cooperative and single-threaded: all fields use plain stores (no lock prefix).
// Do not share a CoroHandle across threads.

const (
	// CoroStatus: int32 — 0=suspended/ready, 2=done.
	CoroStatus = int64(0)
	// CoroCoroRSP: saved RSP for the coroutine side of the context switch.
	// On first resume this points into the primed stack (see emitCoroSpawn).
	CoroCoroRSP = int64(8)
	// CoroCallerRSP: saved RSP for the caller (resume) side.
	// Written on every coro.resume, read on every coro.yield / trampoline exit.
	CoroCallerRSP = int64(16)
	// CoroResult: int64 — value from the most recent coro.yield, or the
	// coroutine body's return value when status == 2.
	CoroResult = int64(24)
	// CoroStackBase: native address of the mmap'd region (used by the dtor).
	CoroStackBase = int64(32)
	// CoroStackLen: length passed to mmap (CoroGuardSize + CoroStackSize).
	CoroStackLen = int64(40)
	// CoroFuncNative: zero-extended 32-bit native code pointer to the body.
	// Valid for non-PIE executables (v1 requirement).
	CoroFuncNative = int64(48)
	// padding to reach 64 bytes.
	_coroPad = int64(56) //nolint:unused

	CoroHandleSize = 64
)

// ── ThreadHandle layout ───────────────────────────────────────────────────────
//
// User area: 64 bytes.  Allocated via memory.ref.alloc.
//
// ThreadTID lives at offset 0, which is also passed as the ctid pointer to
// clone(CLONE_CHILD_SETTID | CLONE_CHILD_CLEARTID).  The kernel writes the
// child TID there at start and clears it to 0 (with a futex wake) at exit.
// thread.join waits on this word via futex(FUTEX_WAIT).

const (
	// ThreadTID: int32 — kernel-managed TID / futex word.  Must be at offset 0
	// (doubles as the ctid argument to clone).
	ThreadTID = int64(0)
	// ThreadExitCode: int64 — written by the child thread just before SYS_exit.
	ThreadExitCode = int64(8)
	// ThreadStackBase: native mmap base for the thread stack.
	ThreadStackBase = int64(16)
	// ThreadStackLen: mmap length (ThreadGuardSize + ThreadStackSize).
	ThreadStackLen = int64(24)
	// ThreadFuncNative: zero-extended 32-bit native code pointer.
	ThreadFuncNative = int64(32)
	// ThreadDetached: int32 — set to 1 by thread.detach.
	ThreadDetached = int64(40)
	// padding.
	_threadPad = int64(48) //nolint:unused

	ThreadHandleSize = 64
)

// CloneFlags for thread.spawn.
//
//	CLONE_VM        0x00000100  share address space
//	CLONE_FS        0x00000200  share filesystem root / cwd / umask
//	CLONE_FILES     0x00000400  share file-descriptor table
//	CLONE_SIGHAND   0x00000800  share signal handlers
//	CLONE_CHILD_SETTID   0x01000000  write child TID into ctid at start
//	CLONE_CHILD_CLEARTID 0x00200000  clear ctid and wake futex at exit
const CloneFlags = 0x01200F00

// Futex operation codes.
const (
	FutexWait = 0
	FutexWake = 1
)

// ── ProcessHandle layout ──────────────────────────────────────────────────────
//
// User area: 32 bytes.  Plain memory.heap.alloc (not ref-counted).
// The caller must call process.wait exactly once.

const (
	// ProcPID: int64 — child PID returned by fork.
	ProcPID = int64(0)
	// ProcRawStatus: int64 — raw status word filled by wait4.
	ProcRawStatus = int64(8)
	// ProcWaited: int32 — set to 1 after the first successful process.wait.
	ProcWaited = int64(16)
	// ProcFuncNative: int64 — native code pointer copied before fork so the
	// child can read it from the COW copy.
	ProcFuncNative = int64(24)

	ProcessHandleSize = 32
)

// ── RC header constant ────────────────────────────────────────────────────────

// rcDtorOffset is the byte offset of the dtor_fn_ptr field in the RC header
// relative to the user pointer (negative displacement).
// Must match RCHeaderSize=32 and the layout in memory/memory.go.
//
//	rcDtor = -RCHeaderSize + 16 = -32 + 16 = -16
const rcDtorOffset = int64(-16)

// ── Concurrency model identifiers ────────────────────────────────────────────

// KernelKind identifies the concurrency model implied by an @kind suffix.
type KernelKind int

const (
	KindAsync   KernelKind = iota // @async  — stackful cooperative coroutines
	KindThread                    // @thread — OS threads via clone(2)
	KindProcess                   // @process — child processes via fork(2)
)

// ParamTag describes how a single parameter crosses a spawn boundary.
type ParamTag int

const (
	TagI32 ParamTag = iota // 32-bit integer, passed as-is
	TagI64                 // 64-bit integer, passed as-is
	TagF32                 // 32-bit float, passed as-is
	TagF64                 // 64-bit float, passed as-is
	TagPtr                 // linear-memory i32 offset — translated to native VA by +R15
)

// FuncInfo describes one @kind-annotated export detected in a wasm module.
type FuncInfo struct {
	FuncIdx      uint32     // absolute function index in the wasm module
	ExportName   string     // full export name, e.g. "worker@thread:ptr.i32"
	BaseName     string     // base name without suffix, e.g. "worker"
	Kind         KernelKind // concurrency backend
	ParamTags    []ParamTag // parameter annotations from the :type.type... list
	NativeSymbol string     // linker symbol the CPU backend emits for the body
}