package pe

// File-level layout constants for PE32+ images.
const (
	fileAlignment     = uint32(0x200)   // 512 bytes; minimum raw-data granularity.
	sectionAlignment  = uint32(0x1000)  // 4 KiB; must be ≥ fileAlignment.
	peSignatureSize   = 4
	coffHeaderSize    = 20
	optHeaderSize     = 240             // PE32+ optional header (no BaseOfData field).
	sectionHeaderSize = 40
	dosStubSize       = 64
	fixedHeaderBytes  = dosStubSize + peSignatureSize + coffHeaderSize + optHeaderSize
)

// dosStub is the minimal 64-byte MS-DOS stub. The PE signature offset (0x3C)
// stores dosStubSize so the Windows loader finds the PE header immediately after.
var dosStub = [dosStubSize]byte{
	0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00, // MZ header
	0x04, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00,
	0xB8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00, // e_lfanew = 0x40 = dosStubSize
}

// Default preferred image-base addresses matching MSVC/LLD defaults.
const (
	defaultExeImageBase = uint64(0x0000000140000000) // 1 GiB mark; standard x64 EXE base.
	defaultDLLImageBase = uint64(0x0000000180000000) // 2 GiB mark; standard x64 DLL base.
)

// Default stack and heap sizes.
const (
	defaultStackReserve = uint64(1 << 20) // 1 MiB
	defaultStackCommit  = uint64(1 << 12) // 4 KiB
	defaultHeapReserve  = uint64(1 << 20) // 1 MiB
	defaultHeapCommit   = uint64(1 << 12) // 4 KiB
)

// Builder assembles a PE32+ image from sections, symbols, imports, exports, and
// Windows-specific tables (exception data, TLS, delay imports, load configuration).
//
// The expected call sequence is:
//
//  1. NewBuilder(machine)
//  2. SetSubsystem, SetImageBase, SetDLL, etc.
//  3. AddSection (user sections, in load order)
//  4. AddSymbol  (needed for entry-point resolution and export table)
//  5. AddImport / AddDelayImport / AddExport
//  6. SetPdata, SetTLS, SetLoadConfig, AddDebugEntry
//  7. Emit() → []byte
type Builder struct {
	machine      MachineType
	subsystem    Subsystem
	imageBase    uint64
	dllMode      bool
	dllName      string
	entry        string

	sections     []Section
	symbols      []Symbol

	imports      []Import
	delayImports []DelayImport
	exports      []Export

	pdataFuncs   []RuntimeFunction  // pre-built RUNTIME_FUNCTION records
	xdataBlob    []byte             // pre-built .xdata section (UNWIND_INFO records)

	tlsData      []byte             // TLS template data
	tlsCallbacks []uint32           // TLS callback RVAs (resolved before Emit)

	loadConfig   *LoadConfig

	debugEntries []DebugEntry

	stackReserve uint64
	stackCommit  uint64
	heapReserve  uint64
	heapCommit   uint64

	majorOSVersion        uint16
	minorOSVersion        uint16
	majorSubsystemVersion uint16
	minorSubsystemVersion uint16

	dllCharacteristics  uint16
	extraFileChars      uint16

	// extraDataDirs allows linker/pe to override or set data-directory entries
	// that would otherwise not be set by the high-level Builder API
	// (e.g. DataDirException for a .pdata section added via AddSection).
	extraDataDirs [NumDataDirectories]DataDirectory
}

// NewBuilder returns a Builder configured for the given machine type.
// The default subsystem is SubsystemWindowsCUI (console), and the default
// DllCharacteristics enable ASLR, high-entropy VA, NX, and CFG.
func NewBuilder(machine MachineType) *Builder {
	return &Builder{
		machine:   machine,
		subsystem: SubsystemWindowsCUI,
		dllCharacteristics: IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA |
			IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE |
			IMAGE_DLLCHARACTERISTICS_NX_COMPAT |
			IMAGE_DLLCHARACTERISTICS_GUARD_CF,
		majorOSVersion:        6,
		minorOSVersion:        0,
		majorSubsystemVersion: 6,
		minorSubsystemVersion: 0,
	}
}

// ── Section & symbol ─────────────────────────────────────────────────────────

// AddSection appends a user section. Sections are laid out in the order added.
func (b *Builder) AddSection(s Section) { b.sections = append(b.sections, s) }

// AddSymbol records a symbol for entry-point resolution and export-table construction.
func (b *Builder) AddSymbol(s Symbol) { b.symbols = append(b.symbols, s) }

// ── Imports & exports ────────────────────────────────────────────────────────

// AddImport appends a standard (load-time) import from a DLL.
func (b *Builder) AddImport(imp Import) { b.imports = append(b.imports, imp) }

// AddDelayImport appends a delay-loaded import. The DLL is not loaded until
// the first call to one of its symbols at runtime.
func (b *Builder) AddDelayImport(d DelayImport) { b.delayImports = append(b.delayImports, d) }

// AddExport appends an export (only meaningful in DLL mode; see SetDLL).
func (b *Builder) AddExport(e Export) { b.exports = append(b.exports, e) }

// ── Exception handling ───────────────────────────────────────────────────────

