// Package macho serialises 64-bit Mach-O executables, dylibs, bundles, and
// kext bundles for macOS and iOS (ARM64 and AMD64).
//
// The package mirrors the macOS ABI natively: segments, sections, dylib
// references, indirect-symbol stubs, rebase/bind opcode tables, export tries,
// chained fixups, build-version metadata, code-signature placeholders, and
// function-start tables are all first-class concepts here.
//
// Parsing and linking live elsewhere; this package only emits bytes.
package macho

// ──────────────────────────────────────────────────────────────────────────────
// Architecture
// ──────────────────────────────────────────────────────────────────────────────

// Arch identifies the target CPU (cpu_type_t | ABI64 bit).
type Arch uint32

const (
	ArchAMD64 Arch = (1 << 24) | 7  // CPU_TYPE_X86_64
	ArchARM64 Arch = (1 << 24) | 12 // CPU_TYPE_ARM64
)

// cpuSubtype returns the CPU subtype for the Arch.
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

// ──────────────────────────────────────────────────────────────────────────────
// File type
// ──────────────────────────────────────────────────────────────────────────────

// FileType is the Mach-O file type (mach_header_64.filetype).
type FileType uint32

const (
	FileTypeObject     FileType = 0x1 // MH_OBJECT      — relocatable object file
	FileTypeExecute    FileType = 0x2 // MH_EXECUTE     — demand-paged executable
	FileTypeCore       FileType = 0x4 // MH_CORE        — core dump
	FileTypeDylib      FileType = 0x6 // MH_DYLIB       — dynamic shared library (.dylib)
	FileTypeDylinker   FileType = 0x7 // MH_DYLINKER    — /usr/lib/dyld itself
	FileTypeBundle     FileType = 0x8 // MH_BUNDLE      — runtime-loadable plug-in (.bundle)
	FileTypeKextBundle FileType = 0xb // MH_KEXT_BUNDLE — kernel extension
)

// ──────────────────────────────────────────────────────────────────────────────
// Header flags
// ──────────────────────────────────────────────────────────────────────────────

// MHFlags are bitfield flags in mach_header_64.flags.
type MHFlags uint32

const (
	MHNoUndefs        MHFlags = 0x00000001 // MH_NOUNDEFS        — no undefined symbols
	MHIncrLink        MHFlags = 0x00000002 // MH_INCRLINK        — incremental link output
	MHDyldLink        MHFlags = 0x00000004 // MH_DYLDLINK        — input for dyld; cannot be statically linked again
	MHBindAtLoad      MHFlags = 0x00000008 // MH_BINDATLOAD      — bind all undefined symbols at load time
	MHPrebound        MHFlags = 0x00000010 // MH_PREBOUND        — prebound image (deprecated)
	MHSplitSegs       MHFlags = 0x00000020 // MH_SPLIT_SEGS      — read-only and read-write segs split
	MHTwoLevel        MHFlags = 0x00000080 // MH_TWOLEVEL        — two-level namespace bindings
	MHForceFlat       MHFlags = 0x00000100 // MH_FORCE_FLAT      — force flat namespace
	MHNoMultiDefs     MHFlags = 0x00000200 // MH_NOMULTIDEFS
	MHNoFixPrebinding MHFlags = 0x00000400 // MH_NOFIXPREBINDING
	MHPrebindable     MHFlags = 0x00000800 // MH_PREBINDABLE
	MHAllModsBound    MHFlags = 0x00001000 // MH_ALLMODSBOUND    — all two-level modules bound
	MHSubsectionsViaSymbols MHFlags = 0x00002000 // MH_SUBSECTIONS_VIA_SYMBOLS
	MHCanonical       MHFlags = 0x00004000 // MH_CANONICAL       — canonicalised via unprebind
	MHWeakDefines     MHFlags = 0x00008000 // MH_WEAK_DEFINES    — has external weak symbols
	MHBindsToWeak     MHFlags = 0x00010000 // MH_BINDS_TO_WEAK   — uses weak symbols
	MHAllowStackExec  MHFlags = 0x00020000 // MH_ALLOW_STACK_EXECUTION
	MHRootSafe        MHFlags = 0x00040000 // MH_ROOT_SAFE
	MHSetUIDSafe      MHFlags = 0x00080000 // MH_SETUID_SAFE
	MHNoReexportedDylibs MHFlags = 0x00100000 // MH_NO_REEXPORTED_DYLIBS
	MHPie             MHFlags = 0x00200000 // MH_PIE             — ASLR main executable
	MHDeadStrippableDylib MHFlags = 0x00400000 // MH_DEAD_STRIPPABLE_DYLIB
	MHHasTLVDescriptors MHFlags = 0x00800000 // MH_HAS_TLV_DESCRIPTORS
	MHNoHeapExecution MHFlags = 0x01000000 // MH_NO_HEAP_EXECUTION
	MHAppExtensionSafe MHFlags = 0x02000000 // MH_APP_EXTENSION_SAFE
	MHNlistOutofsyncWithDyldinfo MHFlags = 0x04000000 // MH_NLIST_OUTOFSYNC_WITH_DYLDINFO
	MHSimSupport      MHFlags = 0x08000000 // MH_SIM_SUPPORT     — built for simulator
	MHDylibInCache    MHFlags = 0x80000000 // MH_DYLIB_IN_CACHE  — in shared cache
)

