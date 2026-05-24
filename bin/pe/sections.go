// Package pe constructs and emits PE32+ executable images.
// It supports static and dynamic executables, DLLs, the Import Address Table,
// the export table, base relocations, and a debug directory passthrough.
//
// For simple self-contained programs use [NewBuilder] directly.
// When coming from the linker package, use the package-level [Emit] function
// which accepts a [PEImage] with all relocations already applied.
package pe

// Arch identifies the target CPU architecture encoded in the COFF file header.
type Arch uint16

const (
	// ArchAMD64 targets the x86-64 architecture (IMAGE_FILE_MACHINE_AMD64).
	ArchAMD64 Arch = 0x8664
	// ArchARM64 targets the AArch64 architecture (IMAGE_FILE_MACHINE_ARM64).
	ArchARM64 Arch = 0xAA64
)

// Subsystem identifies the Windows subsystem required by the image
// (the IMAGE_OPTIONAL_HEADER Subsystem field).
type Subsystem uint16

const (
	SubsystemUnknown        Subsystem = 0  // IMAGE_SUBSYSTEM_UNKNOWN
	SubsystemNative         Subsystem = 1  // IMAGE_SUBSYSTEM_NATIVE
	SubsystemWindows        Subsystem = 2  // IMAGE_SUBSYSTEM_WINDOWS_GUI
	SubsystemConsole        Subsystem = 3  // IMAGE_SUBSYSTEM_WINDOWS_CUI
	SubsystemEFIApplication Subsystem = 10 // IMAGE_SUBSYSTEM_EFI_APPLICATION
	SubsystemEFIBootService Subsystem = 11 // IMAGE_SUBSYSTEM_EFI_BOOT_SERVICE_DRIVER
	SubsystemEFIRuntime     Subsystem = 12 // IMAGE_SUBSYSTEM_EFI_RUNTIME_DRIVER
	SubsystemXbox           Subsystem = 14 // IMAGE_SUBSYSTEM_XBOX
	SubsystemBootApp        Subsystem = 16 // IMAGE_SUBSYSTEM_WINDOWS_BOOT_APPLICATION
)

// COFF IMAGE_FILE_* characteristic flags (Characteristics field of the file header).
const (
	IMAGE_FILE_RELOCS_STRIPPED     = uint16(0x0001) // No base relocations present.
	IMAGE_FILE_EXECUTABLE_IMAGE    = uint16(0x0002) // Image is valid and runnable.
	IMAGE_FILE_LINE_NUMS_STRIPPED  = uint16(0x0004) // COFF line numbers removed (deprecated).
	IMAGE_FILE_LOCAL_SYMS_STRIPPED = uint16(0x0008) // COFF local symbols removed (deprecated).
	IMAGE_FILE_LARGE_ADDRESS_AWARE = uint16(0x0020) // Application can handle addresses > 2 GiB.
	IMAGE_FILE_32BIT_MACHINE       = uint16(0x0100) // 32-bit word machine.
	IMAGE_FILE_DEBUG_STRIPPED      = uint16(0x0200) // Debug info removed.
	IMAGE_FILE_SYSTEM              = uint16(0x1000) // System file, not a user program.
	IMAGE_FILE_DLL                 = uint16(0x2000) // File is a dynamic-link library.
)

// IMAGE_DLLCHARACTERISTICS_* flags (DllCharacteristics field of the optional header).
const (
	IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA       = uint16(0x0020) // Image can handle 64-bit ASLR VA space.
	IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE          = uint16(0x0040) // Image can be relocated at load time (ASLR).
	IMAGE_DLLCHARACTERISTICS_FORCE_INTEGRITY       = uint16(0x0080) // Code integrity checks enforced.
	IMAGE_DLLCHARACTERISTICS_NX_COMPAT             = uint16(0x0100) // Image is NX (DEP) compatible.
	IMAGE_DLLCHARACTERISTICS_NO_ISOLATION          = uint16(0x0200) // Isolation-aware but do not isolate.
	IMAGE_DLLCHARACTERISTICS_NO_SEH                = uint16(0x0400) // No structured exception handling.
	IMAGE_DLLCHARACTERISTICS_NO_BIND               = uint16(0x0800) // Do not bind the image.
	IMAGE_DLLCHARACTERISTICS_APPCONTAINER          = uint16(0x1000) // Must run in an AppContainer.
	IMAGE_DLLCHARACTERISTICS_WDM_DRIVER            = uint16(0x2000) // WDM driver.
	IMAGE_DLLCHARACTERISTICS_GUARD_CF              = uint16(0x4000) // Supports Control Flow Guard.
	IMAGE_DLLCHARACTERISTICS_TERMINAL_SERVER_AWARE = uint16(0x8000) // Terminal Server aware.
)

