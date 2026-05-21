package asm

// JmpReg emits: jmp reg  (indirect unconditional jump through a 64-bit register)
func (a *Assembler) JmpReg(reg int) {
	if reg >= 8 {
		a.Emit(0x41)
	}
	a.Emit(0xFF, byte(0xE0|(reg&7)))
}

// LeaRegDisp emits: lea dst, [base + disp]  (64-bit; used for address-of a field)
func (a *Assembler) LeaRegDisp(dst, base int, disp int64) {
	a.Emit(REXW(dst, base), 0x8D)
	a.EncodeMemOp(dst, base, disp)
}

// ShlRI emits: shl reg, imm8  (64-bit shift left by immediate)
func (a *Assembler) ShlRI(reg int, count uint8) {
	if count == 1 {
		a.Emit(REXWB(reg), 0xD1, ModRM(0b11, 4, byte(reg&7)))
	} else {
		a.Emit(REXWB(reg), 0xC1, ModRM(0b11, 4, byte(reg&7)), count)
	}
}

// ShrRI emits: shr reg, imm8  (64-bit logical right shift by immediate)
func (a *Assembler) ShrRI(reg int, count uint8) {
	if count == 1 {
		a.Emit(REXWB(reg), 0xD1, ModRM(0b11, 5, byte(reg&7)))
	} else {
		a.Emit(REXWB(reg), 0xC1, ModRM(0b11, 5, byte(reg&7)), count)
	}
}

// ShrRI32 emits: shr reg32, imm8  (32-bit logical right shift; zero-extends to 64 bits)
func (a *Assembler) ShrRI32(reg int, count uint8) {
	if reg >= 8 {
		a.Emit(0x41) // REX.B only — no REX.W: 32-bit operation
	}
	if count == 1 {
		a.Emit(0xD1, ModRM(0b11, 5, byte(reg&7)))
	} else {
		a.Emit(0xC1, ModRM(0b11, 5, byte(reg&7)), count)
	}
}

// MovSPToMem emits: mov [base + disp], rsp
func (a *Assembler) MovSPToMem(base int, disp int64) {
	a.StoreMem64(base, disp, RSP)
}

// MovMemToSP emits: mov rsp, [base + disp]
func (a *Assembler) MovMemToSP(base int, disp int64) {
	a.LoadMem64(RSP, base, disp)
}

// MmapAnon emits a 6-argument anonymous mmap(2) syscall.
// The caller must pre-load the desired length into RSI.
// Uses addr=NULL (kernel picks), PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS,
// fd=-1, offset=0.  Returns the mapping's native start address in RAX.
func (a *Assembler) MmapAnon() {
	a.XorRR32(RDI)                                   // addr = NULL
	// RSI = length — set by caller
	a.Emit(0xBA, 0x03, 0x00, 0x00, 0x00)             // mov edx,  3    (PROT_READ|PROT_WRITE)
	a.Emit(0x41, 0xBA, 0x22, 0x00, 0x00, 0x00)       // mov r10d, 0x22 (MAP_PRIVATE|MAP_ANONYMOUS)
	a.MovRI64Neg1(R8)                                 // mov r8,  -1   (fd)
	a.XorRR32(R9)                                     // xor r9d, r9d  (offset = 0)
	a.MovRI32(RAX, 9)                                 // mov eax,  9   (SYS_mmap)
	a.Syscall()
}

// SysMunmap emits the munmap(2) syscall.
// Caller must set RDI=addr, RSI=length.
func (a *Assembler) SysMunmap() {
	a.MovRI32(RAX, 11)
	a.Syscall()
}

// SysMprotect emits the mprotect(2) syscall.
// Caller must set RDI=addr, RSI=length, RDX=prot.
func (a *Assembler) SysMprotect() {
	a.MovRI32(RAX, 10)
	a.Syscall()
}

// SysClone emits the clone(2) syscall.
// Caller must set RDI=flags, RSI=child_stack, RDX=ptid, R10=ctid, R8=tls.
// Returns child TID to the parent, 0 to the child.
func (a *Assembler) SysClone() {
	a.MovRI32(RAX, 56)
	a.Syscall()
}

// SysFork emits the fork(2) syscall.
// Returns the child PID to the parent, 0 to the child.
func (a *Assembler) SysFork() {
	a.MovRI32(RAX, 57)
	a.Syscall()
}

// SysWait4 emits the wait4(2) syscall.
// Caller must set RDI=pid, RSI=*status, RDX=options, R10=rusage.
func (a *Assembler) SysWait4() {
	a.MovRI32(RAX, 61)
	a.Syscall()
}

// SysGettid emits the gettid(2) syscall.  Result is in RAX.
func (a *Assembler) SysGettid() {
	a.MovRI32(RAX, 186)
	a.Syscall()
}

// SysFutex emits the futex(2) syscall.
// Caller must set RDI=uaddr, RSI=op, RDX=val, R10=timeout, R8=uaddr2, R9=val3.
func (a *Assembler) SysFutex() {
	a.MovRI32(RAX, 202)
	a.Syscall()
}

// SysExitThread emits SYS_exit(60), terminating only the calling thread.
// Caller must set RDI=exit_code.
func (a *Assembler) SysExitThread() {
	a.MovRI32(RAX, 60)
	a.Syscall()
}