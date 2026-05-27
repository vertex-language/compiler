// Package pe constructs and emits PE32+ executable images and COFF object files
// for Windows targets. It supports static and dynamic executables, DLLs, COFF
// object files, Windows .lib archives, the Import Address Table, export table,
// delay-load imports, exception unwinding (.pdata/.xdata), thread-local storage,
// Control Flow Guard load configuration, base relocations, and debug directories.
//
// For building final images from a linker, call [NewBuilder] to assemble a PE32+
// image from pre-resolved sections. For emitting COFF object files, use
// [NewObjBuilder]. For emitting static libraries (.lib), use [NewArchiveBuilder].
// For creating import libraries, use [NewImportLibBuilder].
package pe

// MachineType is the target CPU architecture stored in the COFF file header.
type MachineType uint16

const (
	MachineUnknown     MachineType = 0x0000 // IMAGE_FILE_MACHINE_UNKNOWN
	MachineAMD64       MachineType = 0x8664 // IMAGE_FILE_MACHINE_AMD64  – x86-64
	MachineARM64       MachineType = 0xAA64 // IMAGE_FILE_MACHINE_ARM64  – AArch64 LE
	MachineARM64EC     MachineType = 0xA641 // IMAGE_FILE_MACHINE_ARM64EC – ARM64/x64 interop ABI
	MachineARM64X      MachineType = 0xA64E // IMAGE_FILE_MACHINE_ARM64X – native+EC hybrid
	MachineI386        MachineType = 0x014C // IMAGE_FILE_MACHINE_I386
	MachineARMThumb2   MachineType = 0x01C4 // IMAGE_FILE_MACHINE_ARMNT  – ARM Thumb-2 LE
	MachineARM         MachineType = 0x01C0 // IMAGE_FILE_MACHINE_ARM    – ARM LE
	MachineEBC         MachineType = 0x0EBC // IMAGE_FILE_MACHINE_EBC    – EFI byte code
	MachineIA64        MachineType = 0x0200 // IMAGE_FILE_MACHINE_IA64
	MachineRISCV32     MachineType = 0x5032 // IMAGE_FILE_MACHINE_RISCV32
	MachineRISCV64     MachineType = 0x5064 // IMAGE_FILE_MACHINE_RISCV64
	MachineRISCV128    MachineType = 0x5128 // IMAGE_FILE_MACHINE_RISCV128
	MachineLoongArch32 MachineType = 0x6232 // IMAGE_FILE_MACHINE_LOONGARCH32
	MachineLoongArch64 MachineType = 0x6264 // IMAGE_FILE_MACHINE_LOONGARCH64
	MachinePowerPC     MachineType = 0x01F0 // IMAGE_FILE_MACHINE_POWERPC
)

// Subsystem is the Windows subsystem required to run the image
// (IMAGE_OPTIONAL_HEADER Subsystem field).
type Subsystem uint16

const (
	SubsystemUnknown        Subsystem = 0  // IMAGE_SUBSYSTEM_UNKNOWN
	SubsystemNative         Subsystem = 1  // IMAGE_SUBSYSTEM_NATIVE – drivers and NT native processes
	SubsystemWindowsGUI     Subsystem = 2  // IMAGE_SUBSYSTEM_WINDOWS_GUI
	SubsystemWindowsCUI     Subsystem = 3  // IMAGE_SUBSYSTEM_WINDOWS_CUI – console
	SubsystemOS2CUI         Subsystem = 5  // IMAGE_SUBSYSTEM_OS2_CUI
	SubsystemPosixCUI       Subsystem = 7  // IMAGE_SUBSYSTEM_POSIX_CUI
	SubsystemNativeWindows  Subsystem = 8  // IMAGE_SUBSYSTEM_NATIVE_WINDOWS – Win9x driver
	SubsystemWindowsCE      Subsystem = 9  // IMAGE_SUBSYSTEM_WINDOWS_CE_GUI
	SubsystemEFIApplication Subsystem = 10 // IMAGE_SUBSYSTEM_EFI_APPLICATION
	SubsystemEFIBootService Subsystem = 11 // IMAGE_SUBSYSTEM_EFI_BOOT_SERVICE_DRIVER
	SubsystemEFIRuntime     Subsystem = 12 // IMAGE_SUBSYSTEM_EFI_RUNTIME_DRIVER
	SubsystemEFIROM         Subsystem = 13 // IMAGE_SUBSYSTEM_EFI_ROM
	SubsystemXbox           Subsystem = 14 // IMAGE_SUBSYSTEM_XBOX
	SubsystemBootApp        Subsystem = 16 // IMAGE_SUBSYSTEM_WINDOWS_BOOT_APPLICATION
)

