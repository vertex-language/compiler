# bin/macho

```
github.com/vertex-language/compiler/bin/macho
```

Constructs and serialises valid 64-bit Mach-O binaries for macOS and iOS. Given segments, sections, symbols, and dynamic-linking tables, `Emit()` produces a well-formed binary ready to execute or link against. The package handles segment layout, string tables, symbol tables, indirect-symbol tables, dyld rebase/bind opcode encoding, export-trie construction, chained-fixups encoding (macOS 12+), function-starts tables, and all required load commands.

---

## Quick start

```go
b := macho.NewBuilder(macho.ArchARM64)
b.SetFileType(macho.FileTypeExecute)
b.SetBuildVersion(macho.BuildVersion{
    Platform: macho.PlatformMacOS,
    MinOS:    macho.PackVersion(14, 0, 0),
    SDK:      macho.PackVersion(14, 5, 0),
    Tools:    []macho.BuildToolVersion{{Tool: macho.ToolLD, Version: macho.PackVersion(1015, 7, 0)}},
})
b.AddDylib(macho.DylibRef{
    Path:           "/usr/lib/libSystem.B.dylib",
    Kind:           macho.DylibLoad,
    CurrentVersion: macho.PackVersion(1319, 0, 0),
    CompatVersion:  macho.PackVersion(1, 0, 0),
})
b.AddSegment(macho.Segment{
    Name:     "__TEXT",
    InitProt: macho.ProtRead | macho.ProtExec,
    Sections: []macho.Section{
        {
            Name:  "__text",
            Data:  machineCode,
            Align: 4,
            Flags: macho.S_REGULAR | macho.S_ATTR_PURE_INSTRUCTIONS | macho.S_ATTR_SOME_INSTRUCTIONS,
        },
    },
})
b.AddSymbol(macho.Symbol{
    Name: "_main", SegmentName: "__TEXT", SectionName: "__text",
    Value: 0, Global: true,
})
b.SetEntry("_main")

out, err := b.Emit()
if err != nil {
    log.Fatal(err)
}
os.WriteFile("program", out, 0o755)
```

---

## Builder

### Construction

```go
func NewBuilder(arch Arch) *Builder
```

Returns a `Builder` targeting the given architecture. Default file type is `FileTypeExecute`; default dyld mode is `DyldModeChained`.

---

### Architecture

```go
type Arch uint32

const (
    ArchAMD64 Arch = (1 << 24) | 7   // CPU_TYPE_X86_64
    ArchARM64 Arch = (1 << 24) | 12  // CPU_TYPE_ARM64
)
```

---

### File type

```go
func (b *Builder) SetFileType(ft FileType)
```

```go
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
```

---

### Header flags

```go
func (b *Builder) SetFlags(f MHFlags)
```

The Builder automatically ORs in `MHDyldLink | MHTwoLevel | MHPie | MHNoUndefs` as appropriate. Use `SetFlags` only for additional flags you need explicitly.

```go
type MHFlags uint32

const (
    MHNoUndefs                   MHFlags = 0x00000001 // no undefined symbols
    MHIncrLink                   MHFlags = 0x00000002 // incremental link output
    MHDyldLink                   MHFlags = 0x00000004 // input for dyld
    MHBindAtLoad                 MHFlags = 0x00000008 // bind all undefined symbols at load time
    MHPrebound                   MHFlags = 0x00000010 // prebound image (deprecated)
    MHSplitSegs                  MHFlags = 0x00000020 // read-only and read-write segs split
    MHTwoLevel                   MHFlags = 0x00000080 // two-level namespace bindings
    MHForceFlat                  MHFlags = 0x00000100 // force flat namespace
    MHNoMultiDefs                MHFlags = 0x00000200
    MHNoFixPrebinding            MHFlags = 0x00000400
    MHPrebindable                MHFlags = 0x00000800
    MHAllModsBound               MHFlags = 0x00001000 // all two-level modules bound
    MHSubsectionsViaSymbols      MHFlags = 0x00002000
    MHCanonical                  MHFlags = 0x00004000 // canonicalised via unprebind
    MHWeakDefines                MHFlags = 0x00008000 // has external weak symbols
    MHBindsToWeak                MHFlags = 0x00010000 // uses weak symbols
    MHAllowStackExec             MHFlags = 0x00020000
    MHRootSafe                   MHFlags = 0x00040000
    MHSetUIDSafe                 MHFlags = 0x00080000
    MHNoReexportedDylibs         MHFlags = 0x00100000
    MHPie                        MHFlags = 0x00200000 // ASLR main executable
    MHDeadStrippableDylib        MHFlags = 0x00400000
    MHHasTLVDescriptors          MHFlags = 0x00800000
    MHNoHeapExecution            MHFlags = 0x01000000
    MHAppExtensionSafe           MHFlags = 0x02000000
    MHNlistOutofsyncWithDyldinfo MHFlags = 0x04000000
    MHSimSupport                 MHFlags = 0x08000000 // built for simulator
    MHDylibInCache               MHFlags = 0x80000000 // in shared cache
)
```

