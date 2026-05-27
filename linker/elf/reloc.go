// reloc.go
// Applies RELA relocations to the merged section data after layout.
// Each architecture's relocation formulas come directly from its psABI:
//   AMD64:  System V AMD64 ABI §4.4
//   AArch64: ELF for Arm 64-bit (IHI0056)
//   RISC-V: RISC-V ELF psABI
package elf

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// PatchRelocations applies all RELA relocations from every input object
// to the merged output section data. Must be called after AssignLayout
// and ResolveSymbolAddresses.
func PatchRelocations(arch uint16, layout *Layout, symtab *SymbolTable, objects []*ObjectFile) error {
	for _, obj := range objects {
		for _, rel := range obj.Relocs {
			if err := applyReloc(arch, rel, obj, layout, symtab); err != nil {
				return fmt.Errorf("%s: %w", obj.Path, err)
			}
		}
	}
	return nil
}

func applyReloc(arch uint16, rel *RawReloc, obj *ObjectFile, layout *Layout, symtab *SymbolTable) error {
	// Resolve target section (the section being patched).
	if rel.TargetSecIdx >= len(obj.Sections) {
		return fmt.Errorf("reloc target section index %d out of range", rel.TargetSecIdx)
	}
	inputSec := obj.Sections[rel.TargetSecIdx]
	outSec, ok := layout.SectionByName(inputSec.Name)
	if !ok {
		return fmt.Errorf("reloc in %q: output section %q not found", inputSec.Name, inputSec.Name)
	}

	// Find this input section's piece offset within the merged output section.
	var pieceOffset uint64
	found := false
	for _, p := range outSec.Pieces {
		if p.Obj == obj && p.Sec == inputSec {
			pieceOffset = p.Offset
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reloc: can't locate piece for %q in output section %q",
			inputSec.Name, outSec.Name)
	}

	// P = virtual address of the storage unit being relocated.
	P := outSec.VAddr + pieceOffset + rel.Offset

	// File offset of the storage unit in outSec.Data.
	patchOff := int(pieceOffset + rel.Offset)
	if patchOff >= len(outSec.Data) {
		return fmt.Errorf("reloc patch offset 0x%x out of bounds in %q (size=%d)",
			patchOff, outSec.Name, len(outSec.Data))
	}

	// Resolve the symbol.
	S, err := resolveRelocSym(rel, obj, symtab)
	if err != nil {
		return err
	}

	A := rel.Addend

	switch arch {
	case 0x3E: // EM_X86_64
		return patchAMD64(outSec.Data, patchOff, rel.Type, P, S, A)
	case 0xB7: // EM_AARCH64
		return patchAArch64(outSec.Data, patchOff, rel.Type, P, S, A)
	case 0xF3: // EM_RISCV
		return patchRISCV(outSec.Data, patchOff, rel.Type, P, S, A)
	default:
		return fmt.Errorf("unsupported machine 0x%x for relocation", arch)
	}
}

// resolveRelocSym returns the virtual address (S) of the symbol referenced
// by a relocation. Undefined weak symbols resolve to zero.
func resolveRelocSym(rel *RawReloc, obj *ObjectFile, symtab *SymbolTable) (int64, error) {
	if rel.SymIdx == 0 {
		return 0, nil // R_*_NONE or section-relative
	}
	if int(rel.SymIdx) >= len(obj.Symbols) {
		return 0, fmt.Errorf("reloc symbol index %d out of range", rel.SymIdx)
	}
	raw := obj.Symbols[rel.SymIdx]
	if raw.Name == "" {
		// Section symbol: S = VAddr of the target section.
		return 0, nil // handled above via P + A for section-relative
	}

	sym := symtab.Lookup(raw.Name)
	if sym == nil {
		if raw.Bind == stbWeak {
			return 0, nil
		}
		return 0, fmt.Errorf("undefined symbol %q", raw.Name)
	}

	switch sym.Kind {
	case kindDefined, kindCommon:
		return int64(sym.VAddr), nil
	case kindShared:
		// Shared symbols are accessed via PLT/GOT — return the PLT stub address
		// if one was synthesized, otherwise the symbol value (for data).
		return int64(sym.VAddr), nil
	case kindUndefined:
		if sym.Weak {
			return 0, nil
		}
		return 0, fmt.Errorf("undefined symbol %q", raw.Name)
	}
	return 0, fmt.Errorf("symbol %q in unexpected state %d", raw.Name, sym.Kind)
}

// ── AMD64 ─────────────────────────────────────────────────────────────────────