// IMAGE_FILE_* characteristic flags for the COFF file header.
const (
	IMAGE_FILE_RELOCS_STRIPPED         = uint16(0x0001) // No base relocations; must load at preferred base.
	IMAGE_FILE_EXECUTABLE_IMAGE        = uint16(0x0002) // Valid, runnable image.
	IMAGE_FILE_LINE_NUMS_STRIPPED      = uint16(0x0004) // COFF line numbers removed (deprecated).
	IMAGE_FILE_LOCAL_SYMS_STRIPPED     = uint16(0x0008) // COFF local symbols removed (deprecated).
	IMAGE_FILE_AGGRESSIVE_WS_TRIM      = uint16(0x0010) // Obsolete; must be zero for Windows 2000+.
	IMAGE_FILE_LARGE_ADDRESS_AWARE     = uint16(0x0020) // Can handle > 2 GiB addresses.
	IMAGE_FILE_32BIT_MACHINE           = uint16(0x0100) // 32-bit word machine.
	IMAGE_FILE_DEBUG_STRIPPED          = uint16(0x0200) // Debug information removed from image.
	IMAGE_FILE_REMOVABLE_RUN_FROM_SWAP = uint16(0x0400) // Copy to swap if on removable media.
	IMAGE_FILE_NET_RUN_FROM_SWAP       = uint16(0x0800) // Copy to swap if on network media.
	IMAGE_FILE_SYSTEM                  = uint16(0x1000) // System file; not a user program.
	IMAGE_FILE_DLL                     = uint16(0x2000) // Dynamic-link library.
	IMAGE_FILE_UP_SYSTEM_ONLY          = uint16(0x4000) // Run only on a uniprocessor machine.
)

// IMAGE_DLLCHARACTERISTICS_* flags for the optional header DllCharacteristics field.
const (
	IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA       = uint16(0x0020) // 64-bit ASLR VA space.
	IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE          = uint16(0x0040) // Can be relocated at load time (ASLR).
	IMAGE_DLLCHARACTERISTICS_FORCE_INTEGRITY       = uint16(0x0080) // Code integrity checks enforced.
	IMAGE_DLLCHARACTERISTICS_NX_COMPAT             = uint16(0x0100) // NX (DEP) compatible.
	IMAGE_DLLCHARACTERISTICS_NO_ISOLATION          = uint16(0x0200) // Isolation-aware but do not isolate.
	IMAGE_DLLCHARACTERISTICS_NO_SEH                = uint16(0x0400) // No structured exception handling.
	IMAGE_DLLCHARACTERISTICS_NO_BIND               = uint16(0x0800) // Do not bind the image.
	IMAGE_DLLCHARACTERISTICS_APPCONTAINER          = uint16(0x1000) // Must execute in AppContainer.
	IMAGE_DLLCHARACTERISTICS_WDM_DRIVER            = uint16(0x2000) // WDM driver.
	IMAGE_DLLCHARACTERISTICS_GUARD_CF              = uint16(0x4000) // Control Flow Guard supported.
	IMAGE_DLLCHARACTERISTICS_TERMINAL_SERVER_AWARE = uint16(0x8000) // Terminal Server aware.
	IMAGE_DLLCHARACTERISTICS_CET_COMPAT            = uint16(0x4000) // Alias: same bit as GUARD_CF in some toolchains; use GUARD_CF.
)