// IMAGE_SCN_* section characteristic flags (Characteristics field of a section header).
const (
	IMAGE_SCN_CNT_CODE               = uint32(0x00000020) // Section contains executable code.
	IMAGE_SCN_CNT_INITIALIZED_DATA   = uint32(0x00000040) // Section contains initialized data.
	IMAGE_SCN_CNT_UNINITIALIZED_DATA = uint32(0x00000080) // Section contains uninitialized data (BSS).
	IMAGE_SCN_LNK_INFO               = uint32(0x00000200) // Section contains comments or other info.
	IMAGE_SCN_LNK_REMOVE             = uint32(0x00000800) // Section will not appear in the image.
	IMAGE_SCN_LNK_COMDAT             = uint32(0x00001000) // Section contains COMDAT data.
	IMAGE_SCN_GPREL                  = uint32(0x00008000) // Data referenced via the global pointer (GP).
	IMAGE_SCN_MEM_DISCARDABLE        = uint32(0x02000000) // Section can be discarded (e.g. .reloc after loading).
	IMAGE_SCN_MEM_NOT_CACHED         = uint32(0x04000000) // Section cannot be cached.
	IMAGE_SCN_MEM_NOT_PAGED          = uint32(0x08000000) // Section is not pageable.
	IMAGE_SCN_MEM_SHARED             = uint32(0x10000000) // Section is shared in memory.
	IMAGE_SCN_MEM_EXECUTE            = uint32(0x20000000) // Section can be executed as code.
	IMAGE_SCN_MEM_READ               = uint32(0x40000000) // Section can be read.
	IMAGE_SCN_MEM_WRITE              = uint32(0x80000000) // Section can be written.
)

// Base relocation types stored in the high 4 bits of each .reloc entry word.
const (
	IMAGE_REL_BASED_ABSOLUTE = uint8(0)  // Padding; no fixup performed.
	IMAGE_REL_BASED_HIGHLOW  = uint8(3)  // Add the load delta to a 32-bit field.
	IMAGE_REL_BASED_DIR64    = uint8(10) // Add the load delta to a 64-bit field.
)

// Data directory indices within the optional header.
const (
	dataDirExport      = 0
	dataDirImport      = 1
	dataDirResource    = 2
	dataDirException   = 3
	dataDirSecurity    = 4 // Note: file offset, not RVA, per spec.
	dataDirBaseReloc   = 5
	dataDirDebug       = 6
	dataDirGlobalPtr   = 8
	dataDirTLS         = 9
	dataDirLoadConfig  = 10
	dataDirBoundImport = 11
	dataDirIAT         = 12
	dataDirDelayImport = 13
	dataDirCLR         = 14
	numDataDirectories = 16
)

// DataDirectory holds the virtual address and byte size of a data directory entry.
type DataDirectory struct {
	VirtualAddress uint32
	Size           uint32
}

// Section describes a PE section contributed to the image.
type Section struct {
	// Name is the section name. Longer than 8 bytes will be truncated;
	// executable images do not support string-table name references.
	Name string

	// Chars is the IMAGE_SCN_* characteristics bitmask.
	Chars uint32

	// Data contains the initialized bytes of the section.
	// May be nil for BSS-style sections (uninitialised data only).
	Data []byte

	// VirtualSize overrides the in-memory byte count when it differs from
	// len(Data). If zero, len(Data) is used. BSS sections set VirtualSize > 0
	// with nil or empty Data.
	VirtualSize uint32

	// Align is the required in-memory alignment in bytes. Zero defaults to 16.
	// Must be a power of two. Stored in the section header only; the file
	// alignment of raw data is always governed by the FileAlignment constant.
	Align uint32
}

