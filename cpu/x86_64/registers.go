package x86_64

import "github.com/vertex-language/compiler/cpu/x86_64/asm"

// Re-export register constants so the rest of the x86_64 package can use the
// short names without qualifying every reference with asm.RAX etc.
const (
	RAX = asm.RAX; RCX = asm.RCX; RDX = asm.RDX; RBX = asm.RBX
	RSP = asm.RSP; RBP = asm.RBP; RSI = asm.RSI; RDI = asm.RDI
	R8  = asm.R8;  R9  = asm.R9;  R10 = asm.R10; R11 = asm.R11
	R12 = asm.R12; R13 = asm.R13; R14 = asm.R14; R15 = asm.R15
)

// MemBase is the register that permanently holds the wasm linear-memory base.
// Callee-saved (R15); never written by generated wasm code.
const MemBase = R15

// ArgRegs are the SysV AMD64 integer argument registers in call order.
var ArgRegs = asm.ArgRegs