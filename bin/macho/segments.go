// Package macho serialises 64-bit Mach-O executables and dylibs.
// It covers the segment/section model, symbol and string tables, and the
// minimal set of load commands required to produce a runnable binary.
// Relocations are only needed when managing cross-section references
// yourself; if the binary came through the linker package, all relocations
// are already applied.
package macho

// Arch identifies the target CPU.
type Arch uint32

const (
	ArchAMD64 Arch = (1 << 24) | 7  // CPU_TYPE_X86_64
	ArchARM64 Arch = (1 << 24) | 12 // CPU_TYPE_ARM64
)

// cpuSubtype returns the appropriate CPU subtype for a given Arch.
func (a Arch) cpuSubtype() uint32 {
	switch a {
	case ArchAMD64:
		return 3 // CPU_SUBTYPE_X86_64_ALL
	case ArchARM64:
		return 0 // CPU_SUBTYPE_ARM64_ALL
	default:
		return 0
	}
}

// VM protection flags (PROT_*).
type Prot uint32

const (
	ProtRead  Prot = 0x01 // VM_PROT_READ
	ProtWrite Prot = 0x02 // VM_PROT_WRITE
	ProtExec  Prot = 0x04 // VM_PROT_EXECUTE
)

// Section flags (section type in low byte, attributes in upper 3 bytes).
const (
	// Section types (low byte of Flags).
	S_REGULAR                  uint32 = 0x0
	S_ZEROFILL                 uint32 = 0x1
	S_CSTRING_LITERALS         uint32 = 0x2
	S_NON_LAZY_SYMBOL_POINTERS uint32 = 0x6
	S_LAZY_SYMBOL_POINTERS     uint32 = 0x7
	S_SYMBOL_STUBS             uint32 = 0x8
	S_MOD_INIT_FUNC_POINTERS   uint32 = 0x9
	S_MOD_TERM_FUNC_POINTERS   uint32 = 0xa

	// Section attributes (upper 3 bytes).
	S_ATTR_PURE_INSTRUCTIONS uint32 = 0x80000000
	S_ATTR_SOME_INSTRUCTIONS uint32 = 0x00000400
	S_ATTR_DEBUG             uint32 = 0x02000000
)

// Section is one section within a Segment.
// Sections inherit the VM protection of their containing Segment.
// Align is expressed as a byte count (e.g. 4, 8, 16) and must be a power of two.
type Section struct {
	// Name is the section name, e.g. "__text", "__data".
	// Truncated to 16 bytes when serialised.
	Name string
	// Data is the raw content of the section.
	Data []byte
	// Align is the required alignment of the section start (byte count, power of two).
	// If zero, defaults to 1.
	Align uint32
	// Flags contains section type and attribute flags (S_* constants).
	// If zero, defaults to S_REGULAR.
	Flags uint32
	// Relocs holds any relocations for this section.
	// Only relevant when bin is used directly without the linker package.
	Relocs []Reloc
}

// Segment maps a contiguous range of section data into the task address space.
// In a typical executable you have at minimum __TEXT (code, read+exec) and
// __DATA (globals, read+write). __LINKEDIT is synthesised automatically by
// the Builder from the symbol and string tables.
type Segment struct {
	// Name is the segment name, e.g. "__TEXT", "__DATA".
	// Truncated to 16 bytes when serialised.
	Name string
	// Prot is the initial VM protection (and used as maxprot too).
	Prot Prot
	// Sections holds the ordered sections within this segment.
	Sections []Section
}

// Symbol is a Mach-O symbol table entry (maps to nlist_64).
type Symbol struct {
	// Name is the symbol name, e.g. "_main".
	Name string
	// Section is the 1-based section index the symbol lives in, or 0 for NO_SECT.
	// The Builder computes this from SectionName and SegmentName if those are set.
	SectionName string
	SegmentName string
	// Value is the symbol value (usually a virtual address or section offset).
	Value uint64
	// Global marks the symbol as externally visible (N_EXT).
	Global bool
}

// Reloc is a relocation record for a section.
// Format constants live in reloc_arm64.go (ARM64) and are arch-specific.
type Reloc struct {
	// Section is the name of the section that contains the reference.
	Section string
	// Offset is the byte offset within that section of the location to patch.
	Offset uint32
	// Symbol is the target symbol name.
	Symbol string
	// Type is the arch-specific relocation type constant.
	Type uint8
	// Length encodes the relocation size: 0=byte, 1=2-byte, 2=4-byte, 3=8-byte.
	Length uint8
	// PCRel indicates a PC-relative relocation.
	PCRel bool
	// Extern, when true, means the symbol field references the symbol table;
	// when false it references a section number.
	Extern bool
}

// DylibRef describes a dynamic library to be loaded at runtime (LC_LOAD_DYLIB).
type DylibRef struct {
	// Path is the install name of the dylib, e.g. "/usr/lib/libSystem.B.dylib".
	Path string
	// CurrentVersion and CompatVersion are packed 32-bit x.y.z version numbers
	// (bits 31:16 major, bits 15:8 minor, bits 7:0 patch).
	CurrentVersion uint32
	CompatVersion  uint32
}