---

### Segments

```go
func (b *Builder) AddSegment(seg Segment)
```

Appends a segment with all its sections. Call order determines VM layout. `__PAGEZERO` and `__LINKEDIT` are synthesised automatically.

```go
type Segment struct {
    Name     string   // e.g. "__TEXT", "__DATA"; truncated to 16 bytes
    InitProt Prot     // initial VM protection
    MaxProt  Prot     // maximum VM protection; 0 defaults to InitProt
    Flags    SegFlags // SG_* flags
    Sections []Section
}
```

**VM protection (`vm_prot_t`):**

```go
type Prot uint32

const (
    ProtNone  Prot = 0x00 // VM_PROT_NONE
    ProtRead  Prot = 0x01 // VM_PROT_READ
    ProtWrite Prot = 0x02 // VM_PROT_WRITE
    ProtExec  Prot = 0x04 // VM_PROT_EXECUTE
)
```

**Segment flags:**

```go
type SegFlags uint32

const (
    SegHighVM   SegFlags = 0x1 // SG_HIGHVM    — file contents at high VM addresses
    SegFVMLib   SegFlags = 0x2 // SG_FVMLIB    — fixed VM library segment
    SegNoReloc  SegFlags = 0x4 // SG_NORELOC   — no relocs in segment
    SegReadOnly SegFlags = 0x8 // SG_READ_ONLY — dyld will mprotect to r/o after init
)
```

**Standard macOS segments:**

| Segment        | InitProt               | SegFlags      | Purpose                                               |
|----------------|------------------------|---------------|-------------------------------------------------------|
| `__TEXT`       | `ProtRead\|ProtExec`   | —             | Code, read-only data, Mach-O header                   |
| `__DATA_CONST` | `ProtRead\|ProtWrite`  | `SegReadOnly` | Non-lazy GOT, ObjC metadata (locked r/o after init)   |
| `__DATA`       | `ProtRead\|ProtWrite`  | —             | Globals, writable data                                |
| `__OBJC`       | `ProtRead\|ProtWrite`  | —             | Objective-C runtime tables                            |

---

### Sections

```go
type Section struct {
    Name      string  // e.g. "__text", "__data"; truncated to 16 bytes
    Data      []byte  // raw content; nil/empty for zerofill sections
    Size      uint64  // in-memory size for zerofill sections; ignored when Data != nil
    Align     uint32  // byte count, power of two; 0 → 1
    Flags     uint32  // S_* type OR'd with S_ATTR_* attributes
    Reserved1 uint32  // first indirect-symbol-table index (stub/pointer sections)
    Reserved2 uint32  // stub entry size (S_SYMBOL_STUBS only)
    Relocs    []Reloc // relocation records; meaningful for MH_OBJECT output only
}
```

**Section type constants** (low byte of `Flags`):

```go
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
```

**Section attribute constants** (upper 3 bytes of `Flags`):

```go
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
```

**Common section configurations:**

```go
// Executable code
macho.Section{Name: "__text",
    Flags: macho.S_REGULAR | macho.S_ATTR_PURE_INSTRUCTIONS | macho.S_ATTR_SOME_INSTRUCTIONS,
    Data: code, Align: 4}

// Read-only C strings
macho.Section{Name: "__cstring",
    Flags: macho.S_CSTRING_LITERALS,
    Data: cstrings, Align: 1}

// Read-only constants
macho.Section{Name: "__const",
    Flags: macho.S_REGULAR,
    Data: rodata, Align: 8}

// Non-lazy symbol pointers (__got)
macho.Section{Name: "__got",
    Flags: macho.S_NON_LAZY_SYMBOL_POINTERS,
    Data: gotSlots, Align: 8, Reserved1: firstIndirectIdx}

// Lazy symbol pointers
macho.Section{Name: "__la_symbol_ptr",
    Flags: macho.S_LAZY_SYMBOL_POINTERS,
    Data: laSlots, Align: 8, Reserved1: firstIndirectIdx}

// PLT stubs (ARM64: 12 bytes each; AMD64: 6 bytes each)
macho.Section{Name: "__stubs",
    Flags: macho.S_SYMBOL_STUBS | macho.S_ATTR_PURE_INSTRUCTIONS | macho.S_ATTR_SOME_INSTRUCTIONS,
    Data: stubs, Align: 2, Reserved1: firstIndirectIdx, Reserved2: 12}

// Writable initialised data
macho.Section{Name: "__data",
    Flags: macho.S_REGULAR,
    Data: data, Align: 8}

// Zero-fill BSS
macho.Section{Name: "__bss",
    Flags: macho.S_ZEROFILL,
    Size: 4096, Align: 4}

// Constructor function pointers
macho.Section{Name: "__mod_init_func",
    Flags: macho.S_MOD_INIT_FUNC_POINTERS,
    Data: ctors, Align: 8}

// Destructor function pointers
macho.Section{Name: "__mod_term_func",
    Flags: macho.S_MOD_TERM_FUNC_POINTERS,
    Data: dtors, Align: 8}

// Thread-local storage initialised template
macho.Section{Name: "__thread_data",
    Flags: macho.S_THREAD_LOCAL_REGULAR,
    Data: tlsData, Align: 8}

// Thread-local zero-fill template
macho.Section{Name: "__thread_bss",
    Flags: macho.S_THREAD_LOCAL_ZEROFILL,
    Size: tlsBSSSize, Align: 8}

// Thread-local variable descriptors
macho.Section{Name: "__thread_vars",
    Flags: macho.S_THREAD_LOCAL_VARIABLES,
    Data: tlsVars, Align: 8}

// Thread-local variable pointers
macho.Section{Name: "__thread_ptrs",
    Flags: macho.S_THREAD_LOCAL_VARIABLE_POINTERS,
    Data: tlsPtrs, Align: 8}

// Thread-local init functions
macho.Section{Name: "__thread_init",
    Flags: macho.S_THREAD_LOCAL_INIT_FUNCTION_POINTERS,
    Data: tlsInits, Align: 8}

// ObjC method name strings
macho.Section{Name: "__objc_methnames",
    Flags: macho.S_CSTRING_LITERALS,
    Data: methNames, Align: 1}

// Interposing pairs
macho.Section{Name: "__interpose",
    Flags: macho.S_INTERPOSING,
    Data: interposePairs, Align: 8}
```