// ──────────────────────────────────────────────────────────────────────────────
// Platform / build version
// ──────────────────────────────────────────────────────────────────────────────

// Platform identifies the target OS platform for LC_BUILD_VERSION.
type Platform uint32

const (
	PlatformMacOS           Platform = 1
	PlatformIOS             Platform = 2
	PlatformTVOS            Platform = 3
	PlatformWatchOS         Platform = 4
	PlatformBridgeOS        Platform = 5
	PlatformMacCatalyst     Platform = 6
	PlatformIOSSimulator    Platform = 7
	PlatformTVOSSimulator   Platform = 8
	PlatformWatchOSSimulator Platform = 9
	PlatformDriverKit       Platform = 10
	PlatformVisionOS        Platform = 11
	PlatformVisionSimulator Platform = 12
)

// Tool identifies a build tool reported in LC_BUILD_VERSION.
type Tool uint32

const (
	ToolClang Tool = 1
	ToolSwift Tool = 2
	ToolLD    Tool = 3
	ToolLLD   Tool = 4
)

// BuildToolVersion pairs a tool with a packed version number.
type BuildToolVersion struct {
	Tool    Tool
	Version uint32 // packed: bits 31:16 = major, 15:8 = minor, 7:0 = patch
}

// BuildVersion carries the data for LC_BUILD_VERSION.
// Use PackVersion to construct MinOS and SDK.
type BuildVersion struct {
	Platform Platform
	MinOS    uint32             // minimum OS version
	SDK      uint32             // SDK version
	Tools    []BuildToolVersion // optional — tool chain metadata
}

// PackVersion packs a major.minor.patch triple into the 32-bit Mach-O format
// (bits 31:16 = major, bits 15:8 = minor, bits 7:0 = patch).
func PackVersion(major, minor, patch uint16) uint32 {
	return uint32(major)<<16 | uint32(minor)<<8 | uint32(patch)
}

// PackSourceVersion packs an A.B.C.D.E source version into the 40-bit Mach-O
// representation returned as uint64 (bits 39:26=A, 25:20=B, 19:14=C, 13:7=D, 6:0=E).
func PackSourceVersion(a uint32, b, c, d, e uint8) uint64 {
	return uint64(a)<<26 | uint64(b)<<20 | uint64(c)<<14 | uint64(d)<<7 | uint64(e)
}

// ──────────────────────────────────────────────────────────────────────────────
// VM protection
// ──────────────────────────────────────────────────────────────────────────────

// Prot is a VM protection bitmask (vm_prot_t).
type Prot uint32

const (
	ProtNone  Prot = 0x00 // VM_PROT_NONE
	ProtRead  Prot = 0x01 // VM_PROT_READ
	ProtWrite Prot = 0x02 // VM_PROT_WRITE
	ProtExec  Prot = 0x04 // VM_PROT_EXECUTE
)

// ──────────────────────────────────────────────────────────────────────────────
// Segment flags
// ──────────────────────────────────────────────────────────────────────────────

// SegFlags are segment_command_64.flags bits.
type SegFlags uint32

const (
	SegHighVM   SegFlags = 0x1 // SG_HIGHVM    — file contents at high VM addresses
	SegFVMLib   SegFlags = 0x2 // SG_FVMLIB    — fixed VM library segment
	SegNoReloc  SegFlags = 0x4 // SG_NORELOC   — no relocs in segment
	SegReadOnly SegFlags = 0x8 // SG_READ_ONLY — dyld will mprotect to r/o after init (__DATA_CONST)
)

// ──────────────────────────────────────────────────────────────────────────────
// Section types and attributes
// ──────────────────────────────────────────────────────────────────────────────

