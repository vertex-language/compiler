# bin/elf

```
github.com/vertex-language/compiler/bin/elf
```

Constructs and serializes valid ELF64 binaries. Given sections, symbols, and relocations, `Emit()` produces a well-formed binary ready to run or link. The package handles section layout, string tables, program header synthesis, dynamic linking infrastructure, and symbol resolution automatically.

---

## Quick start

```go
b := elf.NewBuilder(elf.ArchAMD64)

b.AddSection(elf.Section{
    Name:  ".text",
    Type:  elf.SHT_PROGBITS,
    Flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR,
    Data:  machineCode,
    Align: 16,
})

b.AddSymbol(elf.Symbol{
    Name:    "main",
    Section: ".text",
    Offset:  0,
    Global:  true,
})

b.SetEntry("main")

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

Returns a `Builder` for the given architecture. The default output type is a position-dependent static executable (`ET_EXEC`).

### Architecture constants

```go
type Arch uint16

const (
    ArchAMD64   Arch = EM_X86_64  // 0x3E
    ArchARM64   Arch = EM_AARCH64 // 0xB7
    ArchRISCV64 Arch = EM_RISCV   // 0xF3
)
```

### Output type

```go
func (b *Builder) SetShared()
```

Switches output to a shared library (`ET_DYN`). Default is `ET_EXEC`.

### Processor flags

```go
func (b *Builder) SetFlags(f uint32)
```

Sets the `e_flags` field. Required for RISC-V targets — a zero `e_flags` is technically malformed for `EM_RISCV`. Use `EF_RISCV_*` constants. Ignored but harmless for AMD64 and ARM64.

```go
// Example: RV64GC with double-precision float ABI
b.SetFlags(elf.EF_RISCV_RVC | elf.EF_RISCV_FLOAT_ABI_DOUBLE)
```

### Entry point

```go
func (b *Builder) SetEntry(name string)
```

Names the entry-point symbol. `Emit` returns an error if the symbol cannot be resolved to a virtual address.

### Sections

```go
func (b *Builder) AddSection(s Section)
```

Appends a section. Allocatable sections (`SHF_ALLOC`) are automatically grouped into `PT_LOAD` segments by their permission flags.

### Symbols

```go
func (b *Builder) AddSymbol(s Symbol)
```

Records a symbol for the `.symtab` section. Local symbols must be added before global/weak symbols if you need a specific ordering; the builder sorts them correctly for the ELF spec regardless.

### Relocations

```go
func (b *Builder) AddReloc(r Reloc)
```

Records a RELA relocation against a named section. The builder synthesizes the corresponding `.rela.<section>` section automatically.

### Dynamic linking

```go
func (b *Builder) SetInterp(path string)
```

Sets the dynamic-linker interpreter path, causing a `PT_INTERP` segment to be emitted.

```go
// Linux x86-64
b.SetInterp("/lib64/ld-linux-x86-64.so.2")

// Linux ARM64
b.SetInterp("/lib/ld-linux-aarch64.so.1")
```

```go
func (b *Builder) AddNeeded(lib string)
```

Adds a `DT_NEEDED` shared library dependency.

```go
b.AddNeeded("libc.so.6")
b.AddNeeded("libpthread.so.0")
```

```go
func (b *Builder) SetSoname(name string)
```

Sets the `DT_SONAME` tag (for shared library output).

```go
func (b *Builder) SetRpath(path string)
```

Sets the `DT_RUNPATH` runtime library search path.

### Custom segments

```go
func (b *Builder) AddSegment(seg Segment)
```

Injects a custom program header. Must be called before `Emit`. See the [Segment](#segment) type below. The following segment types are synthesized automatically and must **not** be added via `AddSegment`:

| Type           | Synthesized when…                                     |
|----------------|-------------------------------------------------------|
| `PT_PHDR`      | Always                                                |
| `PT_INTERP`    | `SetInterp` was called                                |
| `PT_LOAD`      | One per distinct permission group of `SHF_ALLOC` sections |
| `PT_DYNAMIC`   | Dynamic linking is configured                         |
| `PT_TLS`       | Any `SHF_TLS` section is present                      |
| `PT_GNU_STACK` | Always (marks stack non-executable)                   |

Use `AddSegment` for everything else: `PT_GNU_RELRO`, `PT_NOTE`, `PT_GNU_EH_FRAME`, `PT_GNU_PROPERTY`, and any application-specific headers.

### Emit

```go
func (b *Builder) Emit() ([]byte, error)
```

Runs all layout and serialization passes and returns the raw binary bytes. Returns an error if the entry symbol cannot be resolved.

---

## Core types

### Section

```go
type Section struct {
    Name    string  // e.g. ".text", ".data", ".rodata"
    Type    uint32  // SHT_* constant
    Flags   uint64  // SHF_* bitmask
    Data    []byte  // raw section content; nil/empty for SHT_NOBITS
    Align   uint64  // address and file alignment; power of two, ≥ 1; 0 treated as 1
    Size    uint64  // SHT_NOBITS in-memory byte count (ignored when Data is non-empty)
    Link    uint32  // sh_link; semantics defined per section type
    Info    uint32  // sh_info; semantics defined per section type
    EntSize uint64  // entry size for table sections (SHT_SYMTAB, SHT_RELA, etc.)
}
```

Common section configurations:

```go
// Executable code
elf.Section{Name: ".text",   Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR, Data: code,   Align: 16}