---

### Symbols

```go
func (b *Builder) AddSymbol(sym Symbol)
```

Local symbols must be added before global symbols.

```go
type Symbol struct {
    Name          string // mangled name, e.g. "_main", "__ZN3Foo3barEv"
    SegmentName   string // home segment; "" = undefined/external
    SectionName   string // home section; "" = undefined/external
    Value         uint64 // byte offset from section start; resolved to VA by Builder
    Global        bool   // N_EXT — externally visible
    Weak          bool   // N_WEAK_DEF (defined) or N_WEAK_REF (undefined)
    AltEntry      bool   // N_ALT_ENTRY — alternate entry point (Swift thunks)
    PrivateExtern bool   // N_PEXT — module-scoped; global within dylib but not exported
    Desc          uint16 // raw n_desc bits; Builder ORs in computed flag bits
}
```

```go
// Defined global function
macho.Symbol{Name: "_main", SegmentName: "__TEXT", SectionName: "__text", Value: 0, Global: true}

// Defined weak function (can be overridden by another image)
macho.Symbol{Name: "_myMalloc", SegmentName: "__TEXT", SectionName: "__text", Value: 64, Global: true, Weak: true}

// Defined private external (visible within dylib, not exported)
macho.Symbol{Name: "_internal", SegmentName: "__TEXT", SectionName: "__text", Value: 128, PrivateExtern: true}

// Undefined external reference (import)
macho.Symbol{Name: "_printf", Global: true}

// Undefined weak external reference (optional import)
macho.Symbol{Name: "_optionalFn", Global: true, Weak: true}

// Alternate entry point (Swift thunks)
macho.Symbol{Name: "$s3FooBarV", SegmentName: "__TEXT", SectionName: "__text", Value: 0, Global: true, AltEntry: true}
```

---

### Dylib references

```go
func (b *Builder) AddDylib(ref DylibRef)
```

Call order determines the 1-based library ordinal used in bind tables.

```go
type DylibRef struct {
    Path           string    // install name, e.g. "/usr/lib/libSystem.B.dylib" or "@rpath/libFoo.dylib"
    Kind           DylibKind // selects the LC_LOAD_* command type
    CurrentVersion uint32    // PackVersion(major, minor, patch)
    CompatVersion  uint32    // PackVersion(major, minor, patch)
}

type DylibKind uint8

const (
    DylibLoad     DylibKind = iota // LC_LOAD_DYLIB       — required at startup
    DylibWeak                      // LC_LOAD_WEAK_DYLIB  — optional; missing dylib is ok
    DylibReexport                  // LC_REEXPORT_DYLIB   — re-export all symbols upstream
    DylibLazy                      // LC_LAZY_LOAD_DYLIB  — defer load until first symbol use
    DylibUpward                    // LC_LOAD_UPWARD_DYLIB — used in umbrella frameworks
)
```

```go
// Required system library (ordinal 1)
b.AddDylib(macho.DylibRef{
    Path:           "/usr/lib/libSystem.B.dylib",
    Kind:           macho.DylibLoad,
    CurrentVersion: macho.PackVersion(1319, 0, 0),
    CompatVersion:  macho.PackVersion(1, 0, 0),
})

// Optional dependency (ordinal 2)
b.AddDylib(macho.DylibRef{
    Path: "/usr/lib/libcurl.dylib",
    Kind: macho.DylibWeak,
})

// Re-exported sub-library (ordinal 3)
b.AddDylib(macho.DylibRef{
    Path:          "/usr/lib/libc++.1.dylib",
    Kind:          macho.DylibReexport,
    CompatVersion: macho.PackVersion(1, 0, 0),
})

// Lazily loaded library (ordinal 4)
b.AddDylib(macho.DylibRef{
    Path: "@rpath/libOptional.dylib",
    Kind: macho.DylibLazy,
})
```