// IMAGE_SCN_* section characteristic flags (Characteristics field of a section header).
const (
	IMAGE_SCN_TYPE_NO_PAD            = uint32(0x00000008) // Do not pad section to next boundary (deprecated).
	IMAGE_SCN_CNT_CODE               = uint32(0x00000020) // Contains executable code.
	IMAGE_SCN_CNT_INITIALIZED_DATA   = uint32(0x00000040) // Contains initialized data.
	IMAGE_SCN_CNT_UNINITIALIZED_DATA = uint32(0x00000080) // Contains uninitialized data (BSS).
	IMAGE_SCN_LNK_OTHER              = uint32(0x00000100) // Reserved.
	IMAGE_SCN_LNK_INFO               = uint32(0x00000200) // Contains comments or other info (.drectve).
	IMAGE_SCN_LNK_REMOVE             = uint32(0x00000800) // Will not appear in image (object files).
	IMAGE_SCN_LNK_COMDAT             = uint32(0x00001000) // Contains COMDAT data.
	IMAGE_SCN_GPREL                  = uint32(0x00008000) // Referenced via GP-relative addressing.
	IMAGE_SCN_ALIGN_1BYTES           = uint32(0x00100000) // Align data on 1-byte boundary (object only).
	IMAGE_SCN_ALIGN_2BYTES           = uint32(0x00200000)
	IMAGE_SCN_ALIGN_4BYTES           = uint32(0x00300000)
	IMAGE_SCN_ALIGN_8BYTES           = uint32(0x00400000)
	IMAGE_SCN_ALIGN_16BYTES          = uint32(0x00500000) // Default section alignment.
	IMAGE_SCN_ALIGN_32BYTES          = uint32(0x00600000)
	IMAGE_SCN_ALIGN_64BYTES          = uint32(0x00700000)
	IMAGE_SCN_ALIGN_128BYTES         = uint32(0x00800000)
	IMAGE_SCN_ALIGN_256BYTES         = uint32(0x00900000)
	IMAGE_SCN_ALIGN_512BYTES         = uint32(0x00A00000)
	IMAGE_SCN_ALIGN_1024BYTES        = uint32(0x00B00000)
	IMAGE_SCN_ALIGN_2048BYTES        = uint32(0x00C00000)
	IMAGE_SCN_ALIGN_4096BYTES        = uint32(0x00D00000)
	IMAGE_SCN_ALIGN_8192BYTES        = uint32(0x00E00000)
	IMAGE_SCN_LNK_NRELOC_OVFL        = uint32(0x01000000) // Extended relocation count (> 0xFFFF).
	IMAGE_SCN_MEM_DISCARDABLE        = uint32(0x02000000) // Can be discarded after loading.
	IMAGE_SCN_MEM_NOT_CACHED         = uint32(0x04000000) // Cannot be cached.
	IMAGE_SCN_MEM_NOT_PAGED          = uint32(0x08000000) // Not pageable.
	IMAGE_SCN_MEM_SHARED             = uint32(0x10000000) // Shared in memory.
	IMAGE_SCN_MEM_EXECUTE            = uint32(0x20000000) // Executable.
	IMAGE_SCN_MEM_READ               = uint32(0x40000000) // Readable.
	IMAGE_SCN_MEM_WRITE              = uint32(0x80000000) // Writable.
)

// Commonly composed section characteristic sets.
const (
	// ScnCode is the standard characteristic set for executable code sections (.text).
	ScnCode = IMAGE_SCN_CNT_CODE | IMAGE_SCN_MEM_EXECUTE | IMAGE_SCN_MEM_READ

	// ScnROData is the standard characteristic set for read-only data (.rdata, .xdata).
	ScnROData = IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ

	// ScnRWData is the standard characteristic set for read-write data (.data, .bss).
	ScnRWData = IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE

	// ScnBSS is the characteristic set for zero-initialized data (.bss).
	ScnBSS = IMAGE_SCN_CNT_UNINITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_WRITE

	// ScnDiscardable is used for sections that can be freed after image load (.reloc, .debug).
	ScnDiscardable = IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ | IMAGE_SCN_MEM_DISCARDABLE
)

// Base-relocation type values stored in the high 4 bits of each .reloc word.
const (
	IMAGE_REL_BASED_ABSOLUTE  = uint8(0)  // Padding; no fixup.
	IMAGE_REL_BASED_HIGH      = uint8(1)  // Add high 16 bits of delta to WORD at offset.
	IMAGE_REL_BASED_LOW       = uint8(2)  // Add low 16 bits of delta to WORD at offset.
	IMAGE_REL_BASED_HIGHLOW   = uint8(3)  // Add full 32-bit delta to DWORD at offset.
	IMAGE_REL_BASED_HIGHADJ   = uint8(4)  // High 16 bits; next word holds low adjustment.
	IMAGE_REL_BASED_DIR64     = uint8(10) // Add full 64-bit delta to QWORD at offset.
)

