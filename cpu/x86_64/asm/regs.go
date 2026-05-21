package asm

// Register numbers — 3-bit encoding used in ModRM, SIB, and short-form opcodes.
// R8–R15 require a REX prefix with the appropriate extension bit set.
const (
	RAX = 0; RCX = 1; RDX = 2; RBX = 3
	RSP = 4; RBP = 5; RSI = 6; RDI = 7
	R8  = 8; R9  = 9; R10 = 10; R11 = 11
	R12 = 12; R13 = 13; R14 = 14; R15 = 15
)

// ArgRegs lists the SysV AMD64 integer argument registers in call order.
var ArgRegs = [6]int{RDI, RSI, RDX, RCX, R8, R9}