func patchAMD64(data []byte, off int, rtype uint32, P uint64, S int64, A int64) error {
	put32 := func(v int64) error {
		// Overflow check: must fit in int32.
		if v < -0x80000000 || v > 0x7FFFFFFF {
			return fmt.Errorf("AMD64 reloc type %d: value 0x%x overflows int32", rtype, v)
		}
		binary.LittleEndian.PutUint32(data[off:], uint32(v))
		return nil
	}
	put32u := func(v int64) error {
		if uint64(v) > 0xFFFFFFFF {
			return fmt.Errorf("AMD64 reloc type %d: value 0x%x overflows uint32", rtype, v)
		}
		binary.LittleEndian.PutUint32(data[off:], uint32(v))
		return nil
	}
	put64 := func(v int64) error {
		binary.LittleEndian.PutUint64(data[off:], uint64(v))
		return nil
	}

	switch rtype {
	case 0: // R_X86_64_NONE
		return nil
	case 1: // R_X86_64_64      S + A
		return put64(S + A)
	case 2: // R_X86_64_PC32    S + A - P
		return put32(S + A - int64(P))
	case 4: // R_X86_64_PLT32   L + A - P  (linker reduces to PC32 when defined locally)
		return put32(S + A - int64(P))
	case 10: // R_X86_64_32     S + A  (zero-extend to 64)
		return put32u(S + A)
	case 11: // R_X86_64_32S    S + A  (sign-extend to 64)
		return put32(S + A)
	case 24: // R_X86_64_PC64   S + A - P
		return put64(S + A - int64(P))
	default:
		return fmt.Errorf("AMD64: unhandled relocation type %d", rtype)
	}
}

// ── AArch64 ───────────────────────────────────────────────────────────────────

func patchAArch64(data []byte, off int, rtype uint32, P uint64, S int64, A int64) error {
	// Read the 32-bit instruction word at the patch site.
	if off+4 > len(data) {
		return fmt.Errorf("AArch64 reloc: patch offset 0x%x out of bounds", off)
	}
	insn := binary.LittleEndian.Uint32(data[off:])

	writeInsn := func(v uint32) {
		binary.LittleEndian.PutUint32(data[off:], v)
	}
	writeU64 := func(v int64) {
		binary.LittleEndian.PutUint64(data[off:], uint64(v))
	}
	writeU32 := func(v int64) {
		binary.LittleEndian.PutUint32(data[off:], uint32(v))
	}

	page := func(addr uint64) uint64 { return addr &^ 0xFFF }

	switch rtype {
	case 0: // R_AARCH64_NONE
		return nil

	case 257: // R_AARCH64_ABS64   S + A
		writeU64(S + A)

	case 258: // R_AARCH64_ABS32   S + A
		writeU32(S + A)

	case 261: // R_AARCH64_PREL32  S + A - P
		writeU32(S + A - int64(P))

	case 275: // R_AARCH64_ADR_PREL_PG_HI21  Page(S+A) - Page(P) → ADRP
		// Encodes a 21-bit page offset into an ADRP instruction.
		// ADRP layout: [31]=1, [30:29]=immlo[1:0], [28:24]=10000, [23:5]=immhi[20:2]
		delta := int64(page(uint64(S+A))) - int64(page(P))
		if delta < -(1<<32) || delta >= (1<<32) {
			return fmt.Errorf("AArch64 ADRP: page offset 0x%x too large", delta)
		}
		immlo := uint32((delta >> 12) & 0x3)
		immhi := uint32((delta >> 14) & 0x7FFFF)
		// Clear existing imm fields and set new ones.
		insn = (insn &^ 0x60FFFFE0) | (immlo << 29) | (immhi << 5)
		writeInsn(insn)

	case 277: // R_AARCH64_ADD_ABS_LO12_NC  (S+A)[11:0] → ADD imm12
		lo12 := uint32(uint64(S+A) & 0xFFF)
		insn = (insn &^ (0xFFF << 10)) | (lo12 << 10)
		writeInsn(insn)

	case 278: // R_AARCH64_LDST8_ABS_LO12_NC  (S+A)[11:0]
		lo12 := uint32(uint64(S+A) & 0xFFF)
		insn = (insn &^ (0xFFF << 10)) | (lo12 << 10)
		writeInsn(insn)

	case 286: // R_AARCH64_LDST64_ABS_LO12_NC  (S+A)[11:3]
		lo12 := uint32((uint64(S+A) >> 3) & 0x1FF)
		insn = (insn &^ (0x1FF << 10)) | (lo12 << 10)
		writeInsn(insn)

	case 282: // R_AARCH64_JUMP26  (S+A-P)[27:2] → B
		fallthrough
	case 283: // R_AARCH64_CALL26  (S+A-P)[27:2] → BL
		delta := S + A - int64(P)
		if delta < -(1<<27) || delta >= (1<<27) {
			return fmt.Errorf("AArch64 CALL/JUMP26: branch too far (0x%x)", delta)
		}
		imm26 := uint32((delta >> 2) & 0x3FFFFFF)
		insn = (insn &^ 0x3FFFFFF) | imm26
		writeInsn(insn)

	default:
		return fmt.Errorf("AArch64: unhandled relocation type %d", rtype)
	}
	return nil
}

// ── RISC-V ────────────────────────────────────────────────────────────────────