// Read-only data
elf.Section{Name: ".rodata", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC,                     Data: rodata, Align: 8}

// Initialized read-write data
elf.Section{Name: ".data",   Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE,     Data: data,   Align: 8}

// Zero-initialized block (.bss); no file content
elf.Section{Name: ".bss",    Type: elf.SHT_NOBITS,   Flags: elf.SHF_ALLOC | elf.SHF_WRITE,     Size: 4096,   Align: 16}

// TLS initialized template
elf.Section{Name: ".tdata",  Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE | elf.SHF_TLS, Data: tdata, Align: 8}

// TLS zero block
elf.Section{Name: ".tbss",   Type: elf.SHT_NOBITS,   Flags: elf.SHF_ALLOC | elf.SHF_WRITE | elf.SHF_TLS, Size: 64,    Align: 8}
```

### Symbol

```go
type Symbol struct {
    Name    string  // symbol name in .strtab
    Section string  // section name; "" = SHN_UNDEF, "*ABS*" = SHN_ABS
    Offset  uint64  // byte offset from start of Section (or absolute value for *ABS*)
    Size    uint64  // symbol size in bytes; may be 0
    Global  bool    // STB_GLOBAL when true, otherwise STB_LOCAL
    Weak    bool    // STB_WEAK; takes precedence over Global when both are set
    Type    uint8   // STT_* constant; 0 = STT_NOTYPE
    Vis     uint8   // STV_* visibility constant; 0 = STV_DEFAULT
}
```

```go
// Defined global function
elf.Symbol{Name: "main",        Section: ".text",  Offset: 0,   Global: true, Type: elf.STT_FUNC}

// Defined global data object
elf.Symbol{Name: "errno",       Section: ".data",  Offset: 128, Global: true, Type: elf.STT_OBJECT, Size: 4}

// Absolute constant
elf.Symbol{Name: "PAGE_SIZE",   Section: "*ABS*",  Offset: 4096, Global: true}

// Undefined external reference
elf.Symbol{Name: "printf",      Section: "",       Global: true, Type: elf.STT_FUNC}

// Hidden symbol (not exported to other shared objects)
elf.Symbol{Name: "internal_fn", Section: ".text",  Offset: 64,  Global: true, Vis: elf.STV_HIDDEN}

// Weak symbol
elf.Symbol{Name: "malloc",      Section: ".text",  Offset: 512, Weak: true,   Type: elf.STT_FUNC}
```

### Reloc

```go
type Reloc struct {
    Section string  // name of section being patched, e.g. ".text"
    Offset  uint64  // byte offset within Section at which to apply the fixup
    Symbol  string  // name of the symbol the relocation references
    Type    uint32  // architecture-specific relocation type (R_X86_64_*, etc.)
    Addend  int64   // explicit constant addend (r_addend in Elf64_Rela)
}
```

```go
// AMD64: 32-bit PC-relative call to puts (standard for CALL rel32)
elf.Reloc{Section: ".text", Offset: 12, Symbol: "puts", Type: elf.R_X86_64_PLT32, Addend: -4}

// AMD64: 64-bit absolute address
elf.Reloc{Section: ".data", Offset: 0,  Symbol: "table", Type: elf.R_X86_64_64, Addend: 0}

// ARM64: ADRP + ADD pair for PC-relative data reference
elf.Reloc{Section: ".text", Offset: 0, Symbol: "data_sym", Type: elf.R_AARCH64_ADR_PREL_PG_HI21, Addend: 0}
elf.Reloc{Section: ".text", Offset: 4, Symbol: "data_sym", Type: elf.R_AARCH64_ADD_ABS_LO12_NC,  Addend: 0}

