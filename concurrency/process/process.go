// Package process compiles @process-marked wasm functions into child processes
// using the Linux fork syscall on amd64 (clone on arm64, which lacks fork).
//
// After fork the child process reinitialises R15 before entering the function
// body. The child's linear memory is a copy-on-write duplicate of the parent's
// — R15 points to the same virtual address, which is valid because the OS
// gives the child its own page tables.
//
// Wait is implemented with the wait4 syscall (amd64: 61, arm64: 260).
//
// Arch dispatch:
//
//	amd64 — SYS_fork (57)  or  SYS_clone (56) with flags=SIGCHLD
//	arm64 — SYS_clone (220) with flags=SIGCHLD (fork not available)
package process

import "github.com/vertex-language/compiler/object"

// Syscall numbers used by this package.
const (
	SysForkAMD64  = 57  // fork(2)  — amd64 only
	SysCloneAMD64 = 56  // clone(2) — amd64
	SysCloneARM64 = 220 // clone(2) — arm64 (fork not available)
	SysWait4AMD64 = 61  // wait4(2) — amd64
	SysWait4ARM64 = 260 // wait4(2) — arm64
)

// SIGCHLD is passed as the exit_signal to clone so the parent receives
// SIGCHLD when the child exits — required for wait4 to work correctly.
const SIGCHLD = 17

// FuncInfo describes a single @process function to be compiled.
type FuncInfo struct {
	FuncIdx uint32
	Name    string
	Params  []bool // ptr mask
}

// CompileOptions controls process compilation behaviour.
type CompileOptions struct {
	Arch string // "amd64" or "arm64"; empty defaults to amd64
}

// CompileResult holds compiled process artifacts.
type CompileResult = object.WasmObj