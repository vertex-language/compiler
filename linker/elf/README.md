# linker/elf

A static linker for ELF64 binaries, targeting AMD64, AArch64, and RISC-V 64.

Reads `ET_REL` object files (`.o`), `ET_DYN` shared libraries (`.so`), and
GNU/SysV static archives (`.a`). Resolves symbols, merges sections, assigns
virtual addresses, patches RELA relocations, and drives
`github.com/vertex-language/compiler/bin/elf` to emit the final binary.

---

## Pipeline

```
[.o / .a / .so inputs]
      │
      ▼
ParseObject / ParseArchive / ParseShared   (parse layer)
      │
      ▼
SymbolTable.Ingest()                        (resolution + archive extraction)
      │
      ▼
MergeSections()                             (combine same-named sections)
      │
      ▼
AssignLayout()                              (virtual address + file offset)
      │
      ▼
ResolveSymbolAddresses()                    (vaddr per symbol)
      │
      ▼
PatchRelocations()                          (apply RELA formulas)
      │
      ▼
LinkResult.Builder() → bin/elf.Emit()       (serialise)
```

---

## Quick start

```go
lnk := elf.NewLinker(0x3E) // EM_X86_64
lnk.SetEntry("main")
lnk.SetInterp("/lib64/ld-linux-x86-64.so.2")

lnk.AddObject(elf.MustOpenObject("main.o"))
lnk.AddObject(elf.MustOpenObject("util.o"))
lnk.AddArchive(elf.MustOpenArchive("libfoo.a"))
lnk.AddShared(elf.MustOpenShared("libc.so.6"))

result, err := lnk.Link()
if err != nil {
    log.Fatal(err)
}

out, err := result.Builder().Emit()
if err != nil {
    log.Fatal(err)
}
os.WriteFile("program", out, 0o755)
```

---

## Linker

### Construction

```go
func NewLinker(machine uint16) *Linker
```

Returns a `Linker` for the given ELF machine value. Default output type is
`OutputExec`.

```go
const (
    EM_X86_64  = 0x3E
    EM_AARCH64 = 0xB7
    EM_RISCV   = 0xF3
)
```

### Configuration

```go
func (l *Linker) SetOutputType(t OutputType)
func (l *Linker) SetEntry(name string)       // entry-point symbol name
func (l *Linker) SetInterp(path string)      // PT_INTERP dynamic linker path
func (l *Linker) SetSoname(name string)      // DT_SONAME (shared library output)
func (l *Linker) SetRpath(path string)       // DT_RUNPATH
func (l *Linker) SetEFlags(f uint32)         // e_flags (required for RISC-V)
func (l *Linker) AddLibraryPath(path string) // -L search path for DT_NEEDED resolution
```

### Adding inputs

```go
func (l *Linker) AddObject(o *ObjectFile)
func (l *Linker) AddArchive(a *Archive)
func (l *Linker) AddShared(s *SharedLib)
```

Inputs are processed in the order they are added, following classical Unix
left-to-right command-line linker semantics. This matters for archive
extraction: a symbol must be referenced before the archive containing its
definition is processed.

### Linking

```go
func (l *Linker) Link() (*LinkResult, error)
```

Runs all seven phases and returns a `LinkResult` on success. Errors include
undefined symbol references, duplicate strong definitions, malformed input
files, and relocation overflows.

---

## Output types

```go
type OutputType int

const (
    OutputExec   OutputType = iota // ET_EXEC: position-dependent executable
    OutputPIE                       // ET_DYN: position-independent executable
    OutputShared                    // ET_DYN: shared library
)
```

`OutputExec` places the first `PT_LOAD` segment at virtual address `0x400000`
(the standard Linux base). `OutputPIE` and `OutputShared` start at `0x0` and
rely on the dynamic linker for base-address assignment.

---

## LinkResult

```go
type LinkResult struct {
    Arch       binelf.Arch
    OutputType OutputType
    Entry      string
    Interp     string
    Soname     string
    Rpath      string
    Needed     []string   // DT_NEEDED in BFS-visited load order
    Layout     *Layout
    Symtab     *SymbolTable
    Machine    uint16
    EFlags     uint32
}
```

### Builder

```go
func (r *LinkResult) Builder() *binelf.Builder
```

