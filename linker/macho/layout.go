package macho

const (
	// pageSize is the VM page / segment alignment (16 KiB — ARM64 native page,
	// also accepted by macOS x86-64).
	pageSize uint64 = 0x4000

	// execBaseVA is the conventional load address for the first user segment
	// on macOS.  __PAGEZERO occupies [0, execBaseVA).
	execBaseVA uint64 = 0x100000000

	// dylibBaseVA is the base VA for dylib / bundle output (position-independent).
	dylibBaseVA uint64 = 0x0
)

// AssignLayout assigns virtual addresses and file offsets to every
// MergedSection in the layout.  The first PT_LOAD segment starts at
// execBaseVA for OutputExec, or dylibBaseVA for OutputDylib/Bundle.
func AssignLayout(outputType OutputType, layout *Layout) error {
	var base uint64
	switch outputType {
	case OutputExec:
		base = execBaseVA
	default:
		base = dylibBaseVA
	}

	// File begins with the Mach-O header + load commands, which is placed by
	// bin/macho.  We start section data after a full page.
	// The Builder handles actual header placement; we assign starting from
	// pageSize so the bin/macho Builder's own page alignment aligns correctly.
	fileOff := pageSize
	vmAddr := base

	for _, seg := range layout.Segments {
		// Each segment is page-aligned.
		fileOff = alignUp64(fileOff, pageSize)
		vmAddr = alignUp64(vmAddr, pageSize)

		seg.VMAddr = vmAddr
		seg.FileOff = fileOff
		segFileStart := fileOff

		for _, ms := range seg.Sections {
			a := uint64(ms.Align)
			if a < 1 {
				a = 1
			}
			fileOff = alignUp64(fileOff, a)
			vmAddr = alignUp64(vmAddr, a)

			ms.VAddr = vmAddr
			ms.FileOffset = fileOff

			if !ms.IsZerofill() {
				fileOff += ms.Size
			}
			vmAddr += ms.Size
		}

		seg.VMSize = alignUp64(vmAddr-seg.VMAddr, pageSize)
		seg.FileSize = fileOff - segFileStart
	}

	return nil
}