// Package linker — relocate.go
// Patches every relocation site in the merged section buffers.
package linker

import (
	"encoding/binary"
	"fmt"

	"github.com/vertex-language/compiler/object"
)

// relocate patches every pending relocation site.  Must be called after
// computeLayout (lnk.lay is populated), fillGOT, and relax.
func (lnk *linker) relocate() error {
	for _, r := range lnk.relocs {
		buf := lnk.sectionBuf(r.section)
		if buf == nil {
			return fmt.Errorf("linker: relocate: unknown section kind %d for symbol %q",
				r.section, r.symbol)
		}
		if r.codeOff < 0 || r.codeOff+4 > len(buf) {
			return fmt.Errorf("linker: relocate: offset %d out of bounds for symbol %q",
				r.codeOff, r.symbol)
		}

		siteVA := lnk.lay.SectionBaseVA(int(r.section)) + uint64(r.codeOff)

		switch r.kind {
		// ── PC-relative 32-bit ────────────────────────────────────────────────
		case object.RelocRel32:
			targetVA, ok := lnk.resolveSymbolVA(r.symbol)
			if !ok {
				return fmt.Errorf("linker: relocate: undefined symbol %q at offset %d",
					r.symbol, r.codeOff)
			}
			rel := int64(targetVA) - int64(siteVA+4)
			if rel < -(1<<31) || rel > (1<<31-1) {
				return fmt.Errorf("linker: rel32 overflow for %q (%d bytes)", r.symbol, rel)
			}
			binary.LittleEndian.PutUint32(buf[r.codeOff:], uint32(int32(rel)))

		// ── Absolute 64-bit ───────────────────────────────────────────────────
		case object.RelocAbs64:
			if r.codeOff+8 > len(buf) {
				return fmt.Errorf("linker: relocate: abs64 offset %d out of bounds for %q",
					r.codeOff, r.symbol)
			}
			targetVA, ok := lnk.resolveSymbolVA(r.symbol)
			if !ok {
				return fmt.Errorf("linker: relocate: undefined symbol %q", r.symbol)
			}
			binary.LittleEndian.PutUint64(buf[r.codeOff:], targetVA)

		// ── Absolute 32-bit sign-extended ─────────────────────────────────────
		case object.RelocAbs32S:
			targetVA, ok := lnk.resolveSymbolVA(r.symbol)
			if !ok {
				return fmt.Errorf("linker: relocate: undefined symbol %q", r.symbol)
			}
			if int64(targetVA) < -(1<<31) || int64(targetVA) > (1<<31-1) {
				return fmt.Errorf("linker: abs32s overflow for %q (VA=%#x)", r.symbol, targetVA)
			}
			binary.LittleEndian.PutUint32(buf[r.codeOff:], uint32(int32(targetVA)))

		// ── GOT-indirect PC-relative 32-bit ───────────────────────────────────
		// Sites that relax() converted to RelocRel32 are already handled above.
		// Anything remaining here is patched via its GOT slot.
		case object.RelocGOTPCRel32:
			slotIdx := lnk.gotTable.slotFor(r.symbol)
			slotVA  := lnk.gotTable.slotVA(slotIdx, lnk.lay.GotVA)
			rel := int64(slotVA) - int64(siteVA+4)
			if rel < -(1<<31) || rel > (1<<31-1) {
				return fmt.Errorf("linker: gotpcrel32 overflow for %q (%d bytes)", r.symbol, rel)
			}
			binary.LittleEndian.PutUint32(buf[r.codeOff:], uint32(int32(rel)))

		// ── TLS IE GOT-indirect PC-relative 32-bit ────────────────────────────
		case object.RelocTLSGOTPCRel32:
			slotIdx := lnk.gotTable.tlsSlotFor(r.symbol)
			slotVA  := lnk.gotTable.slotVA(slotIdx, lnk.lay.GotVA)
			rel := int64(slotVA) - int64(siteVA+4)
			if rel < -(1<<31) || rel > (1<<31-1) {
				return fmt.Errorf("linker: tlsgottpoff overflow for %q (%d bytes)", r.symbol, rel)
			}
			binary.LittleEndian.PutUint32(buf[r.codeOff:], uint32(int32(rel)))

		// ── TLS local exec: direct TP-relative 32-bit ─────────────────────────
		case object.RelocTPOFF32:
			tlsOff, ok := lnk.sym.tlsOffset(r.symbol)
			if !ok {
				return fmt.Errorf("linker: tpoff32: %q is not a TLS symbol", r.symbol)
			}
			alignedBlock := alignUp(uint64(len(lnk.tdata))+lnk.tbss, tlsBlockAlign)
			tpoff := int64(uint64(tlsOff)) - int64(alignedBlock)
			if tpoff < -(1<<31) || tpoff > (1<<31-1) {
				return fmt.Errorf("linker: tpoff32 overflow for %q (%d)", r.symbol, tpoff)
			}
			binary.LittleEndian.PutUint32(buf[r.codeOff:], uint32(int32(tpoff)))

		default:
			return fmt.Errorf("linker: unsupported relocation kind %d for symbol %q",
				r.kind, r.symbol)
		}
	}
	return nil
}

// sectionBuf returns the merged section buffer for the given sectionKind.
func (lnk *linker) sectionBuf(s sectionKind) []byte {
	switch s {
	case secKindText:   return lnk.code
	case secKindROData: return lnk.rodata
	case secKindData:   return lnk.data
	}
	return nil
}