---

### Dylib identity

```go
func (b *Builder) SetDylibID(ref DylibRef)
```

Sets `LC_ID_DYLIB` for the image's own install name. Required for `FileTypeDylib`; has no effect on `FileTypeExecute`.

```go
b.SetDylibID(macho.DylibRef{
    Path:           "@rpath/libFoo.dylib",
    CurrentVersion: macho.PackVersion(1, 2, 3),
    CompatVersion:  macho.PackVersion(1, 0, 0),
})
```

---

### Entry point

```go
func (b *Builder) SetEntry(name string)
```

Required for `FileTypeExecute`. Emits `LC_MAIN`. The named symbol must be present in the symbol table and resolve to a section VA.

---

### Runtime search paths

```go
func (b *Builder) AddRpath(path string)
```

Appends `LC_RPATH`. Standard macOS patterns:

```go
b.AddRpath("@executable_path/../Frameworks")
b.AddRpath("@loader_path/Frameworks")
b.AddRpath("/usr/local/lib")
```

---

### Build version

```go
func (b *Builder) SetBuildVersion(bv BuildVersion)
```

Emits `LC_BUILD_VERSION`.

```go
type BuildVersion struct {
    Platform Platform
    MinOS    uint32             // use PackVersion
    SDK      uint32             // use PackVersion
    Tools    []BuildToolVersion // optional tool-chain metadata
}

type BuildToolVersion struct {
    Tool    Tool
    Version uint32 // use PackVersion
}

type Platform uint32

const (
    PlatformMacOS            Platform = 1
    PlatformIOS              Platform = 2
    PlatformTVOS             Platform = 3
    PlatformWatchOS          Platform = 4
    PlatformBridgeOS         Platform = 5
    PlatformMacCatalyst      Platform = 6
    PlatformIOSSimulator     Platform = 7
    PlatformTVOSSimulator    Platform = 8
    PlatformWatchOSSimulator Platform = 9
    PlatformDriverKit        Platform = 10
    PlatformVisionOS         Platform = 11
    PlatformVisionSimulator  Platform = 12
)

type Tool uint32

const (
    ToolClang Tool = 1
    ToolSwift Tool = 2
    ToolLD    Tool = 3
    ToolLLD   Tool = 4
)
```

```go
b.SetBuildVersion(macho.BuildVersion{
    Platform: macho.PlatformMacOS,
    MinOS:    macho.PackVersion(14, 0, 0),
    SDK:      macho.PackVersion(14, 5, 0),
    Tools: []macho.BuildToolVersion{
        {Tool: macho.ToolLD,    Version: macho.PackVersion(1015, 7, 0)},
        {Tool: macho.ToolClang, Version: macho.PackVersion(15, 0, 0)},
    },
})
```

---

### Source version

```go
func (b *Builder) SetSourceVersion(v uint64)
```

Emits `LC_SOURCE_VERSION`. Use `PackSourceVersion` to construct the value.

```go
b.SetSourceVersion(macho.PackSourceVersion(1, 2, 3, 4, 5))
```

---

### UUID

```go
func (b *Builder) SetUUID(uuid [16]byte)
```

Embeds `LC_UUID`. If never called, the Builder omits `LC_UUID`.

```go
var uuid [16]byte
_, _ = rand.Read(uuid[:])
uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant bits
b.SetUUID(uuid)
```

---

### Linker options

```go
func (b *Builder) AddLinkerOption(opts []string)
```

Embeds `LC_LINKER_OPTION`. Used by object files to automatically pass flags to the static linker when the object is linked into a final image.

```go
b.AddLinkerOption([]string{"-framework", "Foundation"})
b.AddLinkerOption([]string{"-lc++"})
```

---

### Emit

```go
func (b *Builder) Emit() ([]byte, error)
```

Serialises the complete Mach-O image and returns the raw bytes. Returns an error if the builder state is invalid (e.g. unsupported architecture, or `FileTypeExecute` without a call to `SetEntry`).

---

## Dynamic linking

### DyldMode

```go
func (b *Builder) SetDyldMode(m DyldMode)

type DyldMode uint8

const (
    DyldModeLegacy  DyldMode = iota // LC_DYLD_INFO_ONLY      — macOS < 12 / iOS < 14
    DyldModeChained                 // LC_DYLD_CHAINED_FIXUPS — macOS 12+ / iOS 14+ (default)
)
```

Calling `SetDyldInfo` automatically switches to `DyldModeLegacy`. Calling `SetChainedFixups` automatically switches to `DyldModeChained`.

---

### Legacy mode — DyldInfoBuilder

Used for targets running macOS < 12 or iOS < 14. Produces five `__LINKEDIT` blobs: rebase, bind, weak-bind, lazy-bind, and export trie.

