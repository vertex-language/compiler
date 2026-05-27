// cpu/arm64/asm/regs.go
package asm

// Register numbers — 5-bit encoding used in all A64 instruction fields.
// Register 31 is context-sensitive: SP in stack instructions, XZR elsewhere.
const (
	X0  = 0;  X1  = 1;  X2  = 2;  X3  = 3
	X4  = 4;  X5  = 5;  X6  = 6;  X7  = 7
	X8  = 8;  X9  = 9;  X10 = 10; X11 = 11
	X12 = 12; X13 = 13; X14 = 14; X15 = 15
	X16 = 16; X17 = 17; X18 = 18; X19 = 19
	X20 = 20; X21 = 21; X22 = 22; X23 = 23
	X24 = 24; X25 = 25; X26 = 26; X27 = 27
	X28 = 28; X29 = 29; X30 = 30

	// Aliases for architectural roles.
	FP  = X29 // Frame pointer (AAPCS64)
	LR  = X30 // Link register — BL writes return address here
	SP  = 31  // Stack pointer (only valid in SP-class instructions)
	XZR = 31  // Zero register (read: 0; write: discard)
)

// MemBase is the register permanently holding the wasm linear-memory base
// at runtime.  Mirrors R15 on amd64.  X28 is callee-saved (AAPCS64) and
// never written by generated wasm code.
const MemBase = X28

// ArgRegs are the AAPCS64 integer argument registers in call order.
// Also used for Linux syscall arguments (syscall number goes in X8).
var ArgRegs = [8]int{X0, X1, X2, X3, X4, X5, X6, X7}

// CalleeSaved lists the callee-saved GPRs (excluding FP/LR which get
// special STP/LDP treatment in prologues).
var CalleeSaved = []int{X19, X20, X21, X22, X23, X24, X25, X26, X27, X28}