// RISC-V: AUIPC + JALR pair for function call
elf.Reloc{Section: ".text", Offset: 0, Symbol: "fn", Type: elf.R_RISCV_CALL, Addend: 0}
```

### Segment

```go
type Segment struct {
    Type     uint32   // PT_* constant
    Flags    uint32   // PF_R | PF_W | PF_X permission bitmask
    Align    uint64   // segment alignment; 0 treated as 1
    Sections []string // section names whose extent defines this segment; nil for metadata-only headers
}
```

```go
// PT_GNU_RELRO: marks .got and .dynamic as read-only after relocation
b.AddSegment(elf.Segment{
    Type:     elf.PT_GNU_RELRO,
    Flags:    elf.PF_R,
    Align:    1,
    Sections: []string{".got", ".dynamic"},
})

// PT_NOTE: metadata-only, no file extent
b.AddSegment(elf.Segment{
    Type:  elf.PT_NOTE,
    Flags: elf.PF_R,
    Align: 4,
})

// PT_GNU_EH_FRAME: exception handling frame index
b.AddSegment(elf.Segment{
    Type:     elf.PT_GNU_EH_FRAME,
    Flags:    elf.PF_R,
    Align:    4,
    Sections: []string{".eh_frame_hdr"},
})
```

---

## Note sections

### Types and constants

```go
type Note struct {
    Name string // owner name, e.g. "GNU"; written NUL-terminated in file
    Type uint32 // NT_* constant
    Desc []byte // note payload
}
```

```go
// n_type values for owner "GNU"
const (
    NT_GNU_ABI_TAG      = 1 // minimum OS/ABI version (.note.ABI-tag)
    NT_GNU_HWCAP        = 2 // hardware capability bitfield
    NT_GNU_BUILD_ID     = 3 // unique build identifier (.note.gnu.build-id)
    NT_GNU_GOLD_VERSION = 4 // GNU gold linker version string
    NT_GNU_PROPERTY     = 5 // program properties (.note.gnu.property)
)

// OS identifiers for NT_GNU_ABI_TAG desc[0]
const (
    GNU_ABI_TAG_LINUX    = 0
    GNU_ABI_TAG_HURD     = 1
    GNU_ABI_TAG_SOLARIS  = 2
    GNU_ABI_TAG_FREEBSD  = 3
    GNU_ABI_TAG_NETBSD   = 4
    GNU_ABI_TAG_SYLLABLE = 5
    GNU_ABI_TAG_NACL     = 6
)

// AMD64 CET property flags for NT_GNU_PROPERTY / GNU_PROPERTY_X86_FEATURE_1_AND
const (
    GNU_PROPERTY_X86_FEATURE_1_AND   = uint32(0xc0000002)
    GNU_PROPERTY_X86_FEATURE_1_IBT   = uint32(0x1) // indirect branch tracking
    GNU_PROPERTY_X86_FEATURE_1_SHSTK = uint32(0x2) // shadow stack
)
```

### Builder functions

```go
func BuildNoteSection(notes []Note) []byte
```

Serializes a slice of `Note` values into a `SHT_NOTE` section body, 4-byte aligned per the ELF spec. Use the result directly as `Section.Data`.

```go
func BuildBuildID(id []byte) []byte
```

Returns a `.note.gnu.build-id` section body. `id` is typically a 20-byte SHA-1 digest or a 16-byte UUID.

```go
func BuildABITag(major, minor, patch uint32) []byte
```

Returns a `.note.ABI-tag` section body declaring the minimum Linux kernel version required.

```go
func BuildGNUProperty(featureFlags uint32) []byte
```

Returns a `.note.gnu.property` section body for AMD64 CET features. Without this note the kernel will not enable hardware IBT or shadow stack for the process even if the CPU supports them.

**Usage example:**

```go
b.AddSection(elf.Section{
    Name:  ".note.gnu.build-id",
    Type:  elf.SHT_NOTE,
    Flags: elf.SHF_ALLOC,
    Data:  elf.BuildBuildID(sha1Digest),
    Align: 4,
})

b.AddSection(elf.Section{
    Name:  ".note.ABI-tag",
    Type:  elf.SHT_NOTE,
    Flags: elf.SHF_ALLOC,
    Data:  elf.BuildABITag(3, 2, 0), // requires Linux 3.2+
    Align: 4,
})

