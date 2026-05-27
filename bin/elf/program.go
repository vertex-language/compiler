// program.go
package elf

// Segment describes a custom program header injected via Builder.AddSegment.
//
// The following program headers are synthesized automatically by the emitter
// and must not be added via AddSegment:
//
//   PT_PHDR       — program header table itself
//   PT_INTERP     — set via Builder.SetInterp
//   PT_LOAD       — one per distinct permission group of SHF_ALLOC sections
//   PT_DYNAMIC    — when dynamic linking is configured
//   PT_TLS        — when any SHF_TLS section is present
//   PT_GNU_STACK  — always emitted (marks the stack non-executable)
//
// Use AddSegment for everything else: PT_GNU_RELRO, PT_NOTE,
// PT_GNU_EH_FRAME, PT_GNU_PROPERTY, and any application-specific headers.
type Segment struct {
	// Type is the PT_* constant.
	Type uint32

	// Flags is the PF_R | PF_W | PF_X permission bitmask.
	Flags uint32

	// Align is the segment alignment. 0 is treated as 1.
	Align uint64

	// Sections lists the names of sections whose extent defines this
	// segment's VAddr, FileOffset, FileSize, and MemSize. Leave nil for
	// segments that carry no section data (PT_GNU_STACK, PT_NOTE pointing
	// at a manually-laid-out note, etc.) — those are emitted as-is with
	// zero file extent.
	Sections []string
}

// AddSegment appends a custom program header to the binary. AddSegment must
// be called before Emit.
func (b *Builder) AddSegment(seg Segment) {
	b.extraSegments = append(b.extraSegments, seg)
}