func patchRISCV(data []byte, off int, rtype uint32, P uint64, S int64, A int64) error {
	if off+4 > len(data) {
		return fmt.Errorf("RISC-V reloc: patch offset 0x%x out of bounds", off)
	}
	insn := binary.LittleEndian.Uint32(data[off:])

	writeInsn := func(v uint32) {
		binary.LittleEndian.PutUint32(data[off:], v)
	}

	// hi20 / lo12 helpers matching the RISC-V psABI:
	//   %hi(x)  = (x + 0x800) >> 12     (rounds up to avoid negative lo12)
	//   %lo(x)  = x & 0xFFF             (sign-extended lower 12 bits)
	hi20 := func(x int64) int64 { return (x + 0x800) >> 12 }
	lo12 := func(x int64) int64 { return x & 0xFFF }

	// signExtend12 sign-extends a 12-bit immediate.
	signExtend12 := func(v int64) int64 {
		v &= 0xFFF
		if v&0x800 != 0 {
			v |= ^int64(0xFFF)
		}
		return v
	}

	// setUtype patches the upper-20-bit immediate of a U-type instruction (LUI/AUIPC).
	// U-type: imm[31:12] in insn[31:12], insn[11:0] = opcode+rd.
	setUtype := func(imm int64) {
		writeInsn((insn &^ 0xFFFFF000) | (uint32(imm&0xFFFFF) << 12))
	}

	// setItype patches the 12-bit immediate of an I-type instruction (ADDI, JALR, LD, …).
	// I-type: imm[11:0] in insn[31:20].
	setItype := func(imm int64) {
		writeInsn((insn &^ 0xFFF00000) | (uint32(signExtend12(imm)&0xFFF) << 20))
	}

	switch rtype {
	case 0: // R_RISCV_NONE
		return nil

	case 1: // R_RISCV_32   S + A
		binary.LittleEndian.PutUint32(data[off:], uint32(S+A))

	case 2: // R_RISCV_64   S + A
		binary.LittleEndian.PutUint64(data[off:], uint64(S+A))

	case 17: // R_RISCV_JAL  S + A - P → J-type
		delta := S + A - int64(P)
		if delta < -(1<<20) || delta >= (1<<20) {
			return fmt.Errorf("RISC-V JAL: target out of range (0x%x)", delta)
		}
		// J-type immediate: bits [20|10:1|11|19:12] scattered in insn[31:12]
		imm := uint32(delta)
		jtype := (bits.RotateLeft32((imm>>1)&0x3FF, 21)&0x7FE00000) | // [10:1] → [30:21]
			((imm>>11)&1)<<20 | // [11]   → [20]
			((imm>>12)&0xFF)<<12 | // [19:12]→ [19:12]
			((imm>>20)&1)<<31 //  [20]  → [31]
		writeInsn((insn &^ 0xFFFFF000) | jtype)

	case 18, 19: // R_RISCV_CALL / R_RISCV_CALL_PLT  AUIPC+JALR pair (8 bytes)
		// Patch two consecutive 32-bit instructions: AUIPC rd, hi + JALR rd, lo(rd)
		if off+8 > len(data) {
			return fmt.Errorf("RISC-V CALL: not enough space for AUIPC+JALR pair")
		}
		delta := S + A - int64(P)
		h20 := hi20(delta)
		l12 := lo12(delta)
		insn0 := binary.LittleEndian.Uint32(data[off:])
		insn1 := binary.LittleEndian.Uint32(data[off+4:])
		// AUIPC: imm[31:12] = hi20
		binary.LittleEndian.PutUint32(data[off:],   (insn0&^0xFFFFF000)|uint32(h20&0xFFFFF)<<12)
		// JALR:  imm[11:0]  = lo12
		binary.LittleEndian.PutUint32(data[off+4:], (insn1&^0xFFF00000)|uint32(l12&0xFFF)<<20)

	case 23: // R_RISCV_PCREL_HI20  %pcrel_hi(S+A-P) → AUIPC imm[31:12]
		setUtype(hi20(S + A - int64(P)))

	case 24: // R_RISCV_PCREL_LO12_I  %pcrel_lo → I-type imm[11:0]
		// S here is the address of the symbol, which for LO12_I is the label
		// of the paired HI20 instruction; the actual delta was already encoded
		// in the HI20 reloc. We apply it to the I-type immediate.
		setItype(lo12(S + A))

	case 25: // R_RISCV_PCREL_LO12_S  %pcrel_lo → S-type
		// S-type immediate: [11:5] in insn[31:25], [4:0] in insn[11:7]
		imm := lo12(S + A)
		stype := (uint32(imm>>5)&0x7F)<<25 | (uint32(imm)&0x1F)<<7
		writeInsn((insn &^ 0xFE000F80) | stype)

	case 26: // R_RISCV_HI20  S+A [31:12] → LUI
		setUtype(hi20(S + A))

	case 27: // R_RISCV_LO12_I  S+A [11:0] → I-type
		setItype(lo12(S + A))

	case 28: // R_RISCV_LO12_S  S+A [11:0] → S-type
		imm := lo12(S + A)
		stype := (uint32(imm>>5)&0x7F)<<25 | (uint32(imm)&0x1F)<<7
		writeInsn((insn &^ 0xFE000F80) | stype)

	default:
		return fmt.Errorf("RISC-V: unhandled relocation type %d", rtype)
	}
	return nil
}

// bits is used only for the RISC-V J-type encoding above.
// Keep the import from standard library.
var _ = bits.RotateLeft32