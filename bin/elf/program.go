package elf

// Segment is the user-facing description of a program header. The Builder
// infers LOAD segments automatically; callers use Segment only to inject
// custom program headers (PT_TLS, PT_NOTE, PT_GNU_EH_FRAME, …) via
// Builder.AddSegment before Emit is called.
type Segment struct {
	// Type is the PT_* constant.
	Type uint32

	// Flags is the PF_R | PF_W | PF_X permission bitmask.
	Flags uint32

	// Align overrides the default segment alignment.
	// 0 → page-aligned (0x1000) for PT_LOAD, 1 for metadata segments.
	Align uint64

	// Sections lists the names of sections that belong to this segment.
	// Leave nil for segments that carry no section data (PT_GNU_STACK, …).
	Sections []string

	// The following fields are populated by the emitter during layout
	// and should be left zero by the caller.
	FileOffset uint64
	VAddr      uint64
	FileSize   uint64
	MemSize    uint64
}

// AddSegment appends a custom program header to the binary. AddSegment must
// be called before Emit. The emitter fills in VAddr, FileOffset, FileSize,
// and MemSize from the named sections; custom segments with no sections are
// passed through as-is (useful for PT_GNU_STACK, PT_NOTE, etc.).
func (b *Builder) AddSegment(seg Segment) {
	b.extraSegments = append(b.extraSegments, seg)
}