```go
func NewDyldInfoBuilder() *DyldInfoBuilder

func (d *DyldInfoBuilder) AddRebase(e RebaseEntry)
func (d *DyldInfoBuilder) AddBind(e BindEntry)
func (d *DyldInfoBuilder) AddExport(e ExportEntry)
func (d *DyldInfoBuilder) Build() (rebase, bind, weakBind, lazyBind, exportTrie []byte)
```

```go
func (b *Builder) SetDyldInfo(rebase, bind, weakBind, lazyBind, exportTrie []byte)
```

**RebaseEntry** — an address requiring an ASLR slide fixup:

```go
type RebaseEntry struct {
    SegIndex  int        // zero-based segment index (excluding __PAGEZERO)
    SegOffset uint64     // byte offset from segment start to the pointer word
    Type      RebaseType
}

type RebaseType uint8

const (
    RebaseTypePointer        RebaseType = 1 // REBASE_TYPE_POINTER
    RebaseTypeTextAbsolute32 RebaseType = 2 // REBASE_TYPE_TEXT_ABSOLUTE32
    RebaseTypeTextPCRel32    RebaseType = 3 // REBASE_TYPE_TEXT_PCREL32
)
```

**BindEntry** — an external-symbol binding operation:

```go
type BindEntry struct {
    SegIndex   int      // zero-based segment index (excluding __PAGEZERO)
    SegOffset  uint64   // byte offset from segment start to the pointer word
    LibOrdinal int      // 1-based dylib index, or a BindSpecial* value cast to int
    Name       string   // symbol name
    Type       BindType
    Addend     int64
    Weak       bool     // true → placed in the weak-bind table
    Lazy       bool     // true → placed in the lazy-bind table (PLT backing)
}

type BindType uint8

const (
    BindTypePointer        BindType = 1 // BIND_TYPE_POINTER
    BindTypeTextAbsolute32 BindType = 2 // BIND_TYPE_TEXT_ABSOLUTE32
    BindTypeTextPCRel32    BindType = 3 // BIND_TYPE_TEXT_PCREL32
)

type BindSpecial int

const (
    BindSpecialSelf        BindSpecial = 0  // BIND_SPECIAL_DYLIB_SELF
    BindSpecialMainExec    BindSpecial = -1 // BIND_SPECIAL_DYLIB_MAIN_EXECUTABLE
    BindSpecialFlatLookup  BindSpecial = -2 // BIND_SPECIAL_DYLIB_FLAT_LOOKUP
    BindSpecialWeakLookup  BindSpecial = -3 // BIND_SPECIAL_DYLIB_WEAK_LOOKUP
)
```

**Full legacy example:**

```go
d := macho.NewDyldInfoBuilder()

// ASLR rebase
d.AddRebase(macho.RebaseEntry{SegIndex: 1, SegOffset: 0x10, Type: macho.RebaseTypePointer})

// Regular bind
d.AddBind(macho.BindEntry{
    SegIndex: 1, SegOffset: 0x00,
    LibOrdinal: 1, Name: "_printf",
    Type: macho.BindTypePointer,
})

// Lazy bind (PLT stub backing)
d.AddBind(macho.BindEntry{
    SegIndex: 1, SegOffset: 0x18,
    LibOrdinal: 1, Name: "_exit",
    Type: macho.BindTypePointer,
    Lazy: true,
})

// Weak bind
d.AddBind(macho.BindEntry{
    SegIndex: 1, SegOffset: 0x20,
    LibOrdinal: int(macho.BindSpecialWeakLookup),
    Name: "_malloc",
    Type: macho.BindTypePointer,
    Weak: true,
})

// Exports
d.AddExport(macho.ExportEntry{Name: "_main",   Address: 0,  Flags: macho.ExportKindRegular})
d.AddExport(macho.ExportEntry{Name: "_helper", Address: 64, Flags: macho.ExportKindRegular | macho.ExportWeakDefinition})

rebase, bind, weakBind, lazyBind, exportTrie := d.Build()
b.SetDyldInfo(rebase, bind, weakBind, lazyBind, exportTrie)
```

---

### Modern mode — ChainedFixupsBuilder

Used for macOS 12+ / iOS 14+. No lazy binding; all binds happen at launch via in-page pointer chains.

```go
func NewChainedFixupsBuilder(format ChainedPtrFormat, pageSize uint32) *ChainedFixupsBuilder

func (c *ChainedFixupsBuilder) AddBindTarget(t BindTarget) int   // returns ordinal index
func (c *ChainedFixupsBuilder) AddRebase(r ChainedRebase)
func (c *ChainedFixupsBuilder) AddBind(b ChainedBind)
func (c *ChainedFixupsBuilder) Build(segFileOffsets []uint64, segVMSizes []uint64) []byte
```

```go
func (b *Builder) SetChainedFixups(data []byte)
```

