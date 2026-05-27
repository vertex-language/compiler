// reloc_riscv64.go
package elf

// R_RISCV_* relocation type constants for RISC-V 64-bit (EM_RISCV).
//
// Source: RISC-V ELF psABI Specification
// https://github.com/riscv-non-isa/riscv-elf-psabi-doc
//
// Computation key (shared with AMD64/ARM64 where applicable):
//   A  = explicit addend (r_addend)
//   B  = base address at which the shared object is loaded
//   G  = offset into the GOT for the symbol
//   GOT = address of the Global Offset Table
//   P  = address of the storage unit being relocated (r_offset)
//   S  = value of the symbol referenced
//   V  = value at the relocated location
//   GP = value of __global_pointer$
//
// RISC-V-specific immediates:
//   %hi(x)   = (x + 0x800) >> 12   upper 20 bits with rounding
//   %lo(x)   = x & 0xFFF           lower 12 bits (sign-extended)
//   %pcrel_hi(x) = %hi(x − P)
//   %pcrel_lo(label) = %lo(label_hi_sym − P)  where label_hi_sym has a HI20 reloc
const (
	// ── Dynamic relocations ───────────────────────────────────────────────
	// These are emitted into the final binary and processed by the runtime linker.
	R_RISCV_NONE         = 0  // none
	R_RISCV_32           = 1  // S + A                        (32-bit absolute)
	R_RISCV_64           = 2  // S + A                        (64-bit absolute; also GLOB_DAT)
	R_RISCV_RELATIVE     = 3  // B + A                        (position-independent)
	R_RISCV_COPY         = 4  // copy symbol at runtime
	R_RISCV_JUMP_SLOT    = 5  // S                            (PLT GOT slot)
	R_RISCV_TLS_DTPMOD32 = 6  // TLS module index (32-bit)
	R_RISCV_TLS_DTPMOD64 = 7  // TLS module index (64-bit)
	R_RISCV_TLS_DTPREL32 = 8  // TLS block offset (32-bit)
	R_RISCV_TLS_DTPREL64 = 9  // TLS block offset (64-bit)
	R_RISCV_TLS_TPREL32  = 10 // TLS initial-exec offset (32-bit)
	R_RISCV_TLS_TPREL64  = 11 // TLS initial-exec offset (64-bit)
	R_RISCV_TLSDESC      = 12 // TLS descriptor (runtime)
	R_RISCV_IRELATIVE    = 58 // B + A → ifunc resolver result

	// ── Static relocations ────────────────────────────────────────────────
	// These are processed by the static linker (ld) when building the binary.
	R_RISCV_BRANCH          = 16 // S + A − P    [12:1]   (B-type: BEQ, BNE, …)
	R_RISCV_JAL             = 17 // S + A − P    [20:1]   (J-type: JAL)
	R_RISCV_CALL            = 18 // S + A − P    [31:0]   (AUIPC+JALR pair, 8 bytes)
	R_RISCV_CALL_PLT        = 19 // S + A − P    [31:0]   (like CALL but through PLT)
	R_RISCV_GOT_HI20        = 20 // G + GOT − P  [31:12]  (AUIPC targeting GOT entry)
	R_RISCV_TLS_GOT_HI20    = 21 // G + GOT − P  [31:12]  (TLS IE GOT AUIPC)
	R_RISCV_TLS_GD_HI20     = 22 // G + GOT − P  [31:12]  (TLS GD GOT AUIPC)
	R_RISCV_PCREL_HI20      = 23 // S + A − P    [31:12]  (AUIPC %pcrel_hi)
	R_RISCV_PCREL_LO12_I    = 24 // S − P        [11:0]   (I-type %pcrel_lo; P = paired HI20)
	R_RISCV_PCREL_LO12_S    = 25 // S − P        [11:0]   (S-type %pcrel_lo)
	R_RISCV_HI20            = 26 // S + A        [31:12]  (LUI %hi)
	R_RISCV_LO12_I          = 27 // S + A        [11:0]   (I-type %lo)
	R_RISCV_LO12_S          = 28 // S + A        [11:0]   (S-type %lo)
	R_RISCV_TPREL_HI20      = 29 // S + A − TP   [31:12]  (TLS LE AUIPC)
	R_RISCV_TPREL_LO12_I    = 30 // S + A − TP   [11:0]   (TLS LE I-type)
	R_RISCV_TPREL_LO12_S    = 31 // S + A − TP   [11:0]   (TLS LE S-type)
	R_RISCV_TPREL_ADD       = 32 // TP + S + A             (TLS LE ADD pseudo-reloc)

	// ── Addend-only relocations (used for linker-internal accounting) ─────
	R_RISCV_ADD8  = 33 // V + S + A (8-bit addend)
	R_RISCV_ADD16 = 34 // V + S + A (16-bit)
	R_RISCV_ADD32 = 35 // V + S + A (32-bit)
	R_RISCV_ADD64 = 36 // V + S + A (64-bit)
	R_RISCV_SUB8  = 37 // V − S − A (8-bit)
	R_RISCV_SUB16 = 38 // V − S − A (16-bit)
	R_RISCV_SUB32 = 39 // V − S − A (32-bit)
	R_RISCV_SUB64 = 40 // V − S − A (64-bit)

	// ── Relaxation and compressed-ISA relocations ─────────────────────────
	R_RISCV_GOT32_PCREL = 41 // G − P    [31:0]   (32-bit PC-relative GOT offset)
	R_RISCV_ALIGN       = 43 //                    (alignment directive; addend = nop bytes)
	R_RISCV_RVC_BRANCH  = 44 // S + A − P [8:1]   (CB-type RVC branch)
	R_RISCV_RVC_JUMP    = 45 // S + A − P [11:1]  (CJ-type RVC jump)
	R_RISCV_RELAX       = 51 //                    (hints that the preceding insn may relax)
	R_RISCV_SUB6        = 52 // V − S − A [5:0]
	R_RISCV_SET6        = 53 // S + A     [5:0]
	R_RISCV_SET8        = 54 // S + A     [7:0]
	R_RISCV_SET16       = 55 // S + A     [15:0]
	R_RISCV_SET32       = 56 // S + A     [31:0]
	R_RISCV_32_PCREL    = 57 // S + A − P [31:0]  (32-bit PC-relative)
	R_RISCV_PLT32       = 59 // S + A − P [31:0]  (32-bit PLT-relative; debug sections)
)