# macho — Mach-O Static Linker

Package `macho` implements a static linker for macOS Mach-O executables,
dynamic libraries, and bundles. It reads MH_OBJECT relocatable files (`.o`),
static archives (`.a`), and MH_DYLIB dynamic libraries, resolves symbols,
merges and lays out sections, synthesises stubs and GOT entries, patches
relocations, and emits the final image through
[`github.com/vertex-language/compiler/bin/macho`](../bin/macho).

```go
import "github.com/vertex-language/compiler/linker/macho"
```

---

## Quick start

```go
lnk := macho.NewLinker(macho.ArchARM64)
lnk.SetEntry("_main")
lnk.AddObject(macho.MustOpenObject("main.o"))
lnk.AddDylib(macho.MustOpenDylib("/usr/lib/libSystem.B.dylib"))

result, err := lnk.Link()
if err != nil {
    log.Fatal(err)
}
b := result.Builder()
out, err := b.Emit()
if err != nil {
    log.Fatal(err)
}
os.WriteFile("program", out, 0o755)
```

---

## Linker phases

`Linker.Link()` runs the following phases in order:

| # | Phase | Description |
|---|-------|-------------|
| 0 | **Transitive dylib resolution** | Walks `LC_LOAD_DYLIB` entries of every explicitly added dylib and attempts to open dependencies from `-L` paths and system library paths. Missing dependencies are non-fatal. |
| 1 | **Symbol resolution** | Ingests all explicit objects, then runs a fixed-point archive-extraction loop until no new undefined symbols can be resolved. Dylib exports are registered as `kindDylib` and fill weak/undefined slots. |
| 2 | **Section merging** | Combines input sections that share a `(SegName, SectName)` key into `MergedSection` values, skipping debug sections. Alignment is the maximum over all contributions. |
| 3 | **Layout assignment** | Assigns virtual addresses and file offsets to every merged section, page-aligning each segment. `MH_EXECUTE` output starts at `0x100000000`; dylib/bundle output starts at `0x0`. |
| 4 | **Symbol address resolution** | Translates every defined symbol's section-relative value into an absolute virtual address. |
| 5 | **Stub and GOT synthesis** | Scans all relocations for references to dylib symbols, allocates `__stubs` (in `__TEXT`) and `__got` (in `__DATA_CONST`) entries, re-runs layout, then patches stub byte sequences with correct GOT-relative addresses. |
| 6 | **Relocation patching** | Applies all Mach-O relocation records in-place against the merged section data. |
| 7 | **Entry point validation** | Returns an error if the requested entry symbol is not found in the symbol table. |

---

## API reference

### Linker

```go
func NewLinker(arch uint32) *Linker
```

Creates a linker for the given architecture (`ArchAMD64` or `ArchARM64`).
The default output type is `OutputExec`, the default dyld mode is chained
fixups (`DyldModeChained`), and the default platform is macOS 14.0.

**Configuration setters**

| Method | Description |
|--------|-------------|
| `SetEntry(name string)` | Entry-point symbol name for `MH_EXECUTE` output. |
| `SetOutputType(t OutputType)` | Switch between executable, dylib, and bundle output. |
| `SetInstallName(name string)` | Dylib install name written into `LC_ID_DYLIB`. |
| `SetDyldMode(m DyldMode)` | Chained fixups (default) or legacy dyld info. |
| `SetPlatform(p Platform, minOS, sdk uint32)` | `LC_BUILD_VERSION` platform and version triple. |
| `AddRpath(path string)` | Append an `LC_RPATH` entry. |
| `AddLibraryPath(path string)` | Add a directory to search for transitive dylib dependencies. |

**Input**

```go
func (l *Linker) AddObject(o *ObjectFile)
func (l *Linker) AddArchive(a *Archive)
func (l *Linker) AddDylib(d *DylibFile)
```

The order of `AddDylib` calls determines the 1-based dylib ordinals written
into the output bind information.

**Link**

```go
func (l *Linker) Link() (*LinkResult, error)
```

---

### Parsing

```go
// Relocatable object files (.o)
func OpenObject(path string) (*ObjectFile, error)
func MustOpenObject(path string) *ObjectFile
func ParseObject(data []byte) (*ObjectFile, error)

// Static archives (.a)
func OpenArchive(path string) (*Archive, error)
func MustOpenArchive(path string) *Archive
func ParseArchive(data []byte) (*Archive, error)

// Dynamic libraries (.dylib / .bundle)
func OpenDylib(path string) (*DylibFile, error)
func MustOpenDylib(path string) *DylibFile
func ParseDylib(data []byte) (*DylibFile, error)
```

`ParseObject` only accepts little-endian 64-bit `MH_OBJECT` files; big-endian
and 32-bit variants are rejected with a descriptive error. `ParseDylib` also
accepts `MH_BUNDLE` and `MH_DYLINKER` file types. `ParseArchive` handles both
GNU/SysV symbol-index archives and the BSD `__.SYMDEF` variant.

---

### LinkResult

`Link()` returns a `*LinkResult` on success. The most common path is to obtain
a pre-configured `bin/macho.Builder` and emit from it:

```go
b := result.Builder()
out, err := b.Emit()
```

`Builder()` wires up all segments, sections, symbols, dylib load commands,
RPATHs, and dynamic linking tables (chained fixups or legacy dyld info +
export trie) automatically.

`LinkResult` also exposes the full intermediate state for inspection or
post-processing:

