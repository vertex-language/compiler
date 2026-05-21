// Package linker — tls.go
// TLS layout: computes TP-relative offsets and fills TLS IE GOT slots.
//
// x86-64 uses TLS variant 2 (glibc / System V ABI):
//   • The thread pointer (%fs:0) points to the Thread Control Block (TCB).
//   • The main executable's static TLS block is placed immediately BEFORE
//     the TCB in each thread's address space.
//   • Consequently every TP offset for the main executable is negative:
//
//       tpoff(sym) = sym_tls_template_offset − alignUp(totalTLSSize, 16)
//
// The GOT TLS IE slot for symbol S holds tpoff(S).  The CPU instruction
//   mov sym@gottpoff(%rip), %rax    ; load tpoff(sym) from GOT
//   add %fs:0, %rax                  ; rax = &sym for this thread
// then accesses the per-thread copy of S.
package linker

import "fmt"

const tlsBlockAlign = uint64(16) // x86-64 TLS block alignment

// layoutTLS computes tpoff for every TLS symbol and writes it into the
// corresponding TLS IE GOT slot.  Must be called after all objects are
// ingested (so lnk.tdata and lnk.tbss are final) but before computeLayout.
func (lnk *linker) layoutTLS() error {
	tdataSize := uint64(len(lnk.tdata))
	tbssSize  := lnk.tbss
	if tdataSize == 0 && tbssSize == 0 {
		return nil
	}
	alignedBlock := alignUp(tdataSize+tbssSize, tlsBlockAlign)

	for name, tlsOff := range lnk.sym.tlsDefs {
		tpoff := int64(uint64(tlsOff)) - int64(alignedBlock)
		slot  := lnk.gotTable.tlsSlotFor(name)
		lnk.gotTable.set(slot, uint64(tpoff))
	}
	return nil
}

// buildGOT scans all pending relocation sites and allocates GOT slots for
// every symbol that needs one.  Must be called after all ingestion, before
// computeLayout (so the GOT size is finalised before VAs are assigned).
func (lnk *linker) buildGOT() error {
	for _, r := range lnk.relocs {
		switch r.kind {
		case reGOTPCRel32:
			lnk.gotTable.slotFor(r.symbol)
		case reTLSGOTPCRel32:
			if _, ok := lnk.sym.tlsDefs[r.symbol]; !ok {
				// Symbol might be resolved later (archive); skip for now.
				// If still unresolved, checkResolved will catch it.
				lnk.gotTable.tlsSlotFor(r.symbol)
			} else {
				lnk.gotTable.tlsSlotFor(r.symbol)
			}
		}
	}
	return nil
}

// fillGOT writes the absolute VA of each regular symbol into its GOT slot.
// Must be called after computeLayout (so lnk.lay has all VAs).
func (lnk *linker) fillGOT() error {
	var firstErr error
	lnk.gotTable.forEachReg(func(name string, idx int) {
		if firstErr != nil {
			return
		}
		va, ok := lnk.resolveSymbolVA(name)
		if !ok {
			firstErr = fmt.Errorf("linker: fillGOT: cannot resolve VA for %q", name)
			return
		}
		lnk.gotTable.set(idx, va)
	})
	return firstErr
}