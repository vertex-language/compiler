// cpu/arm64/registers.go
package arm64

import "github.com/vertex-language/compiler/cpu/arm64/asm"

// Re-export the register and condition constants so the rest of the arm64 package
// can use them without qualifying every reference with asm.X0, asm.CondEQ, etc.
const (
	X0  = asm.X0;  X1  = asm.X1;  X2  = asm.X2;  X3  = asm.X3
	X4  = asm.X4;  X5  = asm.X5;  X6  = asm.X6;  X7  = asm.X7
	X8  = asm.X8;  X9  = asm.X9;  X10 = asm.X10; X11 = asm.X11
	X12 = asm.X12; X13 = asm.X13; X14 = asm.X14; X15 = asm.X15
	X16 = asm.X16; X17 = asm.X17; X18 = asm.X18; X19 = asm.X19
	X20 = asm.X20; X21 = asm.X21; X22 = asm.X22; X23 = asm.X23
	X24 = asm.X24; X25 = asm.X25; X26 = asm.X26; X27 = asm.X27
	X28 = asm.X28; X29 = asm.X29; X30 = asm.X30
	FP  = asm.FP;  LR  = asm.LR
	SP  = asm.SP;  XZR = asm.XZR

	// Condition codes
	CondEQ = asm.CondEQ; CondNE = asm.CondNE
	CondCS = asm.CondCS; CondCC = asm.CondCC
	CondMI = asm.CondMI; CondPL = asm.CondPL
	CondVS = asm.CondVS; CondVC = asm.CondVC
	CondHI = asm.CondHI; CondLS = asm.CondLS
	CondGE = asm.CondGE; CondLT = asm.CondLT
	CondGT = asm.CondGT; CondLE = asm.CondLE
	CondAL = asm.CondAL
)

// MemBase is the callee-saved register permanently holding the wasm
// linear-memory base at runtime.  Mirrors R15 on amd64.
const MemBase = asm.MemBase // X28

// ArgRegs are the AAPCS64 integer argument registers in call order.
var ArgRegs = asm.ArgRegs // [X0..X7]