// Data-directory slot indices within the optional header (NumberOfRvaAndSizes = 16).
const (
	DataDirExport      = 0  // .edata – export directory
	DataDirImport      = 1  // .idata – import directory
	DataDirResource    = 2  // .rsrc  – resource directory
	DataDirException   = 3  // .pdata – exception table
	DataDirSecurity    = 4  // certificate table (file offset, not RVA)
	DataDirBaseReloc   = 5  // .reloc – base relocation table
	DataDirDebug       = 6  // .debug – debug directory
	DataDirArchitecture= 7  // reserved, must be zero
	DataDirGlobalPtr   = 8  // global pointer register value (size must be 0)
	DataDirTLS         = 9  // .tls   – thread local storage directory
	DataDirLoadConfig  = 10 // load configuration directory
	DataDirBoundImport = 11 // bound import table
	DataDirIAT         = 12 // import address table (separate entry for fast loader mapping)
	DataDirDelayImport = 13 // .didat – delay-load import descriptors
	DataDirCLR         = 14 // .cormeta – CLR runtime header
	NumDataDirectories = 16
)

// COFF symbol storage-class values (IMAGE_SYM_CLASS_*).
const (
	IMAGE_SYM_CLASS_NULL             = uint8(0)
	IMAGE_SYM_CLASS_AUTOMATIC        = uint8(1)   // Automatic (stack) variable.
	IMAGE_SYM_CLASS_EXTERNAL         = uint8(2)   // Global or imported symbol.
	IMAGE_SYM_CLASS_STATIC           = uint8(3)   // Local section symbol or static variable.
	IMAGE_SYM_CLASS_REGISTER         = uint8(4)
	IMAGE_SYM_CLASS_EXTERNAL_DEF     = uint8(5)
	IMAGE_SYM_CLASS_LABEL            = uint8(6)
	IMAGE_SYM_CLASS_UNDEFINED_LABEL  = uint8(7)
	IMAGE_SYM_CLASS_MEMBER_OF_STRUCT = uint8(8)
	IMAGE_SYM_CLASS_FUNCTION         = uint8(101) // 0x65 – .bf/.ef records.
	IMAGE_SYM_CLASS_FILE             = uint8(103) // 0x67 – source file name.
	IMAGE_SYM_CLASS_SECTION          = uint8(104) // 0x68 – section symbol (aux = section def).
	IMAGE_SYM_CLASS_WEAK_EXTERNAL    = uint8(105) // 0x69 – weak external.
	IMAGE_SYM_CLASS_CLR_TOKEN        = uint8(107) // 0x6B – CLR metadata token.
)

// COFF symbol section number values with special meaning.
const (
	IMAGE_SYM_UNDEFINED = int16(0)  // External reference; defined elsewhere.
	IMAGE_SYM_ABSOLUTE  = int16(-1) // Absolute value; not a section address.
	IMAGE_SYM_DEBUG     = int16(-2) // Debug-info record; no relocation significance.
)

// COFF symbol base types (low byte of Type field).
const (
	IMAGE_SYM_TYPE_NULL   = uint16(0)  // No type information.
	IMAGE_SYM_TYPE_VOID   = uint16(1)
	IMAGE_SYM_TYPE_CHAR   = uint16(2)
	IMAGE_SYM_TYPE_SHORT  = uint16(3)
	IMAGE_SYM_TYPE_INT    = uint16(6)
	IMAGE_SYM_TYPE_LONG   = uint16(7)
	IMAGE_SYM_TYPE_FLOAT  = uint16(8)
	IMAGE_SYM_TYPE_DOUBLE = uint16(9)
	IMAGE_SYM_TYPE_STRUCT = uint16(10)
	IMAGE_SYM_TYPE_UNION  = uint16(11)
	IMAGE_SYM_TYPE_ENUM   = uint16(12)
)

// COFF symbol derived types (high byte of Type field; shift left by 4).
const (
	IMAGE_SYM_DTYPE_NULL     = uint16(0) // No derived type.
	IMAGE_SYM_DTYPE_POINTER  = uint16(1) // Pointer.
	IMAGE_SYM_DTYPE_FUNCTION = uint16(2) // Function returning base type.
	IMAGE_SYM_DTYPE_ARRAY    = uint16(3) // Array of base type.
)

// MakeCOFFType combines a base type and derived type into a COFF symbol Type field.
// Example: MakeCOFFType(IMAGE_SYM_TYPE_INT, IMAGE_SYM_DTYPE_FUNCTION) = 0x0026.
func MakeCOFFType(base, derived uint16) uint16 {
	return base | (derived << 4)
}