// Section type constants (low byte of section_64.flags).
const (
	S_REGULAR                             uint32 = 0x00 // plain code or data
	S_ZEROFILL                            uint32 = 0x01 // zero-fill on demand (BSS)
	S_CSTRING_LITERALS                    uint32 = 0x02 // NUL-terminated string literals
	S_4BYTE_LITERALS                      uint32 = 0x03 // 4-byte literals
	S_8BYTE_LITERALS                      uint32 = 0x04 // 8-byte literals
	S_LITERAL_POINTERS                    uint32 = 0x05 // pointers to literals
	S_NON_LAZY_SYMBOL_POINTERS            uint32 = 0x06 // __got, __nl_symbol_ptr
	S_LAZY_SYMBOL_POINTERS                uint32 = 0x07 // __la_symbol_ptr
	S_SYMBOL_STUBS                        uint32 = 0x08 // __stubs
	S_MOD_INIT_FUNC_POINTERS              uint32 = 0x09 // __mod_init_func (constructors)
	S_MOD_TERM_FUNC_POINTERS              uint32 = 0x0a // __mod_term_func (destructors)
	S_COALESCED                           uint32 = 0x0b // coalesced symbols
	S_GB_ZEROFILL                         uint32 = 0x0c // >4 GB zero-fill
	S_INTERPOSING                         uint32 = 0x0d // pairs of (impl, stub) for interposing
	S_16BYTE_LITERALS                     uint32 = 0x0e // 16-byte literals
	S_DTRACE_DOT_OBJECT                   uint32 = 0x0f // DTrace probe site
	S_LAZY_DYLIB_SYMBOL_POINTERS          uint32 = 0x10 // lazy symbol pointers for lazy loaded dylibs
	S_THREAD_LOCAL_REGULAR                uint32 = 0x11 // TLS template data (__thread_data)
	S_THREAD_LOCAL_ZEROFILL               uint32 = 0x12 // TLS zero-fill template (__thread_bss)
	S_THREAD_LOCAL_VARIABLES              uint32 = 0x13 // TLS variable descriptors (__thread_vars)
	S_THREAD_LOCAL_VARIABLE_POINTERS      uint32 = 0x14 // TLS variable pointers
	S_THREAD_LOCAL_INIT_FUNCTION_POINTERS uint32 = 0x15 // TLS init functions
	S_INIT_FUNC_OFFSETS                   uint32 = 0x16 // function offset table for init
)

// Section attribute constants (upper 3 bytes of section_64.flags).
const (
	S_ATTR_PURE_INSTRUCTIONS   uint32 = 0x80000000 // section contains only machine instructions
	S_ATTR_NO_TOC              uint32 = 0x40000000 // do not use table of contents
	S_ATTR_STRIP_STATIC_SYMS   uint32 = 0x20000000 // ok to strip static symbols in section
	S_ATTR_NO_DEAD_STRIP       uint32 = 0x10000000 // must not be dead-stripped
	S_ATTR_LIVE_SUPPORT        uint32 = 0x08000000 // live blocks may reference this section
	S_ATTR_SELF_MODIFYING_CODE uint32 = 0x04000000 // used with i386 jump tables
	S_ATTR_DEBUG               uint32 = 0x02000000 // debug section (DWARF)
	S_ATTR_SOME_INSTRUCTIONS   uint32 = 0x00000400 // section contains some machine code
	S_ATTR_EXT_RELOC           uint32 = 0x00000200 // has external relocation entries
	S_ATTR_LOC_RELOC           uint32 = 0x00000100 // has local relocation entries
)

// ──────────────────────────────────────────────────────────────────────────────
// Core data structures
// ──────────────────────────────────────────────────────────────────────────────

// Section is one section within a Segment.
//
// Reserved1 and Reserved2 have section-type-specific meanings:
//   - S_SYMBOL_STUBS: Reserved1 = first indirect-symbol-table index;
//     Reserved2 = byte size of one stub entry.
//   - S_NON_LAZY_SYMBOL_POINTERS / S_LAZY_SYMBOL_POINTERS:
//     Reserved1 = first indirect-symbol-table index; Reserved2 = 0.
//   - All other types: both should be 0.
type Section struct {
	// Name is the section name, e.g. "__text", "__data".  Truncated to 16 bytes.
	Name string
	// Data is the raw content.  Nil/empty for S_ZEROFILL sections.
	Data []byte
	// Size is used for S_ZEROFILL / S_GB_ZEROFILL sections only;
	// ignored when Data is non-empty.
	Size uint64
	// Align is the required alignment as a byte count (power of two).  0 → 1.
	Align uint32
	// Flags encodes the section type (low byte) and attributes (upper 3 bytes).
	Flags uint32
	// Reserved1: indirect symbol table index (stub/pointer sections).
	Reserved1 uint32
	// Reserved2: stub entry size (S_SYMBOL_STUBS only).
	Reserved2 uint32
	// Relocs holds arch-specific relocation records.
	// Relevant only for MH_OBJECT output or when not using the linker package.
	Relocs []Reloc
}

