// reloc_arm64.go
package elf

// R_AARCH64_* relocation type constants for AArch64 (EM_AARCH64).
//
// Source: ELF for the Arm 64-bit Architecture (AArch64), IHI0056
// https://github.com/ARM-software/abi-aa/blob/main/aaelf64/aaelf64.rst
//
// Computation key (shared symbols with AMD64 where applicable):
//   A   = explicit addend (r_addend)
//   B   = base address at which the shared object is loaded
//   G   = offset into the GOT for the symbol
//   GOT = address of the Global Offset Table
//   L   = PLT address for the symbol
//   P   = address of the storage unit being relocated (r_offset)
//   S   = value of the symbol referenced by the relocation
//   Z   = size of the symbol
//
// AArch64-specific:
//   Page(x) = x & ~0xFFF   (4 KiB page base)
//   Lo12(x) = x &  0xFFF   (low 12 bits)
const (
	// ── Static data relocations ───────────────────────────────────────────
	R_AARCH64_NONE   = 0   // none
	R_AARCH64_ABS64  = 257 // S + A                (64-bit absolute)
	R_AARCH64_ABS32  = 258 // S + A                (32-bit, assert fits)
	R_AARCH64_ABS16  = 259 // S + A                (16-bit, assert fits)
	R_AARCH64_PREL64 = 260 // S + A − P            (64-bit PC-relative)
	R_AARCH64_PREL32 = 261 // S + A − P            (32-bit PC-relative)
	R_AARCH64_PREL16 = 262 // S + A − P            (16-bit PC-relative)

	// ── MOVW immediate relocations ────────────────────────────────────────
	R_AARCH64_MOVW_UABS_G0    = 263 // S + A  [15:0]
	R_AARCH64_MOVW_UABS_G0_NC = 264 // S + A  [15:0]  (no overflow check)
	R_AARCH64_MOVW_UABS_G1    = 265 // S + A  [31:16]
	R_AARCH64_MOVW_UABS_G1_NC = 266 // S + A  [31:16]
	R_AARCH64_MOVW_UABS_G2    = 267 // S + A  [47:32]
	R_AARCH64_MOVW_UABS_G2_NC = 268 // S + A  [47:32]
	R_AARCH64_MOVW_UABS_G3    = 269 // S + A  [63:48]
	R_AARCH64_MOVW_SABS_G0    = 270 // S + A  [15:0]  signed
	R_AARCH64_MOVW_SABS_G1    = 271 // S + A  [31:16] signed
	R_AARCH64_MOVW_SABS_G2    = 272 // S + A  [47:32] signed

	// ── PC-relative instruction relocations ──────────────────────────────
	R_AARCH64_LD_PREL_LO19        = 273 // S + A − P        [20:2]  (LDR literal)
	R_AARCH64_ADR_PREL_LO21       = 274 // S + A − P        [20:0]  (ADR)
	R_AARCH64_ADR_PREL_PG_HI21    = 275 // Page(S+A)−Page(P)[32:12] (ADRP)
	R_AARCH64_ADR_PREL_PG_HI21_NC = 276 // as above, no overflow check

	// ── ADD / LDR / STR immediate relocations ────────────────────────────
	R_AARCH64_ADD_ABS_LO12_NC     = 277 // S + A  [11:0]  (ADD imm)
	R_AARCH64_LDST8_ABS_LO12_NC   = 278 // S + A  [11:0]  (LDR/STR byte)
	R_AARCH64_LDST16_ABS_LO12_NC  = 284 // S + A  [11:1]
	R_AARCH64_LDST32_ABS_LO12_NC  = 285 // S + A  [11:2]
	R_AARCH64_LDST64_ABS_LO12_NC  = 286 // S + A  [11:3]
	R_AARCH64_LDST128_ABS_LO12_NC = 299 // S + A  [11:4]

	// ── Control-flow relocations ──────────────────────────────────────────
	R_AARCH64_TSTBR14  = 279 // S + A − P  [15:2]  (TBZ, TBNZ)
	R_AARCH64_CONDBR19 = 280 // S + A − P  [20:2]  (B.cond, CBZ, CBNZ)
	R_AARCH64_JUMP26   = 282 // S + A − P  [27:2]  (B)
	R_AARCH64_CALL26   = 283 // S + A − P  [27:2]  (BL)

	// ── GOT relocations ───────────────────────────────────────────────────
	R_AARCH64_GOT_LD_PREL19     = 309 // G(S) − P               [20:2]
	R_AARCH64_ADR_GOT_PAGE      = 311 // Page(G(S)) − Page(P)   (ADRP)
	R_AARCH64_LD64_GOT_LO12_NC  = 312 // G(S)                   [11:3]
	R_AARCH64_LD64_GOTPAGE_LO15 = 313 // G(S) − Page(GOT)       [14:3]

	// ── TLS General Dynamic / Local Dynamic ──────────────────────────────
	R_AARCH64_TLSGD_ADR_PREL21         = 512
	R_AARCH64_TLSGD_ADR_PAGE21         = 513
	R_AARCH64_TLSGD_ADD_LO12_NC        = 514
	R_AARCH64_TLSGD_MOVW_G1            = 515
	R_AARCH64_TLSGD_MOVW_G0_NC         = 516
	R_AARCH64_TLSLD_ADR_PREL21         = 517
	R_AARCH64_TLSLD_ADR_PAGE21         = 518
	R_AARCH64_TLSLD_ADD_LO12_NC        = 519
	R_AARCH64_TLSLD_MOVW_G1            = 520
	R_AARCH64_TLSLD_MOVW_G0_NC         = 521
	R_AARCH64_TLSLD_LD_PREL19          = 522
	R_AARCH64_TLSLD_MOVW_DTPREL_G2     = 523
	R_AARCH64_TLSLD_MOVW_DTPREL_G1     = 524
	R_AARCH64_TLSLD_MOVW_DTPREL_G1_NC  = 525
	R_AARCH64_TLSLD_MOVW_DTPREL_G0     = 526
	R_AARCH64_TLSLD_MOVW_DTPREL_G0_NC  = 527
	R_AARCH64_TLSLD_ADD_DTPREL_HI12    = 528
	R_AARCH64_TLSLD_ADD_DTPREL_LO12    = 529
	R_AARCH64_TLSLD_ADD_DTPREL_LO12_NC = 530

	// ── TLS Initial Exec / Local Exec ─────────────────────────────────────
	R_AARCH64_TLSIE_MOVW_GOTTPREL_G1      = 539
	R_AARCH64_TLSIE_MOVW_GOTTPREL_G0_NC   = 540
	R_AARCH64_TLSIE_ADR_GOTTPREL_PAGE21   = 541
	R_AARCH64_TLSIE_LD64_GOTTPREL_LO12_NC = 542
	R_AARCH64_TLSIE_LD_GOTTPREL_PREL19    = 543
	R_AARCH64_TLSLE_MOVW_TPREL_G2         = 544
	R_AARCH64_TLSLE_MOVW_TPREL_G1         = 545
	R_AARCH64_TLSLE_MOVW_TPREL_G1_NC      = 546
	R_AARCH64_TLSLE_MOVW_TPREL_G0         = 547
	R_AARCH64_TLSLE_MOVW_TPREL_G0_NC      = 548
	R_AARCH64_TLSLE_ADD_TPREL_HI12        = 549
	R_AARCH64_TLSLE_ADD_TPREL_LO12        = 550
	R_AARCH64_TLSLE_ADD_TPREL_LO12_NC     = 551

	// ── TLS descriptor relocations ────────────────────────────────────────
	R_AARCH64_TLSDESC_LD_PREL19   = 560
	R_AARCH64_TLSDESC_ADR_PREL21  = 561
	R_AARCH64_TLSDESC_ADR_PAGE21  = 562
	R_AARCH64_TLSDESC_LD64_LO12   = 563
	R_AARCH64_TLSDESC_ADD_LO12    = 564
	R_AARCH64_TLSDESC_OFF_G1      = 565
	R_AARCH64_TLSDESC_OFF_G0_NC   = 566
	R_AARCH64_TLSDESC_LDR         = 567
	R_AARCH64_TLSDESC_ADD         = 568
	R_AARCH64_TLSDESC_CALL        = 569

	// ── Dynamic (runtime) relocations ─────────────────────────────────────
	R_AARCH64_COPY       = 1024 // copy symbol at runtime
	R_AARCH64_GLOB_DAT   = 1025 // S + A  (GOT entry ← symbol address)
	R_AARCH64_JUMP_SLOT  = 1026 // S + A  (PLT GOT slot)
	R_AARCH64_RELATIVE   = 1027 // B + A  (position-independent)
	R_AARCH64_TLS_DTPMOD = 1028 // TLS module index
	R_AARCH64_TLS_DTPREL = 1029 // TLS block offset
	R_AARCH64_TLS_TPREL  = 1030 // TLS initial-exec offset
	R_AARCH64_TLSDESC    = 1031 // TLS descriptor
	R_AARCH64_IRELATIVE  = 1032 // B + A → ifunc resolver result
)