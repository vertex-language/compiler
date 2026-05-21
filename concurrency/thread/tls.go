package thread

// R15Init documents the R15 initialisation strategy for new threads.
//
// When clone3 creates a new thread the new thread's register state is
// undefined except for rsp (set from clone_args.stack + clone_args.stack_size)
// and rip (set to the entry point). R15 — the linear memory base (MemBase) —
// is not inherited and must be re-established before any wasm memory access.
//
// The entry wrapper (__thread_entry_<name>) handles this unconditionally:
//
//	lea  r15, [rip + __wasm_data_base]
//	add  r15, 65536
//
// All threads in a module share the same linear memory allocation. The base
// address is therefore the same value for every thread. The linker resolves
// __wasm_data_base to the .data section start via a RelocRel32 relocation,
// exactly as it does for the main entry stub and for any coroutine resume path.
//
// No per-thread TLS segment or gs/fs base manipulation is required. R15 is
// sufficient as the sole memory-base register.
//
// If future work needs true thread-local storage (per-thread wasm globals or
// per-thread shadow stacks), the clone_args.tls field and CLONE_SETTLS can be
// used to point a segment register at a per-thread data block. That is tracked
// as a future extension and does not affect the current design.
const R15Init = "lea r15, [rip + __wasm_data_base]; add r15, 65536"