// Segment maps a contiguous range of sections into the task address space.
//
// Standard macOS segments and their conventional protections:
//
//	__TEXT      ProtRead|ProtExec    (code, read-only data, Mach-O header)
//	__DATA_CONST ProtRead|ProtWrite  (r/w during init, then locked r/o via SegReadOnly)
//	__DATA      ProtRead|ProtWrite   (globals, writable data)
//	__OBJC      ProtRead|ProtWrite   (Objective-C runtime metadata)
//
// __PAGEZERO and __LINKEDIT are synthesised automatically by the Builder.
type Segment struct {
	// Name is the segment name.  Truncated to 16 bytes.
	Name string
	// InitProt is the initial VM protection.
	InitProt Prot
	// MaxProt is the maximum VM protection.  If zero, defaults to InitProt.
	MaxProt Prot
	// Flags are segment_command_64 flags (SG_*).
	Flags SegFlags
	// Sections holds the ordered sections within this segment.
	Sections []Section
}

// DylibKind determines which LC_LOAD_* command is emitted.
type DylibKind uint8

const (
	DylibLoad     DylibKind = iota // LC_LOAD_DYLIB       — required at startup
	DylibWeak                      // LC_LOAD_WEAK_DYLIB  — optional (missing dylib is ok)
	DylibReexport                  // LC_REEXPORT_DYLIB   — re-export all symbols upstream
	DylibLazy                      // LC_LAZY_LOAD_DYLIB  — defer load until first symbol use
	DylibUpward                    // LC_LOAD_UPWARD_DYLIB — used in umbrella frameworks
)

// DylibRef describes a dynamic library dependency or identity.
type DylibRef struct {
	// Path is the dylib install name, e.g. "/usr/lib/libSystem.B.dylib"
	// or "@rpath/libFoo.dylib".
	Path string
	// Kind selects the load command type.
	Kind DylibKind
	// CurrentVersion and CompatVersion are packed x.y.z version numbers
	// (use PackVersion to construct them).
	CurrentVersion uint32
	CompatVersion  uint32
}

// Symbol is an nlist_64 symbol table entry.
type Symbol struct {
	// Name is the mangled symbol name, e.g. "_main", "__ZN3Foo3barEv".
	Name string
	// SegmentName and SectionName identify the home section; if both are "",
	// the symbol is treated as N_UNDF (undefined / external reference).
	SegmentName string
	SectionName string
	// Value is the symbol value.  For defined symbols this is the VA offset
	// from the section start.  The Builder resolves it to a final VA.
	Value uint64
	// Global marks the symbol N_EXT (externally visible).
	Global bool
	// Weak sets N_WEAK_DEF (for defined) or N_WEAK_REF (for undefined).
	Weak bool
	// AltEntry sets N_ALT_ENTRY (alternate entry point, e.g. Swift thunks).
	AltEntry bool
	// PrivateExtern sets N_PEXT (private external — module-scoped visibility).
	PrivateExtern bool
	// Desc is the raw n_desc value; the Builder ORs in flag bits it computes.
	Desc uint16
}

// Reloc is a relocation record.  The Type field is arch-specific; use
// RelocTypeARM64 or RelocTypeAMD64 constants and cast to uint8.
type Reloc struct {
	// Section is the section name that contains the address to patch.
	Section string
	// Offset is the byte offset within Section of the location to patch.
	Offset uint32
	// Symbol is the target symbol name.
	Symbol string
	// Type is an arch-specific relocation type constant (cast to uint8).
	Type uint8
	// Length: 0=1-byte, 1=2-byte, 2=4-byte, 3=8-byte.
	Length uint8
	// PCRel indicates PC-relative addressing.
	PCRel bool
	// Extern: true = symbol field is a symbol table index; false = section number.
	Extern bool
}

// DataInCodeEntry marks a range of non-instruction bytes inside a code section.
// Emitted via LC_DATA_IN_CODE into __LINKEDIT.
type DataInCodeEntry struct {
	// Offset is the byte offset from the start of __TEXT to the data range.
	Offset uint32
	// Length is the number of bytes.
	Length uint16
	// Kind is a DICE_KIND_* constant.
	Kind DataInCodeKind
}

// DataInCodeKind classifies embedded data in code sections.
type DataInCodeKind uint16

const (
	DiceKindData        DataInCodeKind = 0x0001 // DICE_KIND_DATA
	DiceKindJumpTable8  DataInCodeKind = 0x0002 // DICE_KIND_JUMP_TABLE8
	DiceKindJumpTable16 DataInCodeKind = 0x0003 // DICE_KIND_JUMP_TABLE16
	DiceKindJumpTable32 DataInCodeKind = 0x0004 // DICE_KIND_JUMP_TABLE32
	DiceKindAbsJumpTable32 DataInCodeKind = 0x0005 // DICE_KIND_ABS_JUMP_TABLE32
)