Constructs and returns a fully-configured `bin/elf.Builder` ready to call
`Emit()` on. Sections are added in layout order; symbols are translated from
the internal representation to `bin/elf.Symbol` values with resolved virtual
addresses.

---

## Parsing inputs

### Object files

```go
func OpenObject(path string) (*ObjectFile, error)
func MustOpenObject(path string) *ObjectFile   // panics on error
func ParseObject(data []byte) (*ObjectFile, error)
```

Parses an `ELF64 ET_REL` relocatable object. Only little-endian 64-bit ELF is
supported. The result exposes sections, symbols, and RELA relocations.

```go
type ObjectFile struct {
    Path     string
    Machine  uint16
    EFlags   uint32
    Sections []*RawSection // index 0 is the null section
    Symbols  []*RawSymbol  // index 0 is the null symbol
    Relocs   []*RawReloc
}
```

### Archives

```go
func OpenArchive(path string) (*Archive, error)
func MustOpenArchive(path string) *Archive
func ParseArchive(data []byte) (*Archive, error)
```

Parses a GNU/SysV `ar` archive (`.a`). Archive members are loaded lazily:
the underlying object is only parsed when the member is actually extracted
during symbol resolution.

```go
type Archive struct {
    Path    string
    Members []*ArchiveMember
}

func (a *Archive) MemberForSymbol(sym string) *ArchiveMember
```

`MemberForSymbol` consults the pre-built symbol index (`/` member) and falls
back to exhaustive scanning if none is present.

```go
type ArchiveMember struct {
    Name string
}

func (m *ArchiveMember) Object() (*ObjectFile, error) // cached
```

### Shared libraries

```go
func OpenShared(path string) (*SharedLib, error)
func MustOpenShared(path string) *SharedLib
func ParseShared(data []byte) (*SharedLib, error)
```

Parses an `ELF64 ET_DYN` shared object. Extracts the `.dynsym` dynamic symbol
table, `DT_NEEDED` dependencies, `DT_SONAME`, and `DT_RPATH`/`DT_RUNPATH`.

```go
type SharedLib struct {
    Path string
}

func (s *SharedLib) Soname() string    // DT_SONAME, or path basename
func (s *SharedLib) Needed() []string  // DT_NEEDED entries
func (s *SharedLib) Rpaths() []string  // DT_RPATH + DT_RUNPATH
func (s *SharedLib) Symbol(name string) (*DynSymbol, bool)
```

Shared library dependencies declared via `DT_NEEDED` are resolved transitively
using a BFS walk. The linker searches, in order: the embedding library's
`DT_RPATH`/`DT_RUNPATH`, paths added with `AddLibraryPath`, then the system
defaults (`/lib64`, `/usr/lib64`, `/lib`, `/usr/lib`, and architecture
multi-lib variants).

---

## Symbol resolution

`SymbolTable.Ingest` implements standard ELF resolution rules across all input
types. The precedence order from strongest to weakest is:

```
Defined (ET_REL hard definition)
  > Common (SHN_COMMON tentative C block)
  > Shared (ET_DYN exported symbol)
  > Lazy   (archive member not yet extracted)
  > Undefined
```

Within `kindDefined`, binding strength applies:

| Existing \ Incoming | STB_GLOBAL | STB_WEAK |
|---------------------|-----------|----------|
| **STB_GLOBAL**      | Error     | Keep existing |
| **STB_WEAK**        | Incoming wins | First one wins |

Additional rules:

- A hard definition always beats a `SHN_COMMON` block.
- When two `SHN_COMMON` blocks collide, the larger size wins.
- `STB_WEAK` undefined symbols resolve to zero — they do not trigger archive
  extraction and do not produce undefined-reference errors.
- Archive extraction repeats in a fixed-point loop until no new members are
  pulled in, allowing cross-dependencies between members in the same archive.

---

## Section merging

```go
func MergeSections(objects []*ObjectFile) (*Layout, error)
```

Combines all input sections that share a name into a single `MergedSection`.
Contributions are appended in object-file order with alignment padding between
them. The alignment of the merged section is the maximum of all contributors.

`SHT_NOBITS` (`.bss`) sections accumulate size without producing file bytes.
Metadata sections (`SHT_SYMTAB`, `SHT_STRTAB`, `SHT_RELA`, `SHT_GROUP`) are
skipped — they are synthesised from scratch by `bin/elf`.

