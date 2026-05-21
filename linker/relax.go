// Package linker — relax.go
// GOTPCRELX relaxation: rewrites GOT-indirect instruction sequences to direct
// PC-relative equivalents when the symbol is non-preemptible (always true in
// a static executable).
//
// Per the x86-64 psABI appendix B.2 "Optimize GOTPCRELX Relocations":
//
// R_X86_64_GOTPCRELX (no REX prefix; 2 bytes precede the 4-byte field):
//   FF 15 [disp32]  CALL *[rip+x]  →  67 E8 [disp32]  addr32 CALL rel32
//   FF 25 [disp32]  JMP  *[rip+x]  →  E9 [disp32] 90  JMP rel32 ; NOP
//
// R_X86_64_REX_GOTPCRELX (REX prefix; 3 bytes precede the 4-byte field):
//   REX 8B ModRM [disp32]  MOV reg,[rip+x]  →  REX 8D ModRM [disp32]  LEA
//
// In all cases the RelocGOTPCRel32 kind is changed to RelocRel32 so that
// relocate() patches the site with  symbolVA − (siteVA + 4)  instead of
// the GOT-indirect value.  The GOT slot previously allocated for the symbol
// is left filled with the symbol VA but is otherwise unused.
//
// Relaxation is skipped for any site where the instruction pattern does not
// match, preserving full correctness: the site will simply be patched via the
// GOT as if relaxation were disabled.
package linker

import "github.com/vertex-language/compiler/object"

// reGOTPCRel32 and reTLSGOTPCRel32 are local aliases to avoid the
// object package qualifier in the hot switch statements.
const (
	reGOTPCRel32    = object.RelocGOTPCRel32
	reTLSGOTPCRel32 = object.RelocTLSGOTPCRel32
)

// relax rewrites GOTPCRELX reloc sites in .text to direct PC-relative form
// where the instruction pattern is recognised.  Must run after fillGOT
// (so GOT slots are valid) and before relocate().
func (lnk *linker) relax() {
	for i := range lnk.relocs {
		r := &lnk.relocs[i]
		if r.kind != object.RelocGOTPCRel32 || r.section != secKindText {
			continue
		}
		off := r.codeOff
		if off < 3 || off+4 > len(lnk.code) {
			continue // not enough bytes to inspect
		}
		b    := lnk.code
		p1   := b[off-1] // byte immediately before the 4-byte field
		p2   := b[off-2] // two bytes before

		switch {
		// ── CALL *[rip+x]  →  addr32 CALL rel32 ────────────────────────────
		// Encoding before: FF 15 [disp32]
		// Encoding after:  67 E8 [disp32]   (total length unchanged: 6 bytes)
		case p2 == 0xff && p1 == 0x15:
			b[off-2] = 0x67
			b[off-1] = 0xe8
			r.kind = object.RelocRel32

		// ── JMP *[rip+x]  →  JMP rel32 ; NOP ────────────────────────────────
		// Encoding before: FF 25 [disp32]          (6 bytes)
		// Encoding after:  E9 [disp32] 90           (5 + 1 = 6 bytes)
		// The disp32 field shifts one byte left: new r.codeOff = off−1.
		case p2 == 0xff && p1 == 0x25:
			b[off-2] = 0xe9
			b[off-1] = 0x00 // first byte of the new disp32 (was opcode 0x25)
			// bytes [off..off+2] are already the remaining 3 disp bytes (all 0)
			b[off+3] = 0x90 // NOP fills the vacated 6th byte
			r.codeOff = off - 1
			r.kind = object.RelocRel32

		// ── REX MOV reg,[rip+x]  →  REX LEA reg,[rip+x] ────────────────────
		// Encoding before: REX 8B ModRM [disp32]   (7 bytes)
		// Encoding after:  REX 8D ModRM [disp32]   (7 bytes, opcode 8B→8D)
		// Works for any REX prefix (48–4F) and any ModRM byte.
		default:
			if off < 3 {
				continue
			}
			p3 := b[off-3]
			if p3&0xf0 == 0x40 && p2 == 0x8b {
				b[off-2] = 0x8d // MOV load → LEA (compute address directly)
				r.kind = object.RelocRel32
			}
		}
	}
}