// COMDAT selection codes for auxiliary section-definition records.
const (
	IMAGE_COMDAT_SELECT_NODUPLICATES = uint8(1) // Error if multiply defined.
	IMAGE_COMDAT_SELECT_ANY          = uint8(2) // Use any definition (most common for inline fns).
	IMAGE_COMDAT_SELECT_SAME_SIZE    = uint8(3) // Use any; error if sizes differ.
	IMAGE_COMDAT_SELECT_EXACT_MATCH  = uint8(4) // Use any; error if contents differ.
	IMAGE_COMDAT_SELECT_ASSOCIATIVE  = uint8(5) // Linked with another COMDAT section.
	IMAGE_COMDAT_SELECT_LARGEST      = uint8(6) // Use the largest definition.
)

// Weak-external search characteristics.
const (
	IMAGE_WEAK_EXTERN_SEARCH_NOLIBRARY = uint32(1) // Do not search libraries.
	IMAGE_WEAK_EXTERN_SEARCH_LIBRARY   = uint32(2) // Search libraries.
	IMAGE_WEAK_EXTERN_SEARCH_ALIAS     = uint32(3) // Symbol is alias for TagIndex symbol.
)

// IMAGE_GUARD_* flags for the GuardFlags field of the load configuration directory.
const (
	IMAGE_GUARD_CF_INSTRUMENTED                    = uint32(0x00000100) // Module performs CFI checks.
	IMAGE_GUARD_CFW_INSTRUMENTED                   = uint32(0x00000200) // Module performs CF + write integrity checks.
	IMAGE_GUARD_CF_FUNCTION_TABLE_PRESENT          = uint32(0x00000400) // Module contains valid CF target metadata.
	IMAGE_GUARD_SECURITY_COOKIE_UNUSED             = uint32(0x00000800) // Module does not use /GS security cookie.
	IMAGE_GUARD_PROTECT_DELAYLOAD_IAT              = uint32(0x00001000) // Module protects the delay-load IAT.
	IMAGE_GUARD_DELAYLOAD_IAT_IN_ITS_OWN_SECTION  = uint32(0x00002000) // Delay IAT is in its own .didat section.
	IMAGE_GUARD_CF_EXPORT_SUPPRESSION_INFO_PRESENT = uint32(0x00004000) // Export suppression info present.
	IMAGE_GUARD_CF_ENABLE_EXPORT_SUPPRESSION       = uint32(0x00008000) // Export suppression enabled.
	IMAGE_GUARD_CF_LONGJUMP_TABLE_PRESENT          = uint32(0x00010000) // Module has longjmp target table.
	IMAGE_GUARD_RF_INSTRUMENTED                    = uint32(0x00020000) // Module contains return flow instrumentation.
	IMAGE_GUARD_RF_ENABLE                          = uint32(0x00040000) // Return flow enforcement enabled.
	IMAGE_GUARD_RF_STRICT                          = uint32(0x00080000) // Return flow strict mode.
	IMAGE_GUARD_RETPOLINE_PRESENT                  = uint32(0x00100000) // Module was built with retpoline support.
	IMAGE_GUARD_EH_CONTINUATION_TABLE_PRESENT      = uint32(0x00200000) // EH continuation target table present.
	IMAGE_GUARD_XFG_ENABLED                        = uint32(0x00400000) // eXtended Flow Guard enabled.
	IMAGE_GUARD_CF_FUNCTION_TABLE_SIZE_MASK        = uint32(0xF0000000) // Stride of CFG function table entries (bytes extra per entry).
	IMAGE_GUARD_CF_FUNCTION_TABLE_SIZE_SHIFT       = 28
)