```go
type Layout struct {
    Sections []*MergedSection
}

func (l *Layout) SectionByName(name string) (*MergedSection, bool)

type MergedSection struct {
    Name       string
    Type       uint32
    Flags      uint64
    Align      uint64
    Pieces     []Piece   // one per contributing input section
    Data       []byte    // nil for SHT_NOBITS
    Size       uint64
    VAddr      uint64    // filled by AssignLayout
    FileOffset uint64    // filled by AssignLayout
}

type Piece struct {
    Obj    *ObjectFile
    Sec    *RawSection
    Offset uint64 // byte offset within the merged output section
}
```

---

## Address layout

```go
func AssignLayout(outputType OutputType, layout *Layout) error
```

Assigns virtual addresses and file offsets to every `MergedSection`. Sections
are grouped into three `PT_LOAD` segments by their `sh_flags`:

| Segment | Flags                      | Contents                          |
|---------|----------------------------|-----------------------------------|
| RX      | `SHF_ALLOC \| SHF_EXECINSTR` | `.text`, `.plt`, etc.           |
| RO      | `SHF_ALLOC` (no W, no X)   | `.rodata`, `.eh_frame`, etc.      |
| RW      | `SHF_ALLOC \| SHF_WRITE`   | `.data`, `.bss`, `.got`, etc.     |

Segments are separated by 2 MiB page-aligned boundaries (`PT_LOAD` alignment =
`0x200000`). Non-allocatable sections (debug info, etc.) are placed at the end
of the file with no virtual address.

```go
func ResolveSymbolAddresses(symtab *SymbolTable, layout *Layout) error
```

Must be called after `AssignLayout`. Walks every defined symbol and computes
its final virtual address as:

```
VAddr = section.VAddr + piece.Offset + symbol.Value
```

Absolute symbols (`SHN_ABS`) use their raw value unchanged.

---

## Relocation patching

```go
func PatchRelocations(arch uint16, layout *Layout, symtab *SymbolTable, objects []*ObjectFile) error
```

Must be called after `ResolveSymbolAddresses`. Applies every `RELA` relocation
from every input object to the merged output section data in place.

Supported architectures and relocation types:

### AMD64

| Type | Value | Formula |
|------|-------|---------|
| `R_X86_64_NONE`  | 0  | — |
| `R_X86_64_64`    | 1  | `S + A` (64-bit) |
| `R_X86_64_PC32`  | 2  | `S + A − P` (32-bit) |
| `R_X86_64_PLT32` | 4  | `S + A − P` (32-bit; reduced to PC32 for local symbols) |
| `R_X86_64_32`    | 10 | `S + A` (zero-extend to 64) |
| `R_X86_64_32S`   | 11 | `S + A` (sign-extend to 64) |
| `R_X86_64_PC64`  | 24 | `S + A − P` (64-bit) |

### AArch64

| Type | Value | Notes |
|------|-------|-------|
| `R_AARCH64_NONE`               | 0   | — |
| `R_AARCH64_ABS64`              | 257 | 64-bit absolute |
| `R_AARCH64_ABS32`              | 258 | 32-bit absolute |
| `R_AARCH64_PREL32`             | 261 | 32-bit PC-relative |
| `R_AARCH64_ADR_PREL_PG_HI21`  | 275 | ADRP: 21-bit page delta |
| `R_AARCH64_ADD_ABS_LO12_NC`   | 277 | ADD: low 12 bits |
| `R_AARCH64_LDST8_ABS_LO12_NC` | 278 | LDR/STR byte: low 12 bits |
| `R_AARCH64_LDST64_ABS_LO12_NC`| 286 | LDR/STR 64-bit: bits [11:3] |
| `R_AARCH64_JUMP26`             | 282 | B: 26-bit branch |
| `R_AARCH64_CALL26`             | 283 | BL: 26-bit branch |

### RISC-V