```go
type BindTarget struct {
    LibOrdinal int    // 1-based into the LC_LOAD_DYLIB list, or a BindSpecial* value
    Name       string
    Addend     int64
    WeakImport bool
}

type ChainedRebase struct {
    SegIndex  int    // zero-based, matching AddSegment order (excluding __PAGEZERO)
    SegOffset uint64 // byte offset from segment start to the pointer word
}

type ChainedBind struct {
    SegIndex  int
    SegOffset uint64
    TargetIdx int   // index returned by AddBindTarget
    Addend    int64
}
```

**ChainedPtrFormat constants:**

```go
type ChainedPtrFormat uint16

const (
    ChainedPtrArm64e            ChainedPtrFormat = 1  // arm64e authenticated/plain 64-bit
    ChainedPtr64                ChainedPtrFormat = 2  // plain 64-bit (arm64/x86-64 non-PIE)
    ChainedPtr32                ChainedPtrFormat = 3  // 32-bit chained pointer
    ChainedPtr32Cache           ChainedPtrFormat = 4  // 32-bit dyld shared cache relative
    ChainedPtr32FirmwareIF      ChainedPtrFormat = 7  // 32-bit firmware intermediate format
    ChainedPtr64Offset          ChainedPtrFormat = 6  // 64-bit offset-based (arm64 modern default)
    ChainedPtrX8664KernelCache  ChainedPtrFormat = 8  // kernel cache pointer format
    ChainedPtrArm64eKernel      ChainedPtrFormat = 9  // arm64e kernel format
    ChainedPtr64KernelCache     ChainedPtrFormat = 10 // 64-bit kernel cache
    ChainedPtrArm64eUserland    ChainedPtrFormat = 12 // arm64e userland with bind/rebase metadata
    ChainedPtrArm64eUserland24  ChainedPtrFormat = 13 // arm64e userland with 24-bit bind ordinal
)
```

Recommended page sizes: `0x4000` for ARM64 macOS userland, `0x1000` for AMD64 macOS userland.

**Full modern example:**

```go
cf := macho.NewChainedFixupsBuilder(macho.ChainedPtr64Offset, 0x4000)

printfIdx := cf.AddBindTarget(macho.BindTarget{LibOrdinal: 1, Name: "_printf"})
exitIdx   := cf.AddBindTarget(macho.BindTarget{LibOrdinal: 1, Name: "_exit"})

// Optional / weak import
mallocIdx := cf.AddBindTarget(macho.BindTarget{LibOrdinal: 1, Name: "_malloc", WeakImport: true})

// Bind pointers
cf.AddBind(macho.ChainedBind{SegIndex: 1, SegOffset: 0x00, TargetIdx: printfIdx})
cf.AddBind(macho.ChainedBind{SegIndex: 1, SegOffset: 0x08, TargetIdx: exitIdx})
cf.AddBind(macho.ChainedBind{SegIndex: 1, SegOffset: 0x10, TargetIdx: mallocIdx})

// Rebase pointer
cf.AddRebase(macho.ChainedRebase{SegIndex: 1, SegOffset: 0x18})

// segFileOffsets and segVMSizes must match the order of AddSegment calls, excluding __PAGEZERO
data := cf.Build(segFileOffsets, segVMSizes)
b.SetChainedFixups(data)
```

---

### Export trie

```go
func BuildExportTrie(exports []ExportEntry) []byte
```

Builds a standalone export trie for `LC_DYLD_EXPORTS_TRIE` (modern mode) or as the export chunk of `LC_DYLD_INFO_ONLY` (legacy mode).

```go
func (b *Builder) SetExportsTrie(data []byte)
```

```go
type ExportEntry struct {
    Name               string
    Address            uint64      // VM offset from image base (no slide); 0 for re-exports
    Flags              ExportFlags

    // Used when ExportReexport is set:
    ReexportLibOrdinal int
    ReexportName       string      // "" = same name in the re-exported dylib

    // Used when ExportStubAndResolver is set:
    StubOffset     uint64
    ResolverOffset uint64
}

type ExportFlags uint64

const (
    // Kind bits (low 2 bits)
    ExportKindRegular     ExportFlags = 0x00 // EXPORT_SYMBOL_FLAGS_KIND_REGULAR
    ExportKindAbsolute    ExportFlags = 0x02 // EXPORT_SYMBOL_FLAGS_KIND_ABSOLUTE
    ExportKindThreadLocal ExportFlags = 0x03 // EXPORT_SYMBOL_FLAGS_KIND_THREAD_LOCAL

    // Attribute bits
    ExportWeakDefinition  ExportFlags = 0x04 // EXPORT_SYMBOL_FLAGS_WEAK_DEFINITION
    ExportReexport        ExportFlags = 0x08 // EXPORT_SYMBOL_FLAGS_REEXPORT
    ExportStubAndResolver ExportFlags = 0x10 // EXPORT_SYMBOL_FLAGS_STUB_AND_RESOLVER
    ExportStaticResolver  ExportFlags = 0x20 // EXPORT_SYMBOL_FLAGS_STATIC_RESOLVER
)
```