| Field | Type | Description |
|-------|------|-------------|
| `Arch` | `uint32` | CPU type (`ArchAMD64` or `ArchARM64`). |
| `OutputType` | `OutputType` | `OutputExec`, `OutputDylib`, or `OutputBundle`. |
| `Layout` | `*Layout` | Merged sections with virtual addresses and file offsets. |
| `Symtab` | `*SymbolTable` | Unified symbol resolution table. |
| `Stubs` | `*StubTable` | Synthesised `__stubs` / `__got` entries, or nil. |
| `Dylibs` | `[]*DylibFile` | Dylib list in ordinal order (1-based). |
| `Rpaths` | `[]string` | `LC_RPATH` entries. |
| `DyldMode` | `DyldMode` | Chained fixups or legacy dyld info. |

---

### Symbol table

```go
func (t *SymbolTable) Lookup(name string) *ResolvedSym
func (t *SymbolTable) All() []*ResolvedSym
```

`Lookup` returns the `ResolvedSym` for `name`, or `nil` if not present.
`All` returns all entries in an unspecified order.

Each `ResolvedSym` carries a `Kind` that reflects its resolution precedence:

| Kind | Precedence | Source |
|------|-----------|--------|
| `kindUndef` | 0 | Unresolved reference |
| `kindLazy` | 1 | Archive member not yet extracted |
| `kindDylib` | 2 | Exported by a `.dylib` |
| `kindCommon` | 3 | Tentative definition (`N_UNDF` + `Value > 0`) |
| `kindDefined` | 4 | Hard definition from a `.o` file |

Higher-precedence kinds always win. Among `kindDefined` entries a strong
definition beats a weak one; among two weak definitions, the first wins. The
largest common block wins among `kindCommon` entries.

---

### Stub and GOT synthesis

`BuildStubs` scans every relocation across all active objects. When a
relocation targets a `kindDylib` symbol, a GOT slot (8 bytes, in
`__DATA_CONST,__got`) and a stub (in `__TEXT,__stubs`) are allocated via
`StubTable.GetOrAdd`. After `AssignLayout` runs a second time,
`FinalizeStubs` patches the stub byte sequences:

| Architecture | Stub encoding |
|-------------|--------------|
| AMD64 | 6-byte `JMP QWORD PTR [RIP+rel32]` (`FF 25 <rel32>`) |
| ARM64 | 12-byte `ADRP x16` / `LDR x16, [x16, #off]` / `BR x16` |

Stub virtual addresses are then written back into the symbol table so that
branch relocations to dylib symbols are redirected through the stub
transparently.

---

### Supported relocation types

**AMD64 (`X86_64_RELOC_*`)**

| Type | Value | Description |
|------|-------|-------------|
| `UNSIGNED` | 0 | Absolute VA (4- or 8-byte) |
| `SIGNED` | 1 | PC-relative signed 32-bit |
| `BRANCH` | 2 | PC-relative call/jump; redirected through stub for dylib symbols |
| `GOT_LOAD` | 3 | PC-relative reference to GOT slot |
| `GOT` | 4 | PC-relative reference to GOT slot |
| `SUBTRACTOR` | 5 | Paired subtraction |
| `SIGNED_{1,2,4}` | 6–8 | PC-relative with implicit bias |
| `TLV` | 9 | Thread-local variable descriptor (treated as GOT_LOAD) |

**ARM64 (`ARM64_RELOC_*`)**

| Type | Value | Description |
|------|-------|-------------|
| `UNSIGNED` | 0 | Absolute VA (4- or 8-byte) |
| `SUBTRACTOR` | 1 | Paired subtraction |
| `BRANCH26` | 2 | 26-bit PC-relative `BL`/`B`; redirected through stub |
| `PAGE21` | 3 | 21-bit `ADRP` page delta |
| `PAGEOFF12` | 4 | 12-bit `ADD`/`LDR`/`STR` page offset |
| `GOT_LOAD_PAGE21` | 5 | `ADRP` to GOT slot page |
| `GOT_LOAD_PAGEOFF12` | 6 | `LDR` page offset to GOT slot (÷8) |
| `POINTER_TO_GOT` | 7 | 32-bit PC-relative to GOT slot |
| `TLVP_LOAD_PAGE21` | 8 | `ADRP` to TLV descriptor page |
| `TLVP_LOAD_PAGEOFF12` | 9 | Page offset to TLV descriptor |
| `ADDEND` | 10 | Explicit addend for the following relocation |

---

## Section and segment layout

Sections are merged into segments in the following canonical order:

`__TEXT` → `__DATA_CONST` → `__DATA` → `__LINKEDIT` → *(other)*

Within a segment, sections are sorted alphabetically by their
`(SegName, SectName)` key. Each segment is page-aligned to 16 KiB
(the native ARM64 page size, also accepted by macOS x86-64). Zerofill
sections (`S_ZEROFILL`, `S_GB_ZEROFILL`, `S_THREAD_LOCAL_ZEROFILL`)
contribute to VM size but not to file size.

Synthesised sections added by `BuildStubs` are appended to their
respective segments before the second layout pass:

| Section | Segment | Purpose |
|---------|---------|---------|
| `__stubs` | `__TEXT` | Import thunk code |
| `__got` | `__DATA_CONST` | Non-lazy symbol pointers |

---

## Supported architectures

| Constant | Value | Architecture |
|----------|-------|--------------|
| `ArchAMD64` | `0x01000007` | x86-64 (`CPU_TYPE_X86_64`) |
| `ArchARM64` | `0x0100000C` | AArch64 (`CPU_TYPE_ARM64`) |

---

## Supported output types

| Constant | Mach-O file type | Description |
|----------|-----------------|-------------|
| `OutputExec` | `MH_EXECUTE` (0x2) | Standalone executable; `__PAGEZERO` at 0, `__TEXT` at `0x100000000` |
| `OutputDylib` | `MH_DYLIB` (0x6) | Position-independent dynamic library |
| `OutputBundle` | `MH_BUNDLE` (0x8) | Loadable bundle (plug-in) |