// Debug type identifiers (IMAGE_DEBUG_DIRECTORY.Type).
const (
	IMAGE_DEBUG_TYPE_UNKNOWN       = uint32(0)
	IMAGE_DEBUG_TYPE_COFF          = uint32(1)  // COFF debug info.
	IMAGE_DEBUG_TYPE_CODEVIEW      = uint32(2)  // CodeView (PDB).
	IMAGE_DEBUG_TYPE_FPO           = uint32(3)  // Frame Pointer Omission info.
	IMAGE_DEBUG_TYPE_MISC          = uint32(4)  // Miscellaneous debug info (DBG file path).
	IMAGE_DEBUG_TYPE_EXCEPTION     = uint32(5)  // Exception table copy.
	IMAGE_DEBUG_TYPE_FIXUP         = uint32(6)  // Fixup table.
	IMAGE_DEBUG_TYPE_OMAP_TO_SRC   = uint32(7)  // Address mapping to source.
	IMAGE_DEBUG_TYPE_OMAP_FROM_SRC = uint32(8)  // Address mapping from source.
	IMAGE_DEBUG_TYPE_BORLAND       = uint32(9)  // Borland.
	IMAGE_DEBUG_TYPE_CLSID         = uint32(11) // CLSID of the DLL.
	IMAGE_DEBUG_TYPE_VC_FEATURE    = uint32(12) // VC feature counts.
	IMAGE_DEBUG_TYPE_POGO          = uint32(13) // Profile-guided optimisation data.
	IMAGE_DEBUG_TYPE_ILTCG         = uint32(14) // Incremental link-time code generation.
	IMAGE_DEBUG_TYPE_MPX           = uint32(15) // Intel MPX.
	IMAGE_DEBUG_TYPE_REPRO         = uint32(16) // Reproducible build hash; TimeDateStamp must equal this hash.
	IMAGE_DEBUG_TYPE_SPGO          = uint32(18) // Sample-profile guided optimisation.
	IMAGE_DEBUG_TYPE_EMBED_PDB     = uint32(17) // Embedded portable PDB.
	IMAGE_DEBUG_TYPE_EX_DLLCHARACTERISTICS = uint32(20) // Extended DLL characteristics.
)

// Import type for short-format import library stubs.
const (
	IMPORT_CODE  = uint16(0) // Import is a code symbol (function).
	IMPORT_DATA  = uint16(1) // Import is a data symbol.
	IMPORT_CONST = uint16(2) // Import is a const symbol.
)

// Import name type for short-format import library stubs.
const (
	IMPORT_ORDINAL          = uint16(0) // Import by ordinal.
	IMPORT_NAME             = uint16(1) // Import by name (symbol name in stub).
	IMPORT_NAME_NOPREFIX    = uint16(2) // Import by name, strip leading ? or @.
	IMPORT_NAME_UNDECORATE  = uint16(3) // Import by name, strip decorations up to first @.
	IMPORT_NAME_EXPORTAS    = uint16(4) // Import by name specified in extra string field.
)

// x64 unwind operation codes (UNWIND_CODE.UnwindOp).
const (
	UWOP_PUSH_NONVOL     = uint8(0)  // Push a nonvolatile integer register.
	UWOP_ALLOC_LARGE     = uint8(1)  // Allocate a large-sized area on the stack.
	UWOP_ALLOC_SMALL     = uint8(2)  // Allocate a small-sized area on the stack (8–128 bytes).
	UWOP_SET_FPREG       = uint8(3)  // Establish frame pointer register.
	UWOP_SAVE_NONVOL     = uint8(4)  // Save nonvolatile register on stack using MOV.
	UWOP_SAVE_NONVOL_FAR = uint8(5)  // Save nonvolatile register on stack with large offset.
	UWOP_EPILOG          = uint8(6)  // Describe the epilog.
	UWOP_SAVE_XMM128     = uint8(8)  // Save all 128 bits of nonvolatile XMM register using MOVAPS.
	UWOP_SAVE_XMM128_FAR = uint8(9)  // Save all 128 bits of nonvolatile XMM with large offset.
	UWOP_PUSH_MACHFRAME  = uint8(10) // Push a machine frame (for interrupt/exception handlers).
)

// UNW_FLAG_* values for UNWIND_INFO.Flags.
const (
	UNW_FLAG_NHANDLER  = uint8(0x0) // No handler.
	UNW_FLAG_EHANDLER  = uint8(0x1) // Function has an exception handler.
	UNW_FLAG_UHANDLER  = uint8(0x2) // Function has a termination handler.
	UNW_FLAG_CHAININFO = uint8(0x4) // Chained unwind info; no handler fields.
)

// x64 integer register numbers used in unwind codes.
const (
	RegRAX = uint8(0)
	RegRCX = uint8(1)
	RegRDX = uint8(2)
	RegRBX = uint8(3)
	RegRSP = uint8(4)
	RegRBP = uint8(5)
	RegRSI = uint8(6)
	RegRDI = uint8(7)
	RegR8  = uint8(8)
	RegR9  = uint8(9)
	RegR10 = uint8(10)
	RegR11 = uint8(11)
	RegR12 = uint8(12)
	RegR13 = uint8(13)
	RegR14 = uint8(14)
	RegR15 = uint8(15)
)