```go
// Regular export
macho.ExportEntry{Name: "_foo", Address: 0, Flags: macho.ExportKindRegular}

// Absolute constant (address is the literal value, not a VA offset)
macho.ExportEntry{Name: "_PAGE_SIZE", Address: 4096, Flags: macho.ExportKindAbsolute}

// Weak definition (may be overridden by another image)
macho.ExportEntry{Name: "_malloc", Address: 512, Flags: macho.ExportKindRegular | macho.ExportWeakDefinition}

// Re-export from sub-library with the same name
macho.ExportEntry{Name: "_bzero", Flags: macho.ExportReexport, ReexportLibOrdinal: 2}

// Re-export with a different name
macho.ExportEntry{Name: "_free", Flags: macho.ExportReexport, ReexportLibOrdinal: 2, ReexportName: "_cfree"}

// Stub + resolver (two-level lazy resolution)
macho.ExportEntry{Name: "_malloc", Flags: macho.ExportStubAndResolver, StubOffset: 0x100, ResolverOffset: 0x200}

// Thread-local symbol
macho.ExportEntry{Name: "_errno", Address: 0x18, Flags: macho.ExportKindThreadLocal}
```

---

## Function starts

```go
func BuildFunctionStarts(textVMAddr uint64, funcVAs []uint64) []byte
func (b *Builder) SetFunctionStarts(data []byte)
```

`LC_FUNCTION_STARTS` provides an encoded table of function start addresses for debuggers and stack unwinders. `funcVAs` must be sorted in ascending order.

```go
funcVAs := []uint64{textBase + 0, textBase + 64, textBase + 128}
b.SetFunctionStarts(macho.BuildFunctionStarts(textBase, funcVAs))
```

---

## Data in code

```go
func (b *Builder) AddDataInCode(e DataInCodeEntry)
```

Marks ranges of non-instruction bytes embedded in code sections (`LC_DATA_IN_CODE`).

```go
type DataInCodeEntry struct {
    Offset uint32         // byte offset from the start of __TEXT to the data range
    Length uint16         // number of bytes in the range
    Kind   DataInCodeKind
}

type DataInCodeKind uint16

const (
    DiceKindData           DataInCodeKind = 0x0001 // DICE_KIND_DATA
    DiceKindJumpTable8     DataInCodeKind = 0x0002 // DICE_KIND_JUMP_TABLE8
    DiceKindJumpTable16    DataInCodeKind = 0x0003 // DICE_KIND_JUMP_TABLE16
    DiceKindJumpTable32    DataInCodeKind = 0x0004 // DICE_KIND_JUMP_TABLE32
    DiceKindAbsJumpTable32 DataInCodeKind = 0x0005 // DICE_KIND_ABS_JUMP_TABLE32
)
```

```go
b.AddDataInCode(macho.DataInCodeEntry{
    Offset: 0x1c0, // from start of __TEXT
    Length: 16,
    Kind:   macho.DiceKindJumpTable32,
})
```

---

## Code signature

```go
func (b *Builder) ReserveCodeSignature(size uint32)
```

Reserves `size` bytes of padding in `__LINKEDIT` and emits `LC_CODE_SIGNATURE` pointing to it. The actual signature is written externally after `Emit` returns (e.g. with `codesign -s - --timestamp=none`). A typical size for ad-hoc signing is 8192 bytes.

```go
b.ReserveCodeSignature(8192)
out, _ := b.Emit()
os.WriteFile("program", out, 0o755)
// exec.Command("codesign", "-s", "-", "--timestamp=none", "program").Run()
```

---

## Relocations (MH_OBJECT only)

Relocations are set on `Section.Relocs` and are only meaningful when emitting `FileTypeObject`. The final linked image produced by the linker package has all relocations applied and these fields are ignored.

```go
type Reloc struct {
    Section string // name of the section containing the address to patch
    Offset  uint32 // byte offset within Section of the location to patch
    Symbol  string // target symbol name
    Type    uint8  // arch-specific relocation type (cast RelocTypeARM64 or RelocTypeAMD64 to uint8)
    Length  uint8  // 0=1-byte, 1=2-byte, 2=4-byte, 3=8-byte
    PCRel   bool   // PC-relative addressing
    Extern  bool   // true = symbol field is a symbol table index; false = section number
}
```

**ARM64 relocation types:**