// Symbol names an address within a section of the image.
type Symbol struct {
	// Name is the symbol name.
	Name string

	// Section is the name of the section that contains this symbol.
	Section string

	// Offset is the byte offset from the start of the section.
	Offset uint32

	// Global marks the symbol as visible outside the image.
	Global bool
}

// Reloc is a COFF-style relocation to be applied to section data at emit time.
//
// Relocs are only needed when using bin/pe directly without the linker package.
// When a PEImage comes from linker/pe, all relocations are already patched into
// the section data and no Reloc records are required.
type Reloc struct {
	// Section is the name of the section containing the patch site.
	Section string

	// Offset is the byte offset within Section where the patch is applied.
	Offset uint32

	// Symbol is the name of the target symbol.
	Symbol string

	// Type is the IMAGE_REL_AMD64_* or IMAGE_REL_ARM64_* relocation type.
	Type uint16
}

// ImportSymbol is a single function or data item imported from a DLL.
type ImportSymbol struct {
	// Name is the function name for a name-based import.
	// Set to empty string to import by ordinal.
	Name string

	// Ordinal is used when Name is empty.
	Ordinal uint16

	// Hint is the export-name-pointer-table index hint used for fast
	// name lookups. Zero is always acceptable.
	Hint uint16
}

// Import describes all symbols imported from a single DLL.
type Import struct {
	// DLL is the case-insensitive name of the DLL (e.g. "kernel32.dll").
	DLL string

	// Symbols lists every function or datum imported from DLL, in the
	// order they will appear in the IAT.
	Symbols []ImportSymbol
}

// Export describes a symbol to be exported from the image.
// Exports are only meaningful when the Builder is in DLL mode (see SetDLL).
type Export struct {
	// Name is the export name. Empty means an unnamed (ordinal-only) export.
	Name string

	// Symbol is the internal symbol whose address is exported.
	// It must name a symbol added via AddSymbol.
	Symbol string

	// Ordinal is the export ordinal. The export address table is indexed by
	// (Ordinal - OrdinalBase), where OrdinalBase is the minimum ordinal.
	Ordinal uint16
}

// DebugData is an opaque payload for the image debug directory.
// The builder wraps it in a single IMAGE_DEBUG_DIRECTORY entry.
type DebugData struct {
	// Type is the IMAGE_DEBUG_TYPE_* constant (e.g. 2 = CodeView).
	Type uint32

	// Data is the raw debug record bytes.
	Data []byte
}

// PEImage is the fully resolved, patched image produced by the linker package
// (linker/pe) and consumed by the package-level [Emit] function.
//
// All virtual addresses have been assigned, all relocations applied, and all
// format-specific structures built before PEImage is constructed. [Emit]
// serializes it directly with no further symbol resolution.
type PEImage struct {
	Arch      Arch
	Subsystem Subsystem
	ImageBase uint64 // 0 ⟹ use architecture default.

	// Sections carries the fully laid-out, patched section data.
	// VirtualAddress and VirtualSize must already be set by the linker.
	Sections []PESection

	// DataDirs holds the 16 data directory entries for the optional header.
	DataDirs [numDataDirectories]DataDirectory

	// EntryRVA is the AddressOfEntryPoint value (image-relative).
	// Zero for DLLs with no explicit entry.
	EntryRVA uint32

	StackReserve uint64 // 0 ⟹ 1 MiB.
	StackCommit  uint64 // 0 ⟹ 4 KiB.
	HeapReserve  uint64 // 0 ⟹ 1 MiB.
	HeapCommit   uint64 // 0 ⟹ 4 KiB.

	DllCharacteristics  uint16 // IMAGE_DLLCHARACTERISTICS_* flags.
	FileCharacteristics uint16 // Additional IMAGE_FILE_* flags ORed in.
	IsDLL               bool
}

// PESection is a single fully-built section inside a [PEImage].
type PESection struct {
	// Name is the section name (up to 8 bytes).
	Name string

	// Chars is the IMAGE_SCN_* characteristics bitmask.
	Chars uint32

	// VirtualAddress is the section's RVA when loaded (image-relative).
	VirtualAddress uint32

	// VirtualSize is the in-memory byte count of the section.
	VirtualSize uint32

	// Data is the file-aligned raw data. len(Data) must be a multiple of
	// FileAlignment (512). May be shorter than VirtualSize (zero-fill implied).
	Data []byte
}