| Type | Value | Notes |
|------|-------|-------|
| `R_RISCV_NONE`        | 0  | — |
| `R_RISCV_32`          | 1  | 32-bit absolute |
| `R_RISCV_64`          | 2  | 64-bit absolute |
| `R_RISCV_JAL`         | 17 | J-type: 20-bit PC-relative |
| `R_RISCV_CALL`        | 18 | AUIPC+JALR pair (8 bytes) |
| `R_RISCV_CALL_PLT`    | 19 | Like CALL, through PLT |
| `R_RISCV_PCREL_HI20`  | 23 | AUIPC `%pcrel_hi` |
| `R_RISCV_PCREL_LO12_I`| 24 | I-type `%pcrel_lo` |
| `R_RISCV_PCREL_LO12_S`| 25 | S-type `%pcrel_lo` |
| `R_RISCV_HI20`        | 26 | LUI `%hi` |
| `R_RISCV_LO12_I`      | 27 | I-type `%lo` |
| `R_RISCV_LO12_S`      | 28 | S-type `%lo` |

Undefined weak symbols resolve to zero in all relocation formulas. Shared
library symbols that have been assigned a PLT stub address are patched using
that address.

---

## Raw types

### RawSection

```go
type RawSection struct {
    Name    string
    Type    uint32
    Flags   uint64
    Data    []byte   // nil for SHT_NOBITS
    Size    uint64
    Align   uint64
    Link    uint32
    Info    uint32
    EntSize uint64
    Index   int      // position in the input section header table
}
```

### RawSymbol

```go
type RawSymbol struct {
    Name        string
    Value       uint64
    Size        uint64
    Bind        uint8   // stbLocal / stbGlobal / stbWeak
    Type        uint8   // sttFunc / sttObject / …
    Vis         uint8   // STV_DEFAULT etc.
    ShndxRaw    uint16  // raw st_shndx value
    SectionName string  // decoded: "", "*ABS*", "*COMMON*", or section name
}
```

### RawReloc

```go
type RawReloc struct {
    TargetSecIdx int    // index of the section being patched
    Offset       uint64
    SymIdx       uint32
    Type         uint32
    Addend       int64
}
```

### DynSymbol

```go
type DynSymbol struct {
    Name    string
    Value   uint64
    Size    uint64
    Bind    uint8
    Type    uint8
    Version string // e.g. "GLIBC_2.17"; empty if unversioned
}
```

---

## Error conditions

| Error | Cause |
|-------|-------|
| `undefined reference to "foo"` | Strong undefined symbol not resolved by any input |
| `duplicate definition of "foo"` | Two `STB_GLOBAL` hard definitions for the same name |
| `not an ELF file` | Input does not start with `\x7fELF` |
| `not ELF64` | Input is 32-bit ELF |
| `only little-endian ELF supported` | Input uses big-endian encoding |
| `not a relocatable object` | Object parser received a non-`ET_REL` file |
| `AMD64 reloc type N: value 0x… overflows int32` | Branch or PC-relative target out of 32-bit range |
| `AArch64 CALL/JUMP26: branch too far` | Branch target beyond ±128 MiB |
| `RISC-V JAL: target out of range` | JAL target beyond ±1 MiB |
| `shared library "foo.so" not found` | Transitive `DT_NEEDED` dependency could not be located |

---

## Notes

**Archive extraction order.** Following the classical Unix linker model,
archive members are only extracted when they resolve an existing undefined
symbol. If object files that reference a symbol appear on the command line
*after* the archive containing its definition, the symbol will go unresolved.
Add objects before archives, or list archives more than once if necessary.

**Weak symbols.** `STB_WEAK` undefined symbols do not trigger archive
extraction and always resolve successfully — to zero if no definition is found.
`STB_WEAK` definitions are silently overridden by any `STB_GLOBAL` definition
of the same name.

**Common blocks.** `SHN_COMMON` (tentative C definitions) are lower-priority
than hard definitions. When multiple common blocks exist for the same symbol,
the one with the largest `Size` wins. A hard definition from any `.o` file
silently discards all common blocks for that symbol.

**RISC-V `e_flags`.** The ELF spec requires a non-zero `e_flags` for
`EM_RISCV`. Call `SetEFlags` with the appropriate `EF_RISCV_*` combination
before linking, or the output binary will be technically malformed.

**Page size.** Virtual address layout uses a 2 MiB `PT_LOAD` alignment
(`0x200000`), which is the standard Linux default. Binaries intended for
systems with a different page size will need a modified `pageSize` constant in
`merge.go`.