// cpu/amd64/registers.go
package amd64

// This package uses the x86_64/asm sub-package for low-level instruction
// encoding. Moving that package to cpu/amd64/asm is a separate refactor.
import "github.com/vertex-language/compiler/cpu/amd64/asm"

const (
	RAX = asm.RAX; RCX = asm.RCX; RDX = asm.RDX; RBX = asm.RBX
	RSP = asm.RSP; RBP = asm.RBP; RSI = asm.RSI; RDI = asm.RDI
	R8  = asm.R8;  R9  = asm.R9;  R10 = asm.R10; R11 = asm.R11
	R12 = asm.R12; R13 = asm.R13; R14 = asm.R14; R15 = asm.R15
)

// MemBase is the register permanently holding the wasm linear-memory base
// at runtime. Callee-saved; never written by generated wasm code.
const MemBase = R15

// ArgRegs are the SysV AMD64 integer argument registers in call order.
var ArgRegs = asm.ArgRegs