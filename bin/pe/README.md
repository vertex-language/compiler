# bin/pe

```
github.com/vertex-language/compiler/bin/pe
```

Constructs and serializes PE32+ executable images, dynamic-link libraries (DLLs), COFF object files (`.obj`), and Windows static/import archives (`.lib`). Given sections, symbols, imports, and relocations, `Emit()` produces well-formed binaries ready for execution or linking.

The package handles PE header synthesis, virtual address layout, import/export table generation (`.idata`/`.edata`), base relocations (`.reloc`), exception unwinding (`.pdata`/`.xdata`), thread-local storage (`.tls`), delay-load imports (`.didat`), Control Flow Guard (CFG) load configurations, and debug directories.

---

## Table of Contents

- [Quick Start](#quick-start)
- [PE32+ Image Builder](#pe32-image-builder)
  - [NewBuilder](#newbuilder)
  - [Configuration Methods](#configuration-methods)
  - [Sections and Symbols](#sections-and-symbols)
  - [Imports and Exports](#imports-and-exports)
  - [Exception Handling — .pdata / .xdata](#exception-handling--pdata--xdata)
  - [Thread-Local Storage — .tls](#thread-local-storage--tls)
  - [Load Configuration and CFG](#load-configuration-and-cfg)
  - [Debug Directories](#debug-directories)
  - [Emit](#emit)
  - [Emit Passes](#emit-passes)
- [COFF Object Files — ObjBuilder](#coff-object-files--objbuilder)
  - [Sections and Relocations](#sections-and-relocations)
  - [Symbols and Auxiliary Records](#symbols-and-auxiliary-records)
- [Static Archives — ArchiveBuilder](#static-archives--archivebuilder)
- [Import Libraries — ImportLibBuilder](#import-libraries--importlibbuilder)
- [TLSBuilder — Manual TLS Construction](#tlsbuilder--manual-tls-construction)
- [PdataBuilder — Manual Pdata Construction](#pdatabuilder--manual-pdata-construction)
- [Low-Level / Pre-Resolved Images — PEImage](#low-level--pre-resolved-images--peimage)
- [Constants Reference](#constants-reference)
  - [Machine Types](#machine-types)
  - [Subsystems](#subsystems)
  - [File Characteristics — IMAGE_FILE_*](#file-characteristics--image_file_)
  - [DLL Characteristics — IMAGE_DLLCHARACTERISTICS_*](#dll-characteristics--image_dllcharacteristics_)
  - [Section Characteristics — IMAGE_SCN_*](#section-characteristics--image_scn_)
  - [Composed Section Helpers](#composed-section-helpers)
  - [Base Relocation Types — IMAGE_REL_BASED_*](#base-relocation-types--image_rel_based_)
  - [Data Directory Indices](#data-directory-indices)
  - [COFF Symbol Storage Classes — IMAGE_SYM_CLASS_*](#coff-symbol-storage-classes--image_sym_class_)
  - [COFF Symbol Section Numbers](#coff-symbol-section-numbers)
  - [COFF Symbol Types](#coff-symbol-types)
  - [COMDAT Selection Codes](#comdat-selection-codes)
  - [Weak External Search Characteristics](#weak-external-search-characteristics)
  - [Guard Flags — IMAGE_GUARD_*](#guard-flags--image_guard_)
  - [Debug Types — IMAGE_DEBUG_TYPE_*](#debug-types--image_debug_type_)
  - [Import Kind and Name Types](#import-kind-and-name-types)
  - [Unwind Operation Codes — UWOP_*](#unwind-operation-codes--uwop_)
  - [Unwind Flags — UNW_FLAG_*](#unwind-flags--unw_flag_)
  - [x64 Register Numbers](#x64-register-numbers)
  - [COFF AMD64 Relocations — IMAGE_REL_AMD64_*](#coff-amd64-relocations--image_rel_amd64_)
  - [COFF ARM64 Relocations — IMAGE_REL_ARM64_*](#coff-arm64-relocations--image_rel_arm64_)

---

## Quick Start

```go
b := pe.NewBuilder(pe.MachineAMD64)

b.AddSection(pe.Section{
    Name:  ".text",
    Chars: pe.ScnCode,
    Data:  machineCode,
})

b.AddSymbol(pe.Symbol{
    Name:    "main",
    Section: ".text",
    Offset:  0,
    Global:  true,
})

b.SetEntry("main")

b.AddImport(pe.Import{
    DLL:     "kernel32.dll",
    Symbols: []pe.ImportSymbol{{Name: "ExitProcess"}},
})

out, err := b.Emit()
if err != nil {
    log.Fatal(err)
}
os.WriteFile("program.exe", out, 0o755)
```

---

## PE32+ Image Builder

### NewBuilder

```go
func NewBuilder(machine MachineType) *Builder
```

Returns a `Builder` for the given architecture. Defaults:

- Subsystem: `SubsystemWindowsCUI` (console)
- Image base: `0x140000000` (EXE) or `0x180000000` (DLL)
- OS/Subsystem version: 6.0 (Windows Vista)
- Stack: 1 MiB reserved / 4 KiB committed
- Heap: 1 MiB reserved / 4 KiB committed
- `DllCharacteristics`: `HIGH_ENTROPY_VA | DYNAMIC_BASE | NX_COMPAT | GUARD_CF`

---

### Configuration Methods

```go
// SetSubsystem sets the Windows subsystem (default: SubsystemWindowsCUI).
func (b *Builder) SetSubsystem(ss Subsystem)

// SetImageBase overrides the preferred load address. Must be a multiple of 64 KiB.
func (b *Builder) SetImageBase(base uint64)

// SetEntry names the entry-point symbol. Emit returns an error if the symbol
// cannot be resolved.
func (b *Builder) SetEntry(name string)

// SetDLL switches to DLL mode and sets the DLL name used in the export directory.
// IMAGE_FILE_DLL is added to file characteristics automatically.
func (b *Builder) SetDLL(name string)

// SetStackSize sets the reserved and committed stack sizes.
func (b *Builder) SetStackSize(reserve, commit uint64)

// SetHeapSize sets the reserved and committed default-process-heap sizes.
func (b *Builder) SetHeapSize(reserve, commit uint64)

// SetOSVersion sets the minimum OS version in the optional header (default 6.0).
func (b *Builder) SetOSVersion(major, minor uint16)

// SetSubsystemVersion sets the minimum subsystem version (default 6.0).
func (b *Builder) SetSubsystemVersion(major, minor uint16)

// SetDllCharacteristics replaces the DllCharacteristics flags entirely.
func (b *Builder) SetDllCharacteristics(f uint16)

// AddDllCharacteristics ORs additional flags into DllCharacteristics.
func (b *Builder) AddDllCharacteristics(f uint16)

// SetExtraFileCharacteristics ORs additional IMAGE_FILE_* flags into the
// computed file characteristics (e.g. IMAGE_FILE_DEBUG_STRIPPED).
func (b *Builder) SetExtraFileCharacteristics(f uint16)
```

---

### Sections and Symbols

#### Section

```go
type Section struct {
    Name        string // Section name, up to 8 bytes (e.g. ".text").
    Chars       uint32 // IMAGE_SCN_* characteristics bitmask.
    Data        []byte // Initialized bytes; nil or empty for BSS-style sections.
    VirtualSize uint32 // Overrides the in-memory byte count when it differs from
                       // len(Data). If zero, len(Data) is used. Set this for BSS.
}
```

#### Symbol

```go
type Symbol struct {
    Name    string // Symbol name.
    Section string // Name of the section containing this symbol.
    Offset  uint32 // Byte offset from the start of the named section.
    Global  bool   // Whether the symbol is externally visible.
}
```

```go
// AddSection appends a user section. Sections are laid out in the order added.
func (b *Builder) AddSection(s Section)

// AddSymbol records a symbol for entry-point resolution and export-table construction.
func (b *Builder) AddSymbol(s Symbol)
```

**Example — BSS section:**

```go
b.AddSection(pe.Section{
    Name:        ".bss",
    Chars:       pe.ScnBSS,
    VirtualSize: 4096, // 4 KiB of zero-initialized data; no file bytes.
})
```

---

### Imports and Exports

#### Import

```go
type Import struct {
    DLL     string        // Case-insensitive DLL name, e.g. "kernel32.dll".
    Symbols []ImportSymbol
}

type ImportSymbol struct {
    Name    string // Function name. Empty → import by ordinal.
    Ordinal uint16 // Used when Name is empty.
    Hint    uint16 // Export-name-pointer-table index hint (0 is always valid).
}
```

#### DelayImport

```go
type DelayImport struct {
    DLL     string        // Case-insensitive DLL name.
    Symbols []ImportSymbol
}
```

#### Export

```go
type Export struct {
    Name    string // Export name. Empty means ordinal-only.
    Symbol  string // Internal symbol whose address is exported.
                   // Must name a symbol added via AddSymbol.
    Ordinal uint16 // Export ordinal.
}
```

```go
// AddImport appends a standard (load-time) DLL import.
func (b *Builder) AddImport(imp Import)

// AddDelayImport appends a delay-loaded DLL import. The DLL is not loaded until
// the first call to one of its symbols at runtime.
func (b *Builder) AddDelayImport(d DelayImport)

// AddExport appends an export entry. Only meaningful in DLL mode (see SetDLL).
func (b *Builder) AddExport(e Export)
```

**Example — named and ordinal imports:**

```go
b.AddImport(pe.Import{
    DLL: "kernel32.dll",
    Symbols: []pe.ImportSymbol{
        {Name: "ExitProcess"},
        {Name: "WriteFile", Hint: 3},
        {Ordinal: 42}, // import by ordinal
    },
})
```

**Example — delay-loaded import:**

```go
b.AddDelayImport(pe.DelayImport{
    DLL:     "advapi32.dll",
    Symbols: []pe.ImportSymbol{{Name: "RegOpenKeyExW"}},
})
```

**Example — DLL with exports:**

```go
b.SetDLL("mylib.dll")
b.AddSymbol(pe.Symbol{Name: "MyFunc", Section: ".text", Offset: 0, Global: true})
b.AddExport(pe.Export{Name: "MyFunc", Symbol: "MyFunc", Ordinal: 1})
```

---

### Exception Handling — .pdata / .xdata

Windows x64 requires unwind information for every non-leaf function. Use `PdataBuilder` to build `.pdata` and `.xdata`, then attach them to the main builder.

```go
// SetPdata provides pre-built .pdata and .xdata section contents.
// Use PdataBuilder to construct them.
func (b *Builder) SetPdata(funcs []RuntimeFunction, xdata []byte)
```

See [PdataBuilder — Manual Pdata Construction](#pdatabuilder--manual-pdata-construction) for the full API.

**Example:**

```go
pb := pe.NewPdataBuilder()
pb.AddWithUnwindInfo(beginRVA, endRVA, &pe.UnwindInfo{
    Flags:         pe.UNW_FLAG_EHANDLER,
    SizeOfProlog:  14,
    FrameRegister: pe.RegRBP,
    FrameOffset:   0,
    Codes: []pe.UnwindCode{
        {PrologOffset: 14, Op: pe.UWOP_ALLOC_SMALL, OpInfo: 4},
        {PrologOffset: 4,  Op: pe.UWOP_SAVE_NONVOL,  OpInfo: pe.RegRBP, Extra: 0},
    },
    ExceptionHandlerRVA: handlerRVA,
    HandlerData:         scopeTableBytes,
})

b.SetPdata(pb.Funcs(), pb.XdataBlob())
```

---

### Thread-Local Storage — .tls

```go
// SetTLS sets the TLS template data and callback RVAs.
// callbackRVAs must be fully resolved before calling Emit.
// For images built without a linker, prefer TLSBuilder directly.
func (b *Builder) SetTLS(templateData []byte, callbackRVAs []uint32)
```

**Example:**

```go
b.SetTLS(initializedTLSBytes, []uint32{callbackRVA})
```

---

### Load Configuration and CFG

```go
// SetLoadConfig sets the IMAGE_LOAD_CONFIG_DIRECTORY64 contents.
func (b *Builder) SetLoadConfig(lc LoadConfig)
```

```go
type LoadConfig struct {
    SecurityCookieVA uint64 // VA of __security_cookie (/GS).

    SEHandlerTableVA uint64 // VA of the safe SEH table (x86 only).
    SEHandlerCount   uint64

    DependentLoadFlags uint16 // Flags for delay-loaded LoadLibrary calls
                              // (e.g. LOAD_LIBRARY_SEARCH_SYSTEM32 = 0x0800).

    GuardCFCheckFunctionPointerVA    uint64 // VA of CFG check-function pointer.
    GuardCFDispatchFunctionPointerVA uint64 // VA of CFG dispatch-function pointer.
    GuardCFFunctionTableVA           uint64 // VA of sorted CFG function-address table.
    GuardCFFunctionCount             uint64 // Number of CFG table entries.
    GuardFlags                       uint32 // IMAGE_GUARD_* flags.

    GuardAddressTakenIATEntryTableVA uint64
    GuardAddressTakenIATEntryCount   uint64
    GuardLongJumpTargetTableVA       uint64
    GuardLongJumpTargetCount         uint64

    CodeIntegrityFlags uint16 // Set non-zero only for kernel-mode binaries.
}
```

**Example:**

```go
b.SetLoadConfig(pe.LoadConfig{
    SecurityCookieVA:                 cookieVA,
    DependentLoadFlags:               0x0800,
    GuardCFCheckFunctionPointerVA:    cfgCheckVA,
    GuardCFDispatchFunctionPointerVA: cfgDispatchVA,
    GuardCFFunctionTableVA:           cfgTableVA,
    GuardCFFunctionCount:             uint64(len(cfgTargets)),
    GuardFlags: pe.IMAGE_GUARD_CF_INSTRUMENTED |
                pe.IMAGE_GUARD_CF_FUNCTION_TABLE_PRESENT,
})
```

---

### Debug Directories

```go
// AddDebugEntry appends one IMAGE_DEBUG_DIRECTORY entry to the .debug section.
func (b *Builder) AddDebugEntry(d DebugEntry)
```

```go
type DebugEntry struct {
    Type uint32 // IMAGE_DEBUG_TYPE_* constant.
    Data []byte // Raw payload bytes written after the directory structure.
}
```

Three convenience constructors are provided:

```go
// BuildCodeViewPDB creates a CodeView PDB 7.0 (RSDS) debug entry.
//   guid    – 16-byte GUID uniquely identifying the PDB build.
//   age     – incremented each time the PDB is updated without a new GUID.
//   pdbPath – path to the .pdb file.
func BuildCodeViewPDB(guid [16]byte, age uint32, pdbPath string) DebugEntry

// BuildReproEntry creates an IMAGE_DEBUG_TYPE_REPRO entry for reproducible builds.
// When present, TimeDateStamp in the COFF header must equal this hash value.
// hash is typically a SHA-256 digest (32 bytes).
func BuildReproEntry(hash []byte) DebugEntry

// BuildVCFeatureEntry creates an IMAGE_DEBUG_TYPE_VC_FEATURE entry.
// Pass zeros for a minimal compliant entry.
func BuildVCFeatureEntry(preVC11, cppPlusPlus11, gs, sdl, guardN uint32) DebugEntry
```

**Example:**

```go
var guid [16]byte
copy(guid[:], pdbGUIDBytes)

b.AddDebugEntry(pe.BuildCodeViewPDB(guid, 1, `C:\build\app.pdb`))
b.AddDebugEntry(pe.BuildReproEntry(sha256OfImage))
b.AddDebugEntry(pe.BuildVCFeatureEntry(0, 0, 1, 0, 0))
```

---

### Emit

```go
// Emit assembles and serializes the complete PE32+ image, returning raw bytes.
func (b *Builder) Emit() ([]byte, error)
```

Returns an error if:
- The entry-point symbol was set but cannot be resolved.
- An export references an unknown symbol.
- Pre-sizing of `.idata` or `.didat` fails.

---

### Emit Passes

When `Emit()` is called, `buildImage` performs the following steps:

1. **Resolve defaults** — image base, stack/heap sizes, OS version.
2. **Decide synthetic sections** — flags which of `.idata`, `.edata`, `.didat`, `.pdata`, `.xdata`, `.tls`, `.rdata$lc`, `.debug`, `.reloc` are needed.
3. **Compute header size** — `sizeOfHeaders` is aligned to `fileAlignment` (512 bytes).
4. **Pre-size synthetic sections** — measures `.idata` and `.edata` before VAs are finalized.
5. **Assign virtual addresses** — user sections then synthetic placeholders, each aligned to `sectionAlignment` (4 KiB).
6. **Build symbol VA map** — maps symbol name → virtual address.
7. **Collect base relocations** — queues `IMAGE_REL_BASED_DIR64` entries for absolute VAs in the TLS directory.
8. **Build synthetic sections** — generates final bytes for all synthetic sections using their finalized VAs.
9. **Assemble section list** — user sections followed by synthetic sections in a fixed order.
10. **Compute `SizeOfImage`** — highest section end, aligned to `sectionAlignment`.
11. **Resolve entry-point RVA** — looks up the entry symbol in the VA map.
12. **Populate data directories** — fills the 16 optional-header data directory entries.
13. **Compute file characteristics** — `EXECUTABLE_IMAGE | LARGE_ADDRESS_AWARE` plus DLL/reloc flags.
14. **Serialize** — writes the DOS stub, PE signature, COFF header, optional header, section headers, and section raw data padded to `fileAlignment`.

---

## COFF Object Files — ObjBuilder

Builds linkable `.obj` files. Unlike image files, COFF objects have no optional header, no DOS stub, no PE signature, and section VAs are zero (relocations are resolved at link time).

```go
func NewObjBuilder(machine MachineType) *ObjBuilder

func (ob *ObjBuilder) AddSection(s COFFSection)
func (ob *ObjBuilder) AddSymbol(s COFFSymbol)
func (ob *ObjBuilder) Emit() ([]byte, error)
```

### Sections and Relocations

```go
type COFFSection struct {
    Name   string     // Section name (up to 8 bytes, or "/offset" for string-table names).
    Chars  uint32     // IMAGE_SCN_* characteristics bitmask.
    Data   []byte     // Raw section bytes.
    Relocs []COFFReloc
}

type COFFReloc struct {
    Offset      uint32 // Byte offset within the section where the fixup is applied.
    SymbolIndex uint32 // 0-based index into the COFF symbol table.
    Type        uint16 // IMAGE_REL_*_* relocation type for the target machine.
}
```

**Example:**

```go
ob := pe.NewObjBuilder(pe.MachineAMD64)

ob.AddSection(pe.COFFSection{
    Name:  ".text",
    Chars: pe.ScnCode,
    Data:  machineCode,
    Relocs: []pe.COFFReloc{
        {Offset: 1, SymbolIndex: 1, Type: pe.IMAGE_REL_AMD64_REL32},
    },
})

objData, err := ob.Emit()
```

---

### Symbols and Auxiliary Records

```go
type COFFSymbol struct {
    Name          string        // Symbol name. Names > 8 bytes go into the string table.
    Value         uint32        // Symbol value (address offset, absolute value, etc.).
    SectionNumber int16         // 1-based section index, or IMAGE_SYM_UNDEFINED /
                                // IMAGE_SYM_ABSOLUTE / IMAGE_SYM_DEBUG.
    Type          uint16        // Built with MakeCOFFType. Use IMAGE_SYM_DTYPE_FUNCTION
                                // for functions.
    StorageClass  uint8         // IMAGE_SYM_CLASS_* value.
    Aux           []COFFAuxRecord
}
```

Symbols must be ordered with all non-`EXTERNAL` symbols before `EXTERNAL` / `WEAK_EXTERNAL` symbols.

```go
// MakeCOFFType combines a base type and derived type into a COFF Type field.
// Example: MakeCOFFType(IMAGE_SYM_TYPE_NULL, IMAGE_SYM_DTYPE_FUNCTION) → 0x0020
func MakeCOFFType(base, derived uint16) uint16
```

#### Auxiliary Records

Each `COFFAuxRecord` carries exactly one non-nil embedded struct:

```go
type COFFAuxRecord struct {
    FunctionDef  *AuxFunctionDef
    WeakExternal *AuxWeakExternal
    SectionDef   *AuxSectionDef
    File         *AuxFile
}
```

```go
// AuxFunctionDef — for function-definition symbols
// (StorageClass == IMAGE_SYM_CLASS_EXTERNAL, Type has DTYPE_FUNCTION).
type AuxFunctionDef struct {
    TagIndex              uint32 // Symbol-table index of the corresponding .bf symbol.
    TotalSize             uint32 // Size of the function's code.
    PointerToNextFunction uint32 // Symbol-table index of the next function, or 0.
}

// AuxWeakExternal — for weak external symbols
// (StorageClass == IMAGE_SYM_CLASS_WEAK_EXTERNAL).
type AuxWeakExternal struct {
    TagIndex        uint32 // Symbol-table index of the default resolution symbol.
    Characteristics uint32 // IMAGE_WEAK_EXTERN_SEARCH_* value.
}

// AuxSectionDef — for section symbols
// (StorageClass == IMAGE_SYM_CLASS_STATIC, symbol represents a section).
type AuxSectionDef struct {
    Length              uint32
    NumberOfRelocations uint16
    NumberOfLinenumbers uint16 // Always 0 (deprecated).
    Checksum            uint32 // COMDAT checksum (JenkinsBHash or CRC32).
    Number              uint16 // 1-based section index of the COMDAT associate, or 0.
    Selection           uint8  // IMAGE_COMDAT_SELECT_* value.
}

// AuxFile — for source-file symbols
// (StorageClass == IMAGE_SYM_CLASS_FILE). Filename may span multiple aux records.
type AuxFile struct {
    FileName string
}
```

**Example — function symbol with aux record:**

```go
ob.AddSymbol(pe.COFFSymbol{
    Name:          "main",
    SectionNumber: 1,
    StorageClass:  pe.IMAGE_SYM_CLASS_EXTERNAL,
    Type:          pe.MakeCOFFType(pe.IMAGE_SYM_TYPE_NULL, pe.IMAGE_SYM_DTYPE_FUNCTION),
    Aux: []pe.COFFAuxRecord{
        {FunctionDef: &pe.AuxFunctionDef{TotalSize: uint32(len(machineCode))}},
    },
})
```

**Example — weak external:**

```go
ob.AddSymbol(pe.COFFSymbol{
    Name:         "weakSym",
    SectionNumber: pe.IMAGE_SYM_UNDEFINED,
    StorageClass: pe.IMAGE_SYM_CLASS_WEAK_EXTERNAL,
    Aux: []pe.COFFAuxRecord{
        {WeakExternal: &pe.AuxWeakExternal{
            TagIndex:        defaultSymIdx,
            Characteristics: pe.IMAGE_WEAK_EXTERN_SEARCH_LIBRARY,
        }},
    },
})
```

**Example — source file symbol:**

```go
ob.AddSymbol(pe.COFFSymbol{
    Name:         ".file",
    SectionNumber: pe.IMAGE_SYM_DEBUG,
    StorageClass: pe.IMAGE_SYM_CLASS_FILE,
    Aux: []pe.COFFAuxRecord{
        {File: &pe.AuxFile{FileName: "main.c"}},
    },
})
```

---

## Static Archives — ArchiveBuilder

Bundles multiple COFF object files into a `.lib` archive. Automatically synthesizes the first/second linker index members and the longnames table.

```go
func NewArchiveBuilder() *ArchiveBuilder

func (ab *ArchiveBuilder) AddMember(m ArchiveMember)
func (ab *ArchiveBuilder) Emit() ([]byte, error)
```

```go
type ArchiveMember struct {
    Name    string   // Member filename, e.g. "foo.obj".
    Data    []byte   // Raw COFF object file bytes.
    Symbols []string // Public symbols defined in Data (for the linker symbol table).
}
```

**Example:**

```go
ab := pe.NewArchiveBuilder()
ab.AddMember(pe.ArchiveMember{
    Name:    "math.obj",
    Data:    mathObjBytes,
    Symbols: []string{"add", "sub", "mul"},
})
ab.AddMember(pe.ArchiveMember{
    Name:    "string.obj",
    Data:    strObjBytes,
    Symbols: []string{"strlen", "strcpy"},
})
libBytes, err := ab.Emit()
```

---

## Import Libraries — ImportLibBuilder

Constructs short-format import libraries used to link against DLLs without needing their raw object files. Each entry generates a short-format COFF import stub (Sig1=`0x0000`, Sig2=`0xFFFF`).

```go
func NewImportLibBuilder(machine MachineType, dll string) *ImportLibBuilder

func (lb *ImportLibBuilder) Add(e ImportEntry)
func (lb *ImportLibBuilder) Emit() ([]byte, error)
```

```go
type ImportEntry struct {
    Name       string // Exported symbol name. Empty → ordinal-only import.
    ExportName string // Overrides the looked-up name for IMPORT_NAME_EXPORTAS.
    Ordinal    uint16 // Ordinal for ordinal imports; used as hint otherwise.
    Kind       uint16 // IMPORT_CODE, IMPORT_DATA, or IMPORT_CONST (default: IMPORT_CODE).
    NameType   uint16 // IMPORT_ORDINAL, IMPORT_NAME, IMPORT_NAME_NOPREFIX,
                      // IMPORT_NAME_UNDECORATE, or IMPORT_NAME_EXPORTAS.
                      // Default (0) with a non-empty Name resolves to IMPORT_NAME.
}
```

Each stub defines `__imp_<Name>` and `<Name>` as public symbols for the linker.

**Example:**

```go
lb := pe.NewImportLibBuilder(pe.MachineAMD64, "kernel32.dll")
lb.Add(pe.ImportEntry{Name: "ExitProcess", Kind: pe.IMPORT_CODE, NameType: pe.IMPORT_NAME})
lb.Add(pe.ImportEntry{Name: "WriteFile",   Kind: pe.IMPORT_CODE, NameType: pe.IMPORT_NAME})
lb.Add(pe.ImportEntry{Ordinal: 7,          Kind: pe.IMPORT_CODE, NameType: pe.IMPORT_ORDINAL})
importLibBytes, err := lb.Emit()
```

---

## TLSBuilder — Manual TLS Construction

`TLSBuilder` constructs the `.tls` section and `IMAGE_TLS_DIRECTORY64` for custom linkers or scenarios where `Builder.SetTLS` is not appropriate.

```go
func NewTLSBuilder() *TLSBuilder

// SetTemplate sets the TLS template data (initialized thread-local variables).
func (tb *TLSBuilder) SetTemplate(data []byte)

// SetZeroFill sets the number of zero-filled bytes appended after the template
// (for uninitialized TLS variables).
func (tb *TLSBuilder) SetZeroFill(n uint32)

// AddCallback appends a TLS callback function RVA. Callbacks are stored as
// VAs in the section and require base relocations.
func (tb *TLSBuilder) AddCallback(rva uint32)

// Build produces the raw .tls section bytes and a populated TLSDirectory.
// sectionVA is the virtual address of the .tls section.
// imageBase is used to convert RVAs to VAs for the directory fields.
func (tb *TLSBuilder) Build(sectionVA uint32, imageBase uint64) (TLSDirectory, []byte)
```

Section layout produced by `Build`:

```
[0]           IMAGE_TLS_DIRECTORY64   (40 bytes)
[40]          TLS template data       (len(template) bytes)
[40+tmpl]     TLS index DWORD         (4 bytes; zeroed, filled by the loader)
[44+tmpl]     padding to 8-byte boundary
[aligned...]  TLS callback array      ((len(callbacks)+1) × 8 bytes; null-terminated)
```

```go
type TLSDirectory struct {
    StartAddressOfRawData uint64 // VA of TLS template data start.
    EndAddressOfRawData   uint64 // VA one past the end of TLS template data.
    AddressOfIndex        uint64 // VA of the DWORD the loader fills with the TLS index.
    AddressOfCallbacks    uint64 // VA of null-terminated callback VA array. 0 if none.
    SizeOfZeroFill        uint32 // Additional zero-initialized bytes after the template.
    Characteristics       uint32 // Reserved; must be zero.
}
```

**Example:**

```go
tb := pe.NewTLSBuilder()
tb.SetTemplate([]byte{0x01, 0x02, 0x03, 0x04}) // initialized TLS vars
tb.SetZeroFill(256)                              // uninitialized TLS vars
tb.AddCallback(tlsCallbackRVA)

dir, rawBytes := tb.Build(tlsSectionVA, imageBase)
// rawBytes → use as PESection.Data
// dir      → convert to DataDirs[DataDirTLS]
```

---

## PdataBuilder — Manual Pdata Construction

`PdataBuilder` accumulates `RUNTIME_FUNCTION` records and their `UNWIND_INFO` structures, producing paired `.pdata` and `.xdata` section bytes.

```go
func NewPdataBuilder() *PdataBuilder

// Add appends a pre-built RUNTIME_FUNCTION with an already-resolved UnwindInfoRVA.
func (pb *PdataBuilder) Add(rf RuntimeFunction)

// AddWithUnwindInfo serializes ui into the internal .xdata buffer and appends a
// RUNTIME_FUNCTION pointing to it. Returns the xdata-section-relative offset of
// the new UNWIND_INFO record.
func (pb *PdataBuilder) AddWithUnwindInfo(beginRVA, endRVA uint32, ui *UnwindInfo) uint32

// Build returns the final .pdata and .xdata section bytes.
// For records added via AddWithUnwindInfo, xdataBaseRVA is added to their
// section-relative offsets to produce final UnwindInfoRVAs.
func (pb *PdataBuilder) Build(pdataBaseRVA, xdataBaseRVA uint32) (pdataSection, xdataSection []byte)

// Funcs returns the accumulated RuntimeFunction slice.
// UnwindInfoRVAs for AddWithUnwindInfo entries are section-relative until Build is called.
func (pb *PdataBuilder) Funcs() []RuntimeFunction

// XdataBlob returns the accumulated .xdata bytes.
func (pb *PdataBuilder) XdataBlob() []byte
```

```go
type RuntimeFunction struct {
    BeginRVA      uint32
    EndRVA        uint32
    UnwindInfoRVA uint32 // RVA of the UNWIND_INFO record in .xdata.
}

type UnwindInfo struct {
    Flags           uint8        // UNW_FLAG_* values.
    SizeOfProlog    uint8        // Byte length of the function prolog.
    FrameRegister   uint8        // Nonvolatile register used as frame pointer (0 = none).
    FrameOffset     uint8        // Scaled offset (×16) from RSP to the frame register value.
    Codes           []UnwindCode // Prolog unwind operations.
    ExceptionHandlerRVA uint32   // Required when Flags includes UNW_FLAG_EHANDLER or _UHANDLER.
    HandlerData     []byte       // Opaque data for the exception handler.
    Chained         *RuntimeFunction // For UNW_FLAG_CHAININFO.
}

type UnwindCode struct {
    PrologOffset uint8  // Byte offset of the end of this prolog instruction.
    Op           uint8  // UWOP_* operation code.
    OpInfo       uint8  // 4-bit register or size operand.
    Extra        uint16 // Additional 16-bit slot (UWOP_ALLOC_LARGE with OpInfo=0,
                        // UWOP_SAVE_NONVOL, UWOP_SAVE_XMM128, etc.).
    Extra2       uint16 // Second additional 16-bit slot (far-save ops,
                        // UWOP_ALLOC_LARGE with OpInfo=1).
}
```

**Example:**

```go
pb := pe.NewPdataBuilder()

// Option A: pre-resolved RUNTIME_FUNCTION (UnwindInfoRVA already absolute).
pb.Add(pe.RuntimeFunction{
    BeginRVA:      0x1000,
    EndRVA:        0x1040,
    UnwindInfoRVA: 0x5000,
})

// Option B: let PdataBuilder manage .xdata layout.
pb.AddWithUnwindInfo(0x1040, 0x1100, &pe.UnwindInfo{
    SizeOfProlog: 4,
    Codes: []pe.UnwindCode{
        {PrologOffset: 4, Op: pe.UWOP_PUSH_NONVOL, OpInfo: pe.RegRBP},
    },
})

// Chained unwind info.
pb.AddWithUnwindInfo(0x1100, 0x1200, &pe.UnwindInfo{
    Flags: pe.UNW_FLAG_CHAININFO,
    Chained: &pe.RuntimeFunction{
        BeginRVA: 0x1040, EndRVA: 0x1100, UnwindInfoRVA: 0x5010,
    },
})

// For use with Builder:
b.SetPdata(pb.Funcs(), pb.XdataBlob())

// For manual use:
pdataBytes, xdataBytes := pb.Build(pdataBaseRVA, xdataBaseRVA)
```

---

## Low-Level / Pre-Resolved Images — PEImage

`PEImage` is the fully resolved, pre-serialized image type produced by `buildImage` and consumed by `serialize`. Use it when you need to write a custom build pipeline that bypasses `Builder` entirely.

```go
type PEImage struct {
    Machine   MachineType
    Subsystem Subsystem
    ImageBase uint64 // 0 → architecture default.

    Sections []PESection
    DataDirs [NumDataDirectories]DataDirectory

    EntryRVA uint32 // AddressOfEntryPoint (image-relative). 0 for DLLs without entry.

    StackReserve uint64 // 0 → 1 MiB
    StackCommit  uint64 // 0 → 4 KiB
    HeapReserve  uint64 // 0 → 1 MiB
    HeapCommit   uint64 // 0 → 4 KiB

    MajorOSVersion uint16 // Default (0) serializes as 6.
    MinorOSVersion uint16
    MajorSubsystemVersion uint16
    MinorSubsystemVersion uint16

    DllCharacteristics  uint16 // IMAGE_DLLCHARACTERISTICS_* flags.
    FileCharacteristics uint16 // Extra IMAGE_FILE_* flags ORed with computed ones.
    IsDLL               bool
}

type PESection struct {
    Name           string // Section name (up to 8 bytes; longer names are truncated).
    Chars          uint32 // IMAGE_SCN_* characteristics bitmask.
    VirtualAddress uint32 // Section RVA when loaded (image-relative).
    VirtualSize    uint32 // In-memory byte count of the section.
    Data           []byte // Raw data padded to fileAlignment (512 bytes).
                          // May be nil for purely BSS sections.
}

type DataDirectory struct {
    VirtualAddress uint32
    Size           uint32
}
```

---

## Constants Reference

### Machine Types

```go
type MachineType uint16

const (
    MachineUnknown     MachineType = 0x0000
    MachineAMD64       MachineType = 0x8664 // x86-64
    MachineARM64       MachineType = 0xAA64 // AArch64 LE
    MachineARM64EC     MachineType = 0xA641 // ARM64/x64 interop ABI
    MachineARM64X      MachineType = 0xA64E // Native+EC hybrid
    MachineI386        MachineType = 0x014C // x86 32-bit
    MachineARMThumb2   MachineType = 0x01C4 // ARM Thumb-2 LE
    MachineARM         MachineType = 0x01C0 // ARM LE
    MachineEBC         MachineType = 0x0EBC // EFI byte code
    MachineIA64        MachineType = 0x0200
    MachineRISCV32     MachineType = 0x5032
    MachineRISCV64     MachineType = 0x5064
    MachineRISCV128    MachineType = 0x5128
    MachineLoongArch32 MachineType = 0x6232
    MachineLoongArch64 MachineType = 0x6264
    MachinePowerPC     MachineType = 0x01F0
)
```

### Subsystems

```go
type Subsystem uint16

const (
    SubsystemUnknown        Subsystem = 0
    SubsystemNative         Subsystem = 1  // Drivers and NT native processes.
    SubsystemWindowsGUI     Subsystem = 2  // Graphical applications.
    SubsystemWindowsCUI     Subsystem = 3  // Console applications.
    SubsystemOS2CUI         Subsystem = 5
    SubsystemPosixCUI       Subsystem = 7
    SubsystemNativeWindows  Subsystem = 8  // Win9x driver.
    SubsystemWindowsCE      Subsystem = 9
    SubsystemEFIApplication Subsystem = 10
    SubsystemEFIBootService Subsystem = 11
    SubsystemEFIRuntime     Subsystem = 12
    SubsystemEFIROM         Subsystem = 13
    SubsystemXbox           Subsystem = 14
    SubsystemBootApp        Subsystem = 16
)
```

### File Characteristics — IMAGE_FILE_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_FILE_RELOCS_STRIPPED` | `0x0001` | No base relocations; must load at preferred base. |
| `IMAGE_FILE_EXECUTABLE_IMAGE` | `0x0002` | Valid, runnable image. |
| `IMAGE_FILE_LINE_NUMS_STRIPPED` | `0x0004` | COFF line numbers removed (deprecated). |
| `IMAGE_FILE_LOCAL_SYMS_STRIPPED` | `0x0008` | COFF local symbols removed (deprecated). |
| `IMAGE_FILE_AGGRESSIVE_WS_TRIM` | `0x0010` | Obsolete; must be zero for Windows 2000+. |
| `IMAGE_FILE_LARGE_ADDRESS_AWARE` | `0x0020` | Can handle > 2 GiB addresses. |
| `IMAGE_FILE_32BIT_MACHINE` | `0x0100` | 32-bit word machine. |
| `IMAGE_FILE_DEBUG_STRIPPED` | `0x0200` | Debug information removed from image. |
| `IMAGE_FILE_REMOVABLE_RUN_FROM_SWAP` | `0x0400` | Copy to swap if on removable media. |
| `IMAGE_FILE_NET_RUN_FROM_SWAP` | `0x0800` | Copy to swap if on network media. |
| `IMAGE_FILE_SYSTEM` | `0x1000` | System file; not a user program. |
| `IMAGE_FILE_DLL` | `0x2000` | Dynamic-link library. |
| `IMAGE_FILE_UP_SYSTEM_ONLY` | `0x4000` | Run only on a uniprocessor machine. |

### DLL Characteristics — IMAGE_DLLCHARACTERISTICS_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA` | `0x0020` | 64-bit ASLR VA space. |
| `IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE` | `0x0040` | Can be relocated at load time (ASLR). |
| `IMAGE_DLLCHARACTERISTICS_FORCE_INTEGRITY` | `0x0080` | Code integrity checks enforced. |
| `IMAGE_DLLCHARACTERISTICS_NX_COMPAT` | `0x0100` | NX (DEP) compatible. |
| `IMAGE_DLLCHARACTERISTICS_NO_ISOLATION` | `0x0200` | Isolation-aware but do not isolate. |
| `IMAGE_DLLCHARACTERISTICS_NO_SEH` | `0x0400` | No structured exception handling. |
| `IMAGE_DLLCHARACTERISTICS_NO_BIND` | `0x0800` | Do not bind the image. |
| `IMAGE_DLLCHARACTERISTICS_APPCONTAINER` | `0x1000` | Must execute in AppContainer. |
| `IMAGE_DLLCHARACTERISTICS_WDM_DRIVER` | `0x2000` | WDM driver. |
| `IMAGE_DLLCHARACTERISTICS_GUARD_CF` | `0x4000` | Control Flow Guard supported. |
| `IMAGE_DLLCHARACTERISTICS_TERMINAL_SERVER_AWARE` | `0x8000` | Terminal Server aware. |

### Section Characteristics — IMAGE_SCN_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_SCN_TYPE_NO_PAD` | `0x00000008` | Do not pad to next boundary (deprecated). |
| `IMAGE_SCN_CNT_CODE` | `0x00000020` | Contains executable code. |
| `IMAGE_SCN_CNT_INITIALIZED_DATA` | `0x00000040` | Contains initialized data. |
| `IMAGE_SCN_CNT_UNINITIALIZED_DATA` | `0x00000080` | Contains uninitialized data (BSS). |
| `IMAGE_SCN_LNK_INFO` | `0x00000200` | Contains comments or linker directives (`.drectve`). |
| `IMAGE_SCN_LNK_REMOVE` | `0x00000800` | Will not appear in the image (object files). |
| `IMAGE_SCN_LNK_COMDAT` | `0x00001000` | Contains COMDAT data. |
| `IMAGE_SCN_GPREL` | `0x00008000` | Referenced via GP-relative addressing. |
| `IMAGE_SCN_ALIGN_1BYTES` | `0x00100000` | Align on 1-byte boundary (object files only). |
| `IMAGE_SCN_ALIGN_2BYTES` | `0x00200000` | |
| `IMAGE_SCN_ALIGN_4BYTES` | `0x00300000` | |
| `IMAGE_SCN_ALIGN_8BYTES` | `0x00400000` | |
| `IMAGE_SCN_ALIGN_16BYTES` | `0x00500000` | Default alignment. |
| `IMAGE_SCN_ALIGN_32BYTES` | `0x00600000` | |
| `IMAGE_SCN_ALIGN_64BYTES` | `0x00700000` | |
| `IMAGE_SCN_ALIGN_128BYTES` | `0x00800000` | |
| `IMAGE_SCN_ALIGN_256BYTES` | `0x00900000` | |
| `IMAGE_SCN_ALIGN_512BYTES` | `0x00A00000` | |
| `IMAGE_SCN_ALIGN_1024BYTES` | `0x00B00000` | |
| `IMAGE_SCN_ALIGN_2048BYTES` | `0x00C00000` | |
| `IMAGE_SCN_ALIGN_4096BYTES` | `0x00D00000` | |
| `IMAGE_SCN_ALIGN_8192BYTES` | `0x00E00000` | |
| `IMAGE_SCN_LNK_NRELOC_OVFL` | `0x01000000` | Extended relocation count (> 0xFFFF). |
| `IMAGE_SCN_MEM_DISCARDABLE` | `0x02000000` | Can be discarded after loading. |
| `IMAGE_SCN_MEM_NOT_CACHED` | `0x04000000` | Cannot be cached. |
| `IMAGE_SCN_MEM_NOT_PAGED` | `0x08000000` | Not pageable. |
| `IMAGE_SCN_MEM_SHARED` | `0x10000000` | Shared in memory. |
| `IMAGE_SCN_MEM_EXECUTE` | `0x20000000` | Executable. |
| `IMAGE_SCN_MEM_READ` | `0x40000000` | Readable. |
| `IMAGE_SCN_MEM_WRITE` | `0x80000000` | Writable. |

### Composed Section Helpers

| Constant | Composition | Typical Section |
|---|---|---|
| `ScnCode` | `CNT_CODE \| MEM_EXECUTE \| MEM_READ` | `.text` |
| `ScnROData` | `CNT_INITIALIZED_DATA \| MEM_READ` | `.rdata`, `.xdata` |
| `ScnRWData` | `CNT_INITIALIZED_DATA \| MEM_READ \| MEM_WRITE` | `.data` |
| `ScnBSS` | `CNT_UNINITIALIZED_DATA \| MEM_READ \| MEM_WRITE` | `.bss` |
| `ScnDiscardable` | `CNT_INITIALIZED_DATA \| MEM_READ \| MEM_DISCARDABLE` | `.reloc`, `.debug` |

### Base Relocation Types — IMAGE_REL_BASED_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_REL_BASED_ABSOLUTE` | `0` | Padding; no fixup. |
| `IMAGE_REL_BASED_HIGH` | `1` | Add high 16 bits of delta to WORD at offset. |
| `IMAGE_REL_BASED_LOW` | `2` | Add low 16 bits of delta to WORD at offset. |
| `IMAGE_REL_BASED_HIGHLOW` | `3` | Add full 32-bit delta to DWORD at offset. |
| `IMAGE_REL_BASED_HIGHADJ` | `4` | High 16 bits; next word holds low adjustment. |
| `IMAGE_REL_BASED_DIR64` | `10` | Add full 64-bit delta to QWORD at offset. |

### Data Directory Indices

| Constant | Index | Section |
|---|---|---|
| `DataDirExport` | `0` | `.edata` — export directory |
| `DataDirImport` | `1` | `.idata` — import directory |
| `DataDirResource` | `2` | `.rsrc` — resource directory |
| `DataDirException` | `3` | `.pdata` — exception table |
| `DataDirSecurity` | `4` | Certificate table (file offset, not RVA) |
| `DataDirBaseReloc` | `5` | `.reloc` — base relocation table |
| `DataDirDebug` | `6` | `.debug` — debug directory |
| `DataDirArchitecture` | `7` | Reserved; must be zero |
| `DataDirGlobalPtr` | `8` | Global pointer register value (size must be 0) |
| `DataDirTLS` | `9` | `.tls` — thread local storage directory |
| `DataDirLoadConfig` | `10` | Load configuration directory |
| `DataDirBoundImport` | `11` | Bound import table |
| `DataDirIAT` | `12` | Import address table |
| `DataDirDelayImport` | `13` | `.didat` — delay-load import descriptors |
| `DataDirCLR` | `14` | `.cormeta` — CLR runtime header |
| `NumDataDirectories` | `16` | Total number of data directory slots |

### COFF Symbol Storage Classes — IMAGE_SYM_CLASS_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_SYM_CLASS_NULL` | `0` | No storage class. |
| `IMAGE_SYM_CLASS_AUTOMATIC` | `1` | Automatic (stack) variable. |
| `IMAGE_SYM_CLASS_EXTERNAL` | `2` | Global or imported symbol. |
| `IMAGE_SYM_CLASS_STATIC` | `3` | Local section symbol or static variable. |
| `IMAGE_SYM_CLASS_REGISTER` | `4` | Register variable. |
| `IMAGE_SYM_CLASS_EXTERNAL_DEF` | `5` | External definition. |
| `IMAGE_SYM_CLASS_LABEL` | `6` | Label. |
| `IMAGE_SYM_CLASS_UNDEFINED_LABEL` | `7` | Undefined label. |
| `IMAGE_SYM_CLASS_MEMBER_OF_STRUCT` | `8` | Member of struct. |
| `IMAGE_SYM_CLASS_FUNCTION` | `101` | `.bf`/`.ef` records. |
| `IMAGE_SYM_CLASS_FILE` | `103` | Source file name. |
| `IMAGE_SYM_CLASS_SECTION` | `104` | Section symbol (aux = section def). |
| `IMAGE_SYM_CLASS_WEAK_EXTERNAL` | `105` | Weak external. |
| `IMAGE_SYM_CLASS_CLR_TOKEN` | `107` | CLR metadata token. |

### COFF Symbol Section Numbers

| Constant | Value | Description |
|---|---|---|
| `IMAGE_SYM_UNDEFINED` | `0` | External reference; defined elsewhere. |
| `IMAGE_SYM_ABSOLUTE` | `-1` | Absolute value; not a section address. |
| `IMAGE_SYM_DEBUG` | `-2` | Debug-info record; no relocation significance. |

### COFF Symbol Types

Base types (low byte of Type field):

| Constant | Value |
|---|---|
| `IMAGE_SYM_TYPE_NULL` | `0` |
| `IMAGE_SYM_TYPE_VOID` | `1` |
| `IMAGE_SYM_TYPE_CHAR` | `2` |
| `IMAGE_SYM_TYPE_SHORT` | `3` |
| `IMAGE_SYM_TYPE_INT` | `6` |
| `IMAGE_SYM_TYPE_LONG` | `7` |
| `IMAGE_SYM_TYPE_FLOAT` | `8` |
| `IMAGE_SYM_TYPE_DOUBLE` | `9` |
| `IMAGE_SYM_TYPE_STRUCT` | `10` |
| `IMAGE_SYM_TYPE_UNION` | `11` |
| `IMAGE_SYM_TYPE_ENUM` | `12` |

Derived types (high byte; shift left by 4 before OR-ing with base type):

| Constant | Value | Description |
|---|---|---|
| `IMAGE_SYM_DTYPE_NULL` | `0` | No derived type. |
| `IMAGE_SYM_DTYPE_POINTER` | `1` | Pointer. |
| `IMAGE_SYM_DTYPE_FUNCTION` | `2` | Function returning base type. |
| `IMAGE_SYM_DTYPE_ARRAY` | `3` | Array of base type. |

Use `MakeCOFFType(base, derived)` to combine them.

### COMDAT Selection Codes

| Constant | Value | Description |
|---|---|---|
| `IMAGE_COMDAT_SELECT_NODUPLICATES` | `1` | Error if multiply defined. |
| `IMAGE_COMDAT_SELECT_ANY` | `2` | Use any definition (most common for inline functions). |
| `IMAGE_COMDAT_SELECT_SAME_SIZE` | `3` | Use any; error if sizes differ. |
| `IMAGE_COMDAT_SELECT_EXACT_MATCH` | `4` | Use any; error if contents differ. |
| `IMAGE_COMDAT_SELECT_ASSOCIATIVE` | `5` | Linked with another COMDAT section. |
| `IMAGE_COMDAT_SELECT_LARGEST` | `6` | Use the largest definition. |

### Weak External Search Characteristics

| Constant | Value | Description |
|---|---|---|
| `IMAGE_WEAK_EXTERN_SEARCH_NOLIBRARY` | `1` | Do not search libraries. |
| `IMAGE_WEAK_EXTERN_SEARCH_LIBRARY` | `2` | Search libraries. |
| `IMAGE_WEAK_EXTERN_SEARCH_ALIAS` | `3` | Symbol is an alias for the TagIndex symbol. |

### Guard Flags — IMAGE_GUARD_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_GUARD_CF_INSTRUMENTED` | `0x00000100` | Module performs CFI checks. |
| `IMAGE_GUARD_CFW_INSTRUMENTED` | `0x00000200` | Module performs CF + write integrity checks. |
| `IMAGE_GUARD_CF_FUNCTION_TABLE_PRESENT` | `0x00000400` | Module contains valid CF target metadata. |
| `IMAGE_GUARD_SECURITY_COOKIE_UNUSED` | `0x00000800` | Module does not use /GS security cookie. |
| `IMAGE_GUARD_PROTECT_DELAYLOAD_IAT` | `0x00001000` | Module protects the delay-load IAT. |
| `IMAGE_GUARD_DELAYLOAD_IAT_IN_ITS_OWN_SECTION` | `0x00002000` | Delay IAT is in its own `.didat` section. |
| `IMAGE_GUARD_CF_EXPORT_SUPPRESSION_INFO_PRESENT` | `0x00004000` | Export suppression info present. |
| `IMAGE_GUARD_CF_ENABLE_EXPORT_SUPPRESSION` | `0x00008000` | Export suppression enabled. |
| `IMAGE_GUARD_CF_LONGJUMP_TABLE_PRESENT` | `0x00010000` | Module has longjmp target table. |
| `IMAGE_GUARD_RF_INSTRUMENTED` | `0x00020000` | Module contains return flow instrumentation. |
| `IMAGE_GUARD_RF_ENABLE` | `0x00040000` | Return flow enforcement enabled. |
| `IMAGE_GUARD_RF_STRICT` | `0x00080000` | Return flow strict mode. |
| `IMAGE_GUARD_RETPOLINE_PRESENT` | `0x00100000` | Module was built with retpoline support. |
| `IMAGE_GUARD_EH_CONTINUATION_TABLE_PRESENT` | `0x00200000` | EH continuation target table present. |
| `IMAGE_GUARD_XFG_ENABLED` | `0x00400000` | eXtended Flow Guard enabled. |
| `IMAGE_GUARD_CF_FUNCTION_TABLE_SIZE_MASK` | `0xF0000000` | Stride of CFG function table entries. |
| `IMAGE_GUARD_CF_FUNCTION_TABLE_SIZE_SHIFT` | `28` | Shift for the size mask field. |

### Debug Types — IMAGE_DEBUG_TYPE_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_DEBUG_TYPE_UNKNOWN` | `0` | |
| `IMAGE_DEBUG_TYPE_COFF` | `1` | COFF debug info. |
| `IMAGE_DEBUG_TYPE_CODEVIEW` | `2` | CodeView (PDB). Use `BuildCodeViewPDB`. |
| `IMAGE_DEBUG_TYPE_FPO` | `3` | Frame Pointer Omission info. |
| `IMAGE_DEBUG_TYPE_MISC` | `4` | Miscellaneous debug info (DBG file path). |
| `IMAGE_DEBUG_TYPE_EXCEPTION` | `5` | Exception table copy. |
| `IMAGE_DEBUG_TYPE_FIXUP` | `6` | Fixup table. |
| `IMAGE_DEBUG_TYPE_OMAP_TO_SRC` | `7` | Address mapping to source. |
| `IMAGE_DEBUG_TYPE_OMAP_FROM_SRC` | `8` | Address mapping from source. |
| `IMAGE_DEBUG_TYPE_BORLAND` | `9` | Borland. |
| `IMAGE_DEBUG_TYPE_CLSID` | `11` | CLSID of the DLL. |
| `IMAGE_DEBUG_TYPE_VC_FEATURE` | `12` | VC++ feature counts. Use `BuildVCFeatureEntry`. |
| `IMAGE_DEBUG_TYPE_POGO` | `13` | Profile-guided optimisation data. |
| `IMAGE_DEBUG_TYPE_ILTCG` | `14` | Incremental link-time code generation. |
| `IMAGE_DEBUG_TYPE_MPX` | `15` | Intel MPX. |
| `IMAGE_DEBUG_TYPE_REPRO` | `16` | Reproducible build hash. Use `BuildReproEntry`. |
| `IMAGE_DEBUG_TYPE_EMBED_PDB` | `17` | Embedded portable PDB. |
| `IMAGE_DEBUG_TYPE_SPGO` | `18` | Sample-profile guided optimisation. |
| `IMAGE_DEBUG_TYPE_EX_DLLCHARACTERISTICS` | `20` | Extended DLL characteristics. |

### Import Kind and Name Types

Kind:

| Constant | Value | Description |
|---|---|---|
| `IMPORT_CODE` | `0` | Import is a code symbol (function). |
| `IMPORT_DATA` | `1` | Import is a data symbol. |
| `IMPORT_CONST` | `2` | Import is a const symbol. |

Name type:

| Constant | Value | Description |
|---|---|---|
| `IMPORT_ORDINAL` | `0` | Import by ordinal. |
| `IMPORT_NAME` | `1` | Import by name (symbol name in stub). |
| `IMPORT_NAME_NOPREFIX` | `2` | Import by name, strip leading `?` or `@`. |
| `IMPORT_NAME_UNDECORATE` | `3` | Import by name, strip decorations up to first `@`. |
| `IMPORT_NAME_EXPORTAS` | `4` | Import by name specified in `ExportName` field. |

### Unwind Operation Codes — UWOP_*

| Constant | Value | Description |
|---|---|---|
| `UWOP_PUSH_NONVOL` | `0` | Push a nonvolatile integer register. |
| `UWOP_ALLOC_LARGE` | `1` | Allocate a large area on the stack (OpInfo=0: 2 slots, =1: 3 slots). |
| `UWOP_ALLOC_SMALL` | `2` | Allocate a small area on the stack (8–128 bytes, 1 slot). |
| `UWOP_SET_FPREG` | `3` | Establish the frame pointer register. |
| `UWOP_SAVE_NONVOL` | `4` | Save nonvolatile register on stack using MOV (2 slots). |
| `UWOP_SAVE_NONVOL_FAR` | `5` | Save nonvolatile register with large offset (3 slots). |
| `UWOP_EPILOG` | `6` | Describe the epilog (2 slots). |
| `UWOP_SAVE_XMM128` | `8` | Save all 128 bits of nonvolatile XMM register using MOVAPS (2 slots). |
| `UWOP_SAVE_XMM128_FAR` | `9` | Save all 128 bits of XMM with large offset (3 slots). |
| `UWOP_PUSH_MACHFRAME` | `10` | Push a machine frame (interrupt/exception handlers). |

### Unwind Flags — UNW_FLAG_*

| Constant | Value | Description |
|---|---|---|
| `UNW_FLAG_NHANDLER` | `0x0` | No handler. |
| `UNW_FLAG_EHANDLER` | `0x1` | Function has an exception handler. |
| `UNW_FLAG_UHANDLER` | `0x2` | Function has a termination handler. |
| `UNW_FLAG_CHAININFO` | `0x4` | Chained unwind info; no handler fields present. |

### x64 Register Numbers

```go
const (
    RegRAX = uint8(0);  RegRCX = uint8(1);  RegRDX = uint8(2);  RegRBX = uint8(3)
    RegRSP = uint8(4);  RegRBP = uint8(5);  RegRSI = uint8(6);  RegRDI = uint8(7)
    RegR8  = uint8(8);  RegR9  = uint8(9);  RegR10 = uint8(10); RegR11 = uint8(11)
    RegR12 = uint8(12); RegR13 = uint8(13); RegR14 = uint8(14); RegR15 = uint8(15)
)
```

### COFF AMD64 Relocations — IMAGE_REL_AMD64_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_REL_AMD64_ABSOLUTE` | `0x0000` | No-op; used for padding. |
| `IMAGE_REL_AMD64_ADDR64` | `0x0001` | 8-byte VA. Produces `BASED_DIR64` base reloc. |
| `IMAGE_REL_AMD64_ADDR32` | `0x0002` | 4-byte VA (must fit in 32 bits). Produces `BASED_HIGHLOW` base reloc. |
| `IMAGE_REL_AMD64_ADDR32NB` | `0x0003` | 4-byte RVA (image-relative; no base reloc). |
| `IMAGE_REL_AMD64_REL32` | `0x0004` | 4-byte signed PC-relative offset. Used for CALL/JMP. |
| `IMAGE_REL_AMD64_REL32_1` | `0x0005` | REL32 with 1 extra byte of instruction payload. |
| `IMAGE_REL_AMD64_REL32_2` | `0x0006` | REL32 with 2 extra bytes. |
| `IMAGE_REL_AMD64_REL32_3` | `0x0007` | REL32 with 3 extra bytes. |
| `IMAGE_REL_AMD64_REL32_4` | `0x0008` | REL32 with 4 extra bytes. |
| `IMAGE_REL_AMD64_REL32_5` | `0x0009` | REL32 with 5 extra bytes. |
| `IMAGE_REL_AMD64_SECTION` | `0x000A` | 2-byte 1-based section index (debug info). |
| `IMAGE_REL_AMD64_SECREL` | `0x000B` | 4-byte offset from beginning of section. |
| `IMAGE_REL_AMD64_SECREL7` | `0x000C` | Low 7 bits of a byte with 7-bit section-relative offset. |
| `IMAGE_REL_AMD64_TOKEN` | `0x000D` | 4-byte CLR metadata token. |
| `IMAGE_REL_AMD64_SREL32` | `0x000E` | Span-dependent signed 32-bit relocation; must be followed by PAIR. |
| `IMAGE_REL_AMD64_PAIR` | `0x000F` | Must immediately follow an SREL32 record. |
| `IMAGE_REL_AMD64_SSPAN32` | `0x0010` | Span-dependent signed 32-bit relocation applied to an instruction displacement. |

### COFF ARM64 Relocations — IMAGE_REL_ARM64_*

| Constant | Value | Description |
|---|---|---|
| `IMAGE_REL_ARM64_ABSOLUTE` | `0x0000` | No-op; used for padding. |
| `IMAGE_REL_ARM64_ADDR32` | `0x0001` | 4-byte VA of the target. |
| `IMAGE_REL_ARM64_ADDR32NB` | `0x0002` | 4-byte RVA of the target. |
| `IMAGE_REL_ARM64_BRANCH26` | `0x0003` | 26-bit displacement for B / BL. |
| `IMAGE_REL_ARM64_PAGEBASE_REL21` | `0x0004` | 21-bit page-relative offset for ADRP. |
| `IMAGE_REL_ARM64_REL21` | `0x0005` | 21-bit PC-relative offset for ADR. |
| `IMAGE_REL_ARM64_PAGEOFFSET_12A` | `0x0006` | 12-bit page offset for ADD (after ADRP). |
| `IMAGE_REL_ARM64_PAGEOFFSET_12L` | `0x0007` | 12-bit scaled page offset for LDR/STR (after ADRP). |
| `IMAGE_REL_ARM64_SECREL` | `0x0008` | 4-byte section-relative offset. |
| `IMAGE_REL_ARM64_SECREL_LOW12A` | `0x0009` | Bits [11:0] of section-relative offset for ADD. |
| `IMAGE_REL_ARM64_SECREL_HIGH12A` | `0x000A` | Bits [23:12] of section-relative offset for ADD. |
| `IMAGE_REL_ARM64_SECREL_LOW12L` | `0x000B` | Bits [11:0] of section-relative offset for LDR/STR (scaled). |
| `IMAGE_REL_ARM64_TOKEN` | `0x000C` | 4-byte CLR metadata token. |
| `IMAGE_REL_ARM64_SECTION` | `0x000D` | 2-byte 1-based section index (debug info). |
| `IMAGE_REL_ARM64_ADDR64` | `0x000E` | 8-byte VA of the target. |
| `IMAGE_REL_ARM64_BRANCH19` | `0x000F` | 19-bit displacement for CBZ, CBNZ, B.cond. |
| `IMAGE_REL_ARM64_BRANCH14` | `0x0010` | 14-bit displacement for TBZ/TBNZ. |
| `IMAGE_REL_ARM64_REL32` | `0x0011` | 4-byte signed PC-relative offset. |