```go
type RelocTypeARM64 uint8

const (
    ARM64_RELOC_UNSIGNED              RelocTypeARM64 = 0  // absolute 64-bit pointer
    ARM64_RELOC_SUBTRACTOR            RelocTypeARM64 = 1  // symbol A − B; must precede UNSIGNED
    ARM64_RELOC_BRANCH26              RelocTypeARM64 = 2  // BL/B 26-bit PC-relative displacement
    ARM64_RELOC_PAGE21                RelocTypeARM64 = 3  // ADRP high-21 PC-relative
    ARM64_RELOC_PAGEOFF12             RelocTypeARM64 = 4  // ADD/LDR/STR low-12 page offset
    ARM64_RELOC_GOT_LOAD_PAGE21       RelocTypeARM64 = 5  // ADRP to GOT slot
    ARM64_RELOC_GOT_LOAD_PAGEOFF12    RelocTypeARM64 = 6  // LDR 12-bit offset into GOT slot
    ARM64_RELOC_POINTER_TO_GOT        RelocTypeARM64 = 7  // 32-bit PC-relative pointer to GOT
    ARM64_RELOC_TLVP_LOAD_PAGE21      RelocTypeARM64 = 8  // ADRP to TLV pointer
    ARM64_RELOC_TLVP_LOAD_PAGEOFF12   RelocTypeARM64 = 9  // LDR 12-bit TLV offset
    ARM64_RELOC_ADDEND                RelocTypeARM64 = 10 // explicit addend for next reloc
    ARM64_RELOC_AUTHENTICATED_POINTER RelocTypeARM64 = 11 // PAC-authenticated pointer (arm64e)
)
```

**AMD64 relocation types:**

```go
type RelocTypeAMD64 uint8

const (
    X86_64_RELOC_UNSIGNED   RelocTypeAMD64 = 0 // absolute 64-bit pointer
    X86_64_RELOC_SIGNED     RelocTypeAMD64 = 1 // 32-bit PC-relative signed displacement
    X86_64_RELOC_BRANCH     RelocTypeAMD64 = 2 // 32-bit PC-relative call/jmp displacement
    X86_64_RELOC_GOT_LOAD   RelocTypeAMD64 = 3 // 32-bit PC-relative MOV via GOT
    X86_64_RELOC_GOT        RelocTypeAMD64 = 4 // 32-bit PC-relative reference to GOT slot
    X86_64_RELOC_SUBTRACTOR RelocTypeAMD64 = 5 // subtractor; must precede UNSIGNED
    X86_64_RELOC_SIGNED_1   RelocTypeAMD64 = 6 // SIGNED with −1 implicit addend
    X86_64_RELOC_SIGNED_2   RelocTypeAMD64 = 7 // SIGNED with −2 implicit addend
    X86_64_RELOC_SIGNED_4   RelocTypeAMD64 = 8 // SIGNED with −4 implicit addend
    X86_64_RELOC_TLV        RelocTypeAMD64 = 9 // 32-bit PC-relative TLV reference
)
```

---

## Version helpers

```go
func PackVersion(major, minor, patch uint16) uint32
```

Packs a `major.minor.patch` triple into the 32-bit Mach-O format (bits 31:16 = major, bits 15:8 = minor, bits 7:0 = patch). Used for `BuildVersion.MinOS`, `BuildVersion.SDK`, `DylibRef.CurrentVersion`, and `DylibRef.CompatVersion`.

```go
func PackSourceVersion(a uint32, b, c, d, e uint8) uint64
```

Packs an `A.B.C.D.E` source version into the 40-bit Mach-O representation (bits 39:26=A, 25:20=B, 19:14=C, 13:7=D, 6:0=E). Used with `SetSourceVersion`.

---

## Emit passes

| Pass | Action |
|------|--------|
| 1    | Dry-run load-command byte count |
| 2    | Assign file offsets and virtual addresses to sections |
| 3    | Build symbol table and string table |
| 4    | Collect relocation entries |
| 5    | Build indirect symbol table |
| 6    | Assign `__LINKEDIT` file offsets (dyld blobs, sym/str tables) |
| 7    | Resolve entry-point offset (LC_MAIN) |
| 8    | Compute final `ncmds` / `sizeofcmds` |
| 9    | Allocate output buffer |
| 10   | Write Mach-O header |
| 11   | Write all load commands in order |
| 12   | Write section data |
| 13   | Write `__LINKEDIT` blobs (dyld info, exports, sym/str tables) |

---

## Fixed structure sizes

| Constant                      | Bytes | Structure |
|-------------------------------|-------|-----------|
| `sizeofMachHeader64`          | 32    | `mach_header_64` |
| `sizeofSegmentCommand64`      | 64    | `segment_command_64` |
| `sizeofSection64`             | 80    | `section_64` |
| `sizeofSymtabCommand`         | 24    | `symtab_command` |
| `sizeofDysymtabCommand`       | 80    | `dysymtab_command` |
| `sizeofEntryPointCommand`     | 24    | `entry_point_command` (LC_MAIN) |
| `sizeofDylibCommand`          | 24    | `dylib_command` base |
| `sizeofUUIDCommand`           | 24    | `uuid_command` |
| `sizeofBuildVersionCommand`   | 24    | `build_version_command` base |
| `sizeofBuildToolVersion`      | 8     | `build_tool_version` |
| `sizeofSourceVersionCommand`  | 16    | `source_version_command` |
| `sizeofDyldInfoCommand`       | 48    | `dyld_info_command` |
| `sizeofLinkeditDataCommand`   | 16    | `linkedit_data_command` |
| `sizeofNlist64`               | 16    | `nlist_64` |
| `sizeofRelocEntry`            | 8     | `relocation_info` |
| `sizeofDataInCodeEntry`       | 8     | `data_in_code_entry` |