// SetPdata provides the pre-built .pdata and .xdata section contents.
// funcs is the sorted list of RUNTIME_FUNCTION records; xdata is the
// corresponding .xdata (UNWIND_INFO) blob. Use [PdataBuilder] to construct them.
func (b *Builder) SetPdata(funcs []RuntimeFunction, xdata []byte) {
	b.pdataFuncs = funcs
	b.xdataBlob = xdata
}

// ── TLS ──────────────────────────────────────────────────────────────────────

// SetTLS sets the TLS template data. callbackRVAs lists the RVAs of
// TLS callback functions in the order they should be invoked. Callers
// must have assigned the RVAs before calling Emit; use AddSymbol to
// record the relevant symbols and obtain their RVAs from the linker.
//
// For images built directly (no linker), use [TLSBuilder] to produce
// a ready-made .tls section and directory instead.
func (b *Builder) SetTLS(templateData []byte, callbackRVAs []uint32) {
	b.tlsData = templateData
	b.tlsCallbacks = callbackRVAs
}

// ── Load configuration ───────────────────────────────────────────────────────

// SetLoadConfig sets the IMAGE_LOAD_CONFIG_DIRECTORY64 contents.
// The most common fields are SecurityCookieVA (required for /GS)
// and GuardFlags + GuardCFFunctionTableVA (required for CFG).
func (b *Builder) SetLoadConfig(lc LoadConfig) { b.loadConfig = &lc }

// ── Debug ────────────────────────────────────────────────────────────────────

// AddDebugEntry appends one IMAGE_DEBUG_DIRECTORY entry to the .debug section.
// Use [BuildCodeViewPDB] and [BuildReproEntry] to create common entry types.
func (b *Builder) AddDebugEntry(d DebugEntry) { b.debugEntries = append(b.debugEntries, d) }

// ── Configuration ────────────────────────────────────────────────────────────

// SetSubsystem sets the required Windows subsystem (default: SubsystemWindowsCUI).
func (b *Builder) SetSubsystem(ss Subsystem) { b.subsystem = ss }

// SetImageBase overrides the preferred load address (default: architecture-specific).
// Must be a multiple of 64 KiB.
func (b *Builder) SetImageBase(base uint64) { b.imageBase = base }

// SetEntry names the entry-point symbol. Emit returns an error if the symbol
// cannot be resolved to a virtual address.
func (b *Builder) SetEntry(name string) { b.entry = name }

// SetDLL puts the builder in DLL mode and sets the DLL name used in the export
// directory. IMAGE_FILE_DLL is added to the file characteristics automatically.
func (b *Builder) SetDLL(name string) { b.dllMode = true; b.dllName = name }

// SetDllCharacteristics replaces the default DllCharacteristics flags.
func (b *Builder) SetDllCharacteristics(f uint16) { b.dllCharacteristics = f }

// AddDllCharacteristics ORs additional flags into DllCharacteristics.
func (b *Builder) AddDllCharacteristics(f uint16) { b.dllCharacteristics |= f }

// SetExtraFileCharacteristics ORs additional IMAGE_FILE_* flags into the
// computed file characteristics (e.g. IMAGE_FILE_DEBUG_STRIPPED).
func (b *Builder) SetExtraFileCharacteristics(f uint16) { b.extraFileChars = f }

// SetExtraDataDir overrides a data-directory entry. Used by linker/pe when
// .pdata, .tls, or .debug sections are contributed as plain user sections
// (via AddSection) rather than via SetPdata / SetTLS / AddDebugEntry.
// An entry with both VA and Size equal to zero is ignored.
func (b *Builder) SetExtraDataDir(slot int, va, size uint32) {
	if slot >= 0 && slot < NumDataDirectories {
		b.extraDataDirs[slot] = DataDirectory{VirtualAddress: va, Size: size}
	}
}

// SetStackSize sets the reserved and committed stack sizes.
// Defaults are 1 MiB reserved / 4 KiB committed.
func (b *Builder) SetStackSize(reserve, commit uint64) {
	b.stackReserve, b.stackCommit = reserve, commit
}

// SetHeapSize sets the reserved and committed default-process-heap sizes.
// Defaults are 1 MiB reserved / 4 KiB committed.
func (b *Builder) SetHeapSize(reserve, commit uint64) {
	b.heapReserve, b.heapCommit = reserve, commit
}

// SetOSVersion sets the minimum OS version encoded in the optional header.
// The default is 6.0 (Windows Vista).
func (b *Builder) SetOSVersion(major, minor uint16) {
	b.majorOSVersion, b.minorOSVersion = major, minor
}

// SetSubsystemVersion sets the minimum subsystem version. Default is 6.0.
func (b *Builder) SetSubsystemVersion(major, minor uint16) {
	b.majorSubsystemVersion, b.minorSubsystemVersion = major, minor
}

// ── Emit ─────────────────────────────────────────────────────────────────────

// Emit assembles and serializes the complete PE32+ image.
func (b *Builder) Emit() ([]byte, error) {
	img, err := b.buildImage()
	if err != nil {
		return nil, err
	}
	return serialize(img)
}