b.AddSection(elf.Section{
    Name:  ".note.gnu.property",
    Type:  elf.SHT_NOTE,
    Flags: elf.SHF_ALLOC,
    Data:  elf.BuildGNUProperty(elf.GNU_PROPERTY_X86_FEATURE_1_IBT | elf.GNU_PROPERTY_X86_FEATURE_1_SHSTK),
    Align: 8,
})
```

---

## Dynamic linking — DynBuilder

`DynBuilder` provides fine-grained control over PLT/GOT layout when you need to manage lazy binding stubs and GOT slots directly, rather than using the high-level `SetInterp` / `AddNeeded` path alone.

### Types

```go
type DynSym struct {
    Name     string // external symbol name, e.g. "printf", "malloc"
    NeedsPLT bool   // allocate a PLT stub and .got.plt slot (R_*_JUMP_SLOT); for imported functions
    NeedsGOT bool   // allocate a .got entry (R_*_GLOB_DAT); for imported data objects
}
```

### Construction and population

```go
func NewDynBuilder(arch Arch) *DynBuilder

func (db *DynBuilder) Add(sym DynSym)
```

```go
db := elf.NewDynBuilder(elf.ArchAMD64)
db.Add(elf.DynSym{Name: "printf",  NeedsPLT: true})
db.Add(elf.DynSym{Name: "malloc",  NeedsPLT: true})
db.Add(elf.DynSym{Name: "environ", NeedsGOT: true})
```

### Emitting sections

```go
func (db *DynBuilder) Sections() []Section
```

Returns the set of ELF sections produced by this `DynBuilder` in the order they should be added to the `Builder`:

| Section      | Type         | Contents                                |
|--------------|--------------|-----------------------------------------|
| `.got.plt`   | `SHT_PROGBITS` | GOT entries for PLT stubs (3 reserved + 1 per PLT sym) |
| `.plt`       | `SHT_PROGBITS` | PLT0 header + one 16-byte stub per symbol |
| `.got`       | `SHT_PROGBITS` | GOT entries for data symbols            |
| `.rela.plt`  | `SHT_RELA`     | `R_*_JUMP_SLOT` relocations             |
| `.rela.dyn`  | `SHT_RELA`     | `R_*_GLOB_DAT` relocations              |

```go
func (db *DynBuilder) DynSyms() []Symbol
```

Returns the ordered dynamic symbol list for populating `.dynsym`. Pass these to `Builder.AddSymbol` after the sections.

```go
func (db *DynBuilder) PLTEntrySize() int
```

Returns the PLT stub byte size for the target architecture (always 16).

**Full dynamic linking example:**

```go
b := elf.NewBuilder(elf.ArchAMD64)
b.SetInterp("/lib64/ld-linux-x86-64.so.2")
b.AddNeeded("libc.so.6")

db := elf.NewDynBuilder(elf.ArchAMD64)
db.Add(elf.DynSym{Name: "printf", NeedsPLT: true})
db.Add(elf.DynSym{Name: "exit",   NeedsPLT: true})

for _, sec := range db.Sections() {
    b.AddSection(sec)
}
for _, sym := range db.DynSyms() {
    b.AddSymbol(sym)
}

b.AddSection(elf.Section{Name: ".text", ...})
b.SetEntry("main")

out, _ := b.Emit()
```

---

## Hash sections

### GNU hash (.gnu.hash)

```go
func BuildGNUHash(sortedNames []string, symOffset uint32) []byte
```

Builds a `.gnu.hash` (`SHT_GNU_HASH`) section body. The input names must already be sorted into GNU hash order — use `SortGNUHashSyms` to produce that ordering. `symOffset` is the `.dynsym` index of the first hashed symbol (typically `1`, since entry `0` is always the null symbol).

```go
func SortGNUHashSyms(symNames []string) (sorted []string, perm []int)
```

Returns `symNames` sorted by `gnuHash(name) % nbuckets`. The second return value maps each output position to its original input index, which you need to reorder your `.dynsym` entries to match.

```go
sorted, perm := elf.SortGNUHashSyms([]string{"printf", "malloc", "free"})
gnuHashData := elf.BuildGNUHash(sorted, 1)

b.AddSection(elf.Section{
    Name:  ".gnu.hash",
    Type:  elf.SHT_GNU_HASH,
    Flags: elf.SHF_ALLOC,
    Data:  gnuHashData,
    Align: 8,
})
```

### SysV hash (.hash)

```go
func BuildSysVHash(symNames []string) []byte
```

Builds a `.hash` (`SHT_HASH`) section body using the legacy System V hash algorithm. `symNames` must include the null entry at index `0` (pass `""` as the first element).

```go
names := []string{"", "printf", "malloc", "free"} // index 0 = null
b.AddSection(elf.Section{
    Name:  ".hash",
    Type:  elf.SHT_HASH,
    Flags: elf.SHF_ALLOC,
    Data:  elf.BuildSysVHash(names),
    Align: 4,
})
```

---

## Symbol versioning

### Types

```go
type VersionNeed struct {
    Library  string   // DT_NEEDED library name, e.g. "libc.so.6"
    Versions []string // version strings required from Library, e.g. ["GLIBC_2.5", "GLIBC_2.17"]
}
```

Version indices are assigned in the order they appear across all `VersionNeed` entries, starting at `2` (`0 = VER_NDX_LOCAL`, `1 = VER_NDX_GLOBAL`).

### Builder functions

```go
func BuildVersionSym(indices []uint16) []byte
```

Builds a `.gnu.version` (`SHT_GNU_VERSYM`) section body. `indices` must have one `uint16` per `.dynsym` entry including the null entry at `[0]`. Use `VER_NDX_LOCAL` (`0`), `VER_NDX_GLOBAL` (`1`), or a user-assigned index ≥ 2.

```go
func BuildVersionNeed(needs []VersionNeed, stringOffset func(string) uint32) []byte
```

Builds a `.gnu.version_r` (`SHT_GNU_VERNEED`) section body. The `stringOffset` callback must return each library and version string's byte offset within the target `.dynstr` section. All strings must be pre-interned into `.dynstr` before calling.

**Usage example:**

```go
needs := []elf.VersionNeed{
    {Library: "libc.so.6", Versions: []string{"GLIBC_2.5", "GLIBC_2.17"}},
}

// indices: [0]=null, [1]=printf@GLIBC_2.5 (idx 2), [2]=mkostemp@GLIBC_2.17 (idx 3)
verSymData := elf.BuildVersionSym([]uint16{0, 2, 3})

verNeedData := elf.BuildVersionNeed(needs, func(s string) uint32 {
    return dynstrTab.Offset(s) // your dynstr interning function
})
```

---

## Constants reference

### Section header types (`sh_type`)

| Constant            | Value        | Description                        |
|---------------------|--------------|------------------------------------|
| `SHT_NULL`          | `0`          | Inactive section                   |
| `SHT_PROGBITS`      | `1`          | Program-defined content            |
| `SHT_SYMTAB`        | `2`          | Symbol table                       |
| `SHT_STRTAB`        | `3`          | String table                       |
| `SHT_RELA`          | `4`          | Relocation entries with addends    |
| `SHT_HASH`          | `5`          | SysV symbol hash table             |
| `SHT_DYNAMIC`       | `6`          | Dynamic linking information        |
| `SHT_NOTE`          | `7`          | Note section                       |
| `SHT_NOBITS`        | `8`          | Zero-filled (`.bss`)               |
| `SHT_REL`           | `9`          | Relocation entries without addends |
| `SHT_DYNSYM`        | `11`         | Dynamic symbol table               |
| `SHT_INIT_ARRAY`    | `14`         | Array of constructors              |
| `SHT_FINI_ARRAY`    | `15`         | Array of destructors               |
| `SHT_PREINIT_ARRAY` | `16`         | Array of pre-constructors          |
| `SHT_GROUP`         | `17`         | Section group                      |
| `SHT_GNU_HASH`      | `0x6FFFFFF6` | GNU symbol hash table              |
| `SHT_GNU_VERNEED`   | `0x6FFFFFFE` | GNU version requirements           |
| `SHT_GNU_VERSYM`    | `0x6FFFFFFF` | GNU version symbol table           |

### Section header flags (`sh_flags`)

| Constant               | Value  | Description                              |
|------------------------|--------|------------------------------------------|
| `SHF_WRITE`            | `0x001`| Section is writable at runtime           |
| `SHF_ALLOC`            | `0x002`| Occupies memory during execution         |
| `SHF_EXECINSTR`        | `0x004`| Contains executable machine code         |
| `SHF_MERGE`            | `0x010`| May be merged to eliminate duplicates    |
| `SHF_STRINGS`          | `0x020`| Contains NUL-terminated strings          |
| `SHF_INFO_LINK`        | `0x040`| `sh_info` holds a section header index   |
| `SHF_LINK_ORDER`       | `0x080`| Preserve section order after combining   |
| `SHF_OS_NONCONFORMING` | `0x100`| OS-specific handling required            |
| `SHF_GROUP`            | `0x200`| Member of a section group                |
| `SHF_TLS`              | `0x400`| Section holds thread-local data          |
| `SHF_COMPRESSED`       | `0x800`| Section is compressed                    |

### Program header types (`p_type`)

| Constant          | Value        | Description                               |
|-------------------|--------------|-------------------------------------------|
| `PT_NULL`         | `0`          | Unused entry                              |
| `PT_LOAD`         | `1`          | Loadable segment                          |
| `PT_DYNAMIC`      | `2`          | Dynamic linking information               |
| `PT_INTERP`       | `3`          | Interpreter path                          |
| `PT_NOTE`         | `4`          | Note section                              |
| `PT_PHDR`         | `6`          | Program header table itself               |
| `PT_TLS`          | `7`          | Thread-local storage template             |
| `PT_GNU_EH_FRAME` | `0x6474E550` | Exception handling frame index            |
| `PT_GNU_STACK`    | `0x6474E551` | Stack executability (`PF_X` means exec)   |
| `PT_GNU_RELRO`    | `0x6474E552` | Read-only after relocation                |
| `PT_GNU_PROPERTY` | `0x6474E553` | GNU program property note                 |

### Program header flags (`p_flags`)

| Constant | Value | Description   |
|----------|-------|---------------|
| `PF_X`   | `0x1` | Executable    |
| `PF_W`   | `0x2` | Writable      |
| `PF_R`   | `0x4` | Readable      |

### Symbol binding

| Constant     | Value | Description                     |
|--------------|-------|---------------------------------|
| `STB_LOCAL`  | `0`   | Not visible outside object file |
| `STB_GLOBAL` | `1`   | Visible to all objects          |
| `STB_WEAK`   | `2`   | Like global but lower precedence|

### Symbol type

| Constant       | Value | Description                    |
|----------------|-------|--------------------------------|
| `STT_NOTYPE`   | `0`   | Type not specified             |
| `STT_OBJECT`   | `1`   | Data object                    |
| `STT_FUNC`     | `2`   | Function or executable code    |
| `STT_SECTION`  | `3`   | Associated with a section      |
| `STT_FILE`     | `4`   | Source file name               |
| `STT_COMMON`   | `5`   | Uninitialized common block     |
| `STT_TLS`      | `6`   | Thread-local data object       |
| `STT_GNU_IFUNC`| `10`  | Indirect function              |

### Symbol visibility

| Constant        | Value | Description                               |
|-----------------|-------|-------------------------------------------|
| `STV_DEFAULT`   | `0`   | Visibility determined by binding type     |
| `STV_INTERNAL`  | `1`   | Processor-specific hidden visibility      |
| `STV_HIDDEN`    | `2`   | Not visible outside the defining module   |
| `STV_PROTECTED` | `3`   | Visible but not preemptable               |

### RISC-V e_flags

| Constant                    | Value    | Description                          |
|-----------------------------|----------|--------------------------------------|
| `EF_RISCV_RVC`              | `0x0001` | Compressed (C) extension present     |
| `EF_RISCV_FLOAT_ABI_SOFT`   | `0x0000` | Software float ABI (no FPU)          |
| `EF_RISCV_FLOAT_ABI_SINGLE` | `0x0002` | Single-precision float ABI           |
| `EF_RISCV_FLOAT_ABI_DOUBLE` | `0x0004` | Double-precision float ABI (most common) |
| `EF_RISCV_FLOAT_ABI_QUAD`   | `0x0006` | Quad-precision float ABI             |
| `EF_RISCV_RVE`              | `0x0008` | E extension (embedded, 16 integer registers) |
| `EF_RISCV_TSO`              | `0x0010` | TSO memory model                     |

---

## Relocation constants

### AMD64 — `R_X86_64_*`

| Constant                  | Value | Formula          | Notes                            |
|---------------------------|-------|------------------|----------------------------------|
| `R_X86_64_NONE`           | `0`   | —                |                                  |
| `R_X86_64_64`             | `1`   | `S + A`          | 64-bit absolute                  |
| `R_X86_64_PC32`           | `2`   | `S + A − P`      | 32-bit PC-relative               |
| `R_X86_64_GOT32`          | `3`   | `G + A`          | 32-bit GOT offset                |
| `R_X86_64_PLT32`          | `4`   | `L + A − P`      | 32-bit PLT-relative; use for calls |
| `R_X86_64_COPY`           | `5`   | —                | Copy symbol at runtime           |
| `R_X86_64_GLOB_DAT`       | `6`   | `S`              | GOT entry ← symbol address       |
| `R_X86_64_JUMP_SLOT`      | `7`   | `S`              | PLT GOT slot                     |
| `R_X86_64_RELATIVE`       | `8`   | `B + A`          | Position-independent             |
| `R_X86_64_GOTPCREL`       | `9`   | `G + GOT + A − P`|                                  |
| `R_X86_64_32`             | `10`  | `S + A`          | 32-bit zero-extend               |
| `R_X86_64_32S`            | `11`  | `S + A`          | 32-bit sign-extend               |
| `R_X86_64_16`             | `12`  | `S + A`          | 16-bit                           |
| `R_X86_64_PC16`           | `13`  | `S + A − P`      | 16-bit PC-relative               |
| `R_X86_64_8`              | `14`  | `S + A`          | 8-bit                            |
| `R_X86_64_PC8`            | `15`  | `S + A − P`      | 8-bit PC-relative                |
| `R_X86_64_DTPMOD64`       | `16`  | —                | TLS module index                 |
| `R_X86_64_DTPOFF64`       | `17`  | —                | TLS block offset (64-bit)        |
| `R_X86_64_TPOFF64`        | `18`  | —                | TLS initial-exec offset (64-bit) |
| `R_X86_64_TLSGD`          | `19`  | —                | PC-relative offset to GD GOT entry |
| `R_X86_64_TLSLD`          | `20`  | —                | PC-relative offset to LD GOT entry |
| `R_X86_64_DTPOFF32`       | `21`  | —                | TLS block offset (32-bit)        |
| `R_X86_64_GOTTPOFF`       | `22`  | —                | PC-relative offset to IE GOT entry |
| `R_X86_64_TPOFF32`        | `23`  | —                | TLS initial-exec offset (32-bit) |
| `R_X86_64_PC64`           | `24`  | `S + A − P`      | 64-bit PC-relative               |
| `R_X86_64_GOTOFF64`       | `25`  | `S + A − GOT`    |                                  |
| `R_X86_64_GOTPC32`        | `26`  | `GOT + A − P`    | 32-bit                           |
| `R_X86_64_SIZE32`         | `32`  | `Z + A`          | Symbol size, 32-bit              |
| `R_X86_64_SIZE64`         | `33`  | `Z + A`          | Symbol size, 64-bit              |
| `R_X86_64_GOTPC32_TLSDESC`| `34`  | —                | GOT offset to TLS descriptor     |
| `R_X86_64_TLSDESC_CALL`   | `35`  | —                | Relaxable call through TLS descriptor |
| `R_X86_64_TLSDESC`        | `36`  | —                | TLS descriptor                   |
| `R_X86_64_IRELATIVE`      | `37`  | `B + A`          | ifunc resolver result            |
| `R_X86_64_GOTPCRELX`      | `41`  | —                | Like `GOTPCREL`, relaxable       |
| `R_X86_64_REX_GOTPCRELX`  | `42`  | —                | Like `GOTPCREL` with REX, relaxable |

### AArch64 — `R_AARCH64_*`

Selected commonly-used constants; see `reloc_arm64.go` for the full list.

| Constant                        | Value | Formula               | Instruction        |
|---------------------------------|-------|-----------------------|--------------------|
| `R_AARCH64_NONE`                | `0`   | —                     |                    |
| `R_AARCH64_ABS64`               | `257` | `S + A`               | 64-bit absolute    |
| `R_AARCH64_ABS32`               | `258` | `S + A`               | 32-bit absolute    |
| `R_AARCH64_PREL64`              | `260` | `S + A − P`           | 64-bit PC-relative |
| `R_AARCH64_PREL32`              | `261` | `S + A − P`           | 32-bit PC-relative |
| `R_AARCH64_ADR_PREL_PG_HI21`   | `275` | `Page(S+A)−Page(P)` [32:12] | ADRP          |
| `R_AARCH64_ADD_ABS_LO12_NC`    | `277` | `S + A` [11:0]        | ADD imm            |
| `R_AARCH64_LDST8_ABS_LO12_NC`  | `278` | `S + A` [11:0]        | LDR/STR byte       |
| `R_AARCH64_LDST64_ABS_LO12_NC` | `286` | `S + A` [11:3]        | LDR/STR 64-bit     |
| `R_AARCH64_JUMP26`              | `282` | `S + A − P` [27:2]   | B                  |
| `R_AARCH64_CALL26`              | `283` | `S + A − P` [27:2]   | BL                 |
| `R_AARCH64_ADR_GOT_PAGE`        | `311` | `Page(G(S))−Page(P)` | ADRP (GOT)         |
| `R_AARCH64_LD64_GOT_LO12_NC`   | `312` | `G(S)` [11:3]         | LDR (GOT lo12)     |
| `R_AARCH64_COPY`                | `1024`| —                     | Copy at runtime    |
| `R_AARCH64_GLOB_DAT`            | `1025`| `S + A`               | GOT ← symbol addr  |
| `R_AARCH64_JUMP_SLOT`           | `1026`| `S + A`               | PLT GOT slot       |
| `R_AARCH64_RELATIVE`            | `1027`| `B + A`               | Position-independent|
| `R_AARCH64_IRELATIVE`           | `1032`| `B + A`               | ifunc result       |

Full TLS (`TLSGD`, `TLSLD`, `TLSIE`, `TLSLE`, `TLSDESC`) and `MOVW` constants are defined in `reloc_arm64.go`.

### RISC-V 64 — `R_RISCV_*`

Selected commonly-used constants; see `reloc_riscv64.go` for the full list.

| Constant               | Value | Formula              | Notes                            |
|------------------------|-------|----------------------|----------------------------------|
| `R_RISCV_NONE`         | `0`   | —                    |                                  |
| `R_RISCV_32`           | `1`   | `S + A`              | 32-bit absolute                  |
| `R_RISCV_64`           | `2`   | `S + A`              | 64-bit absolute; also `GLOB_DAT` |
| `R_RISCV_RELATIVE`     | `3`   | `B + A`              | Position-independent             |
| `R_RISCV_COPY`         | `4`   | —                    | Copy symbol at runtime           |
| `R_RISCV_JUMP_SLOT`    | `5`   | `S`                  | PLT GOT slot                     |
| `R_RISCV_BRANCH`       | `16`  | `S + A − P` [12:1]  | B-type: BEQ, BNE, …             |
| `R_RISCV_JAL`          | `17`  | `S + A − P` [20:1]  | J-type: JAL                      |
| `R_RISCV_CALL`         | `18`  | `S + A − P` [31:0]  | AUIPC+JALR pair (8 bytes)        |
| `R_RISCV_CALL_PLT`     | `19`  | `S + A − P` [31:0]  | Like `CALL` but through PLT      |
| `R_RISCV_GOT_HI20`     | `20`  | `G+GOT−P` [31:12]   | AUIPC targeting GOT entry        |
| `R_RISCV_PCREL_HI20`   | `23`  | `S + A − P` [31:12] | AUIPC `%pcrel_hi`                |
| `R_RISCV_PCREL_LO12_I` | `24`  | `S − P` [11:0]      | I-type `%pcrel_lo`               |
| `R_RISCV_PCREL_LO12_S` | `25`  | `S − P` [11:0]      | S-type `%pcrel_lo`               |
| `R_RISCV_HI20`         | `26`  | `S + A` [31:12]     | LUI `%hi`                        |
| `R_RISCV_LO12_I`       | `27`  | `S + A` [11:0]      | I-type `%lo`                     |
| `R_RISCV_LO12_S`       | `28`  | `S + A` [11:0]      | S-type `%lo`                     |
| `R_RISCV_ADD32`        | `35`  | `V + S + A`          | 32-bit addend                    |
| `R_RISCV_ADD64`        | `36`  | `V + S + A`          | 64-bit addend                    |
| `R_RISCV_SUB32`        | `39`  | `V − S − A`          | 32-bit subtraction               |
| `R_RISCV_SUB64`        | `40`  | `V − S − A`          | 64-bit subtraction               |
| `R_RISCV_ALIGN`        | `43`  | —                    | Alignment directive              |
| `R_RISCV_RELAX`        | `51`  | —                    | Hints preceding insn may relax   |
| `R_RISCV_IRELATIVE`    | `58`  | `B + A`              | ifunc resolver result            |

Full TLS and compressed-ISA (`RVC_BRANCH`, `RVC_JUMP`) constants are defined in `reloc_riscv64.go`.

---

## Emit passes

Understanding what `Emit` does internally can help with debugging malformed output:

| Pass | Action |
|------|--------|
| 1 | Collect user sections; create `.rela.*` placeholders; create dynamic / metadata sections |
| 2 | Build `.shstrtab` |
| 3 | Build `.symtab` and `.strtab` (initial pass, addresses unknown) |
| 4 | Build `.dynamic` and `.dynstr` |
| 5 | Assign file offsets and virtual addresses to all sections |
| 6 | Resolve symbol virtual addresses |
| 7 | Rebuild `.symtab` with resolved addresses |
| 8 | Build `.rela.*` section data |
| 9 | Resolve entry-point address |
| 10 | Build program headers; re-layout if count differs from estimate |
| 11 | Locate end of file content; place section header table |
| 12 | Serialize everything into a flat `[]byte` |

---

## Fixed structure sizes

These constants are exported for callers that need to compute offsets or sizes manually:

| Constant        | Bytes | Structure              |
|-----------------|-------|------------------------|
| `elfHeaderSize` | `64`  | `Elf64_Ehdr`           |
| `phdrEntrySize` | `56`  | `Elf64_Phdr`           |
| `shdrEntrySize` | `64`  | `Elf64_Shdr`           |
| `symEntrySize`  | `24`  | `Elf64_Sym`            |
| `relaEntrySize` | `24`  | `Elf64_Rela`           |
| `dynEntrySize`  | `16`  | `Elf64_Dyn`            |