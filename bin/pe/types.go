package pe

// ────────────────────────────────────────────────────────────────────────────
// Image-emission types  (consumed by Builder → serialize)
// ────────────────────────────────────────────────────────────────────────────

// DataDirectory holds the virtual address and byte size of one optional-header
// data directory entry.
type DataDirectory struct {
	VirtualAddress uint32
	Size           uint32
}

// PEImage is the fully resolved, pre-serialized image produced by [Builder.buildImage]
// and consumed by [serialize]. All virtual addresses have been assigned, all
// relocations applied, and all Windows-specific tables built before this type
// is constructed. [serialize] writes it directly with no further resolution.
type PEImage struct {
	Machine   MachineType
	Subsystem Subsystem
	ImageBase uint64 // 0 → architecture default.

	// Sections carries the fully laid-out, patched section data.
	// VirtualAddress and VirtualSize must be set by the caller.
	Sections []PESection

	// DataDirs holds all NumDataDirectories data directory entries.
	DataDirs [NumDataDirectories]DataDirectory

	// EntryRVA is the AddressOfEntryPoint (image-relative).
	// Zero for DLLs with no explicit entry point.
	EntryRVA uint32

	StackReserve uint64 // 0 → 1 MiB
	StackCommit  uint64 // 0 → 4 KiB
	HeapReserve  uint64 // 0 → 1 MiB
	HeapCommit   uint64 // 0 → 4 KiB

	// MajorOSVersion / MinorOSVersion: minimum Windows version required.
	// Default (0/0) serializes as 6/0 (Windows Vista).
	MajorOSVersion uint16
	MinorOSVersion uint16

	// MajorSubsystemVersion / MinorSubsystemVersion: minimum subsystem version.
	// Default (0/0) serializes as 6/0.
	MajorSubsystemVersion uint16
	MinorSubsystemVersion uint16

	DllCharacteristics  uint16 // IMAGE_DLLCHARACTERISTICS_* flags.
	FileCharacteristics uint16 // Extra IMAGE_FILE_* flags ORed with computed ones.
	IsDLL               bool
}

// PESection is a single fully-built section inside a [PEImage].
type PESection struct {
	// Name is the section name (up to 8 bytes; longer names are truncated).
	Name string

	// Chars is the IMAGE_SCN_* characteristics bitmask.
	Chars uint32

	// VirtualAddress is the section RVA when loaded (image-relative).
	VirtualAddress uint32

	// VirtualSize is the in-memory byte count of the section.
	VirtualSize uint32

	// Data is the raw data padded to FileAlignment. len(Data) must be a multiple
	// of fileAlignment (512). May be shorter than VirtualSize (zero-fill implied).
	// May be nil for purely BSS sections.
	Data []byte
}

// ────────────────────────────────────────────────────────────────────────────
// Builder input types
// ────────────────────────────────────────────────────────────────────────────

// Section describes a user-provided section contributed to the image builder.
type Section struct {
	// Name is the section name. Must be ≤ 8 bytes for image files.
	Name string

	// Chars is the IMAGE_SCN_* characteristics bitmask.
	Chars uint32

	// Data contains initialized bytes. Nil or empty for BSS-style sections.
	Data []byte

	// VirtualSize overrides the in-memory byte count when it differs from
	// len(Data). If zero, len(Data) is used. BSS sections set VirtualSize > 0
	// with nil or empty Data.
	VirtualSize uint32
}

// Symbol names an address within a section for entry-point resolution and
// export-table construction.
type Symbol struct {
	// Name is the symbol name.
	Name string

	// Section is the name of the section containing this symbol.
	Section string

	// Offset is the byte offset from the start of the named section.
	Offset uint32

	// Global marks the symbol as externally visible.
	Global bool
}

// ImportSymbol describes a single function or variable imported from a DLL.
type ImportSymbol struct {
	// Name is the function name for a named import. Empty → import by ordinal.
	Name string

	// Ordinal is used when Name is empty.
	Ordinal uint16

	// Hint is the export-name-pointer-table index hint (0 is always acceptable).
	Hint uint16
}

// Import describes all symbols imported from one DLL.
type Import struct {
	// DLL is the case-insensitive DLL name (e.g. "kernel32.dll").
	DLL string

	// Symbols lists every function or datum imported from DLL.
	Symbols []ImportSymbol
}

// DelayImport describes a DLL whose load is deferred until first use.
type DelayImport struct {
	// DLL is the case-insensitive DLL name.
	DLL string

	// Symbols lists the functions to import lazily.
	Symbols []ImportSymbol
}

// Export describes a symbol to be exported from the image (DLL mode only).
type Export struct {
	// Name is the export name. Empty means an unnamed (ordinal-only) export.
	Name string

	// Symbol is the internal symbol whose address is exported.
	// Must name a symbol added via Builder.AddSymbol.
	Symbol string

	// Ordinal is the export ordinal. The export address table is indexed by
	// (Ordinal − OrdinalBase), where OrdinalBase is the minimum ordinal.
	Ordinal uint16
}

// ────────────────────────────────────────────────────────────────────────────
// Windows-specific table input types
// ────────────────────────────────────────────────────────────────────────────

// RuntimeFunction is one entry in the .pdata section (RUNTIME_FUNCTION).
// All three fields are image-relative addresses (RVAs).
type RuntimeFunction struct {
	BeginRVA       uint32
	EndRVA         uint32
	UnwindInfoRVA  uint32 // RVA of the UNWIND_INFO record in .xdata.
}

// UnwindCode is a single prolog operation record in an UNWIND_INFO structure.
type UnwindCode struct {
	// PrologOffset is the byte offset of the end of this instruction within the prolog.
	PrologOffset uint8
	// Op is the UWOP_* operation code.
	Op uint8
	// OpInfo is the 4-bit register or size operand (interpretation depends on Op).
	OpInfo uint8
	// Extra holds an additional 16-bit slot value, used by multi-slot operations
	// (UWOP_ALLOC_LARGE with OpInfo=0, UWOP_SAVE_NONVOL, UWOP_SAVE_XMM128, etc.).
	Extra uint16
	// Extra2 holds a second additional 16-bit slot value, used by far-save operations
	// and UWOP_ALLOC_LARGE with OpInfo=1.
	Extra2 uint16
}

// UnwindInfo describes an UNWIND_INFO structure for a single function in .xdata.
type UnwindInfo struct {
	// Flags is a combination of UNW_FLAG_* values.
	Flags uint8
	// SizeOfProlog is the byte length of the function prolog.
	SizeOfProlog uint8
	// FrameRegister is the nonvolatile register used as the frame pointer (0 = none).
	FrameRegister uint8
	// FrameOffset is the scaled offset (× 16) from RSP to the frame register value.
	FrameOffset uint8
	// Codes is the list of prolog unwind operations.
	Codes []UnwindCode
	// ExceptionHandlerRVA is the RVA of the language-specific exception/termination handler.
	// Required when Flags includes UNW_FLAG_EHANDLER or UNW_FLAG_UHANDLER.
	ExceptionHandlerRVA uint32
	// HandlerData is opaque data for the exception handler.
	HandlerData []byte
	// Chained is the chained RUNTIME_FUNCTION for UNW_FLAG_CHAININFO.
	Chained *RuntimeFunction
}

// TLSDirectory describes the IMAGE_TLS_DIRECTORY64 structure.
// All address fields are virtual addresses (VAs), not RVAs.
type TLSDirectory struct {
	// StartAddressOfRawData is the VA of the TLS template data start.
	StartAddressOfRawData uint64
	// EndAddressOfRawData is the VA one past the end of the TLS template data.
	EndAddressOfRawData uint64
	// AddressOfIndex is the VA of the DWORD that the loader fills with the TLS index.
	AddressOfIndex uint64
	// AddressOfCallbacks is the VA of a null-terminated array of TLS callback VAs.
	// Zero if there are no callbacks.
	AddressOfCallbacks uint64
	// SizeOfZeroFill is additional zero-initialized bytes appended after the template.
	SizeOfZeroFill uint32
	// Characteristics is reserved; must be zero.
	Characteristics uint32
}

// LoadConfig holds the fields the linker typically populates in
// IMAGE_LOAD_CONFIG_DIRECTORY64. Fields not set remain zero.
type LoadConfig struct {
	// SecurityCookieVA is the virtual address of the __security_cookie variable.
	// Required for /GS stack-cookie protection.
	SecurityCookieVA uint64

	// SEHandlerTableVA and SEHandlerCount describe the safe SEH table (x86 only).
	SEHandlerTableVA uint64
	SEHandlerCount   uint64

	// DependentLoadFlags is ORed into the flags passed to LoadLibrary for
	// delay-loaded dependencies (e.g. LOAD_LIBRARY_SEARCH_SYSTEM32 = 0x0800).
	DependentLoadFlags uint16

	// GuardCFCheckFunctionPointerVA is the VA of the CFG check-function pointer.
	GuardCFCheckFunctionPointerVA    uint64
	// GuardCFDispatchFunctionPointerVA is the VA of the CFG dispatch-function pointer.
	GuardCFDispatchFunctionPointerVA uint64
	// GuardCFFunctionTableVA is the VA of the sorted CFG function-address table.
	GuardCFFunctionTableVA uint64
	// GuardCFFunctionCount is the number of entries in the CFG table.
	GuardCFFunctionCount uint64
	// GuardFlags is a combination of IMAGE_GUARD_* values.
	GuardFlags uint32

	// GuardAddressTakenIATEntryTableVA and Count describe the address-taken IAT table.
	GuardAddressTakenIATEntryTableVA    uint64
	GuardAddressTakenIATEntryCount      uint64
	// GuardLongJumpTargetTableVA and Count describe the longjmp target table.
	GuardLongJumpTargetTableVA  uint64
	GuardLongJumpTargetCount    uint64

	// CodeIntegrityFlags enables kernel-mode code integrity for the image.
	// Set to 0 for user-mode binaries.
	CodeIntegrityFlags uint16
}

// DebugEntry describes one IMAGE_DEBUG_DIRECTORY record.
type DebugEntry struct {
	// Type is an IMAGE_DEBUG_TYPE_* constant.
	Type uint32
	// Data is the raw payload bytes written after the directory structure.
	Data []byte
}

// ────────────────────────────────────────────────────────────────────────────
// COFF object-file types  (consumed by ObjBuilder → serializeObject)
// ────────────────────────────────────────────────────────────────────────────

// COFFSection describes one section in a COFF object file.
type COFFSection struct {
	// Name is the section name (up to 8 bytes, or "/offset" for string-table names).
	Name string
	// Chars is the IMAGE_SCN_* characteristics bitmask.
	Chars uint32
	// Data is the raw section bytes.
	Data []byte
	// Relocs lists COFF relocations to be applied to Data.
	Relocs []COFFReloc
}

// COFFReloc is one COFF relocation record within an object-file section.
type COFFReloc struct {
	// Offset is the byte offset within the section where the fixup is applied.
	Offset uint32
	// SymbolIndex is the 0-based index into the COFF symbol table.
	SymbolIndex uint32
	// Type is the IMAGE_REL_*_* relocation type for the target machine.
	Type uint16
}

// COFFSymbol describes one entry in the COFF symbol table.
type COFFSymbol struct {
	// Name is the symbol name. Names longer than 8 bytes go into the string table.
	Name string
	// Value is the symbol value (address offset, absolute value, etc.).
	Value uint32
	// SectionNumber is the 1-based section index, or IMAGE_SYM_UNDEFINED /
	// IMAGE_SYM_ABSOLUTE / IMAGE_SYM_DEBUG.
	SectionNumber int16
	// Type is built with MakeCOFFType. Use IMAGE_SYM_DTYPE_FUNCTION for functions.
	Type uint16
	// StorageClass is an IMAGE_SYM_CLASS_* value.
	StorageClass uint8
	// Aux holds optional auxiliary records (each must be exactly 18 bytes when serialized).
	Aux []COFFAuxRecord
}

// COFFAuxRecord is one auxiliary symbol record (18 bytes when serialized).
// Exactly one of the embedded structs should be non-nil.
type COFFAuxRecord struct {
	FunctionDef *AuxFunctionDef
	WeakExternal *AuxWeakExternal
	SectionDef  *AuxSectionDef
	File        *AuxFile
}

// AuxFunctionDef is the auxiliary record for a function-definition symbol
// (StorageClass == IMAGE_SYM_CLASS_EXTERNAL, Type has DTYPE_FUNCTION).
type AuxFunctionDef struct {
	TagIndex          uint32 // Symbol-table index of the corresponding .bf symbol.
	TotalSize         uint32 // Size of the function's code.
	PointerToNextFunction uint32 // Symbol-table index of the next function, or 0.
}

// AuxWeakExternal is the auxiliary record for a weak external symbol
// (StorageClass == IMAGE_SYM_CLASS_WEAK_EXTERNAL).
type AuxWeakExternal struct {
	TagIndex       uint32 // Symbol-table index of the default resolution symbol.
	Characteristics uint32 // IMAGE_WEAK_EXTERN_SEARCH_* value.
}

// AuxSectionDef is the auxiliary record for a section symbol
// (StorageClass == IMAGE_SYM_CLASS_STATIC, symbol represents a section).
type AuxSectionDef struct {
	Length              uint32 // Section data byte count.
	NumberOfRelocations uint16
	NumberOfLinenumbers uint16 // Always 0 (deprecated).
	Checksum            uint32 // COMDAT checksum (JenkinsBHash or CRC32 of data).
	Number              uint16 // 1-based section index of the COMDAT associate, or 0.
	Selection           uint8  // IMAGE_COMDAT_SELECT_* value.
}

// AuxFile is the auxiliary record for a source-file symbol
// (StorageClass == IMAGE_SYM_CLASS_FILE). Filename may span multiple aux records.
type AuxFile struct {
	FileName string
}