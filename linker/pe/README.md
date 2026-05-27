# pe — PE32+ Static Linker

Package `pe` implements a static linker for Windows PE32+ executables and DLLs.
It reads COFF relocatable object files (`.obj`) and COFF/GNU ar import libraries
(`.lib` / `.a`), resolves symbols, merges and lays out sections, patches COFF
relocations, synthesises import thunks, and emits the final image through
[`github.com/vertex-language/compiler/bin/pe`](../bin/pe).

```go
import "github.com/vertex-language/compiler/linker/pe"
```

---

## Quick start

```go
lnk := pe.NewLinker(pe.MachineAMD64)
lnk.SetEntry("main")
lnk.AddObject(pe.MustOpenObject("main.obj"))
lnk.AddArchive(pe.MustOpenArchive("kernel32.lib"))

result, err := lnk.Link()
if err != nil {
    log.Fatal(err)
}
out, err := result.Emit()
if err != nil {
    log.Fatal(err)
}
os.WriteFile("program.exe", out, 0o755)
```

---

## Linker phases

`Linker.Link()` runs the following phases in order:

| # | Phase | Description |
|---|-------|-------------|
| 1 | **Symbol resolution** | Ingests all explicit objects, then runs a fixed-point archive-extraction loop until no new undefined symbols can be resolved. Import stubs are pre-scanned so `__imp_*` symbols are always available. |
| 2 | **Undefined check** | Returns an error if any strong undefined symbol remains after archive extraction. |
| 3 | **Section merging** | Combines input sections that share a name into `MergedSection` values, respecting COMDAT selection rules and stripping linker-only sections (`.drectve`, `.llvm_addrsig`, `LNK_INFO`, `LNK_REMOVE`). |
| 4 | **Image base** | Picks a default image base (`.exe` → `0x140000000`, `.dll` → `0x180000000`) unless overridden with `SetImageBase`. |
| 5 | **VA assignment** | Assigns virtual addresses to every merged section and to the synthetic sections that `bin/pe` will generate (`.idata`, `.edata`, `.reloc`, load-config, debug). IAT slot RVAs are computed here. |
| 6 | **Symbol addresses** | Translates every defined symbol's section-relative offset into an absolute VA. |
| 7 | **Thunk synthesis** | Generates a `.text$thk` section containing `jmp [__imp_*]` stubs for every directly-called import (AMD64: `FF /4`; ARM64: `adrp / ldr / br`). |
| 8 | **Relocation patching** | Applies all COFF relocation records in-place against the merged section data. |

---

## API reference

### Linker

```go
func NewLinker(machine MachineType) *Linker
```

Creates a linker for the given machine (`MachineAMD64` or `MachineARM64`).
ASLR (`DYNAMIC_BASE`, `HIGH_ENTROPY_VA`), NX (`NX_COMPAT`), and CFG
(`GUARD_CF`) are enabled by default. The default subsystem is
`SubsystemWindowsCUI` and the default OS/subsystem version is 6.0.

**Configuration setters**

| Method | Description |
|--------|-------------|
| `SetEntry(name string)` | Entry-point symbol name. |
| `SetSubsystem(ss Subsystem)` | PE subsystem (CUI, GUI, …). |
| `SetImageBase(base uint64)` | Override the default image base. |
| `SetDLL(name string)` | Switch to DLL mode; `name` is the DLL filename written into `.edata`. |
| `SetDynamicBase(v bool)` | Enable or disable `.reloc` generation. |
| `SetDllCharacteristics(f uint16)` | Replace the DLL-characteristics flags word. |
| `AddDllCharacteristics(f uint16)` | OR additional flags into the characteristics word. |
| `SetStackSize(reserve, commit uint64)` | Override stack reserve/commit. |
| `SetHeapSize(reserve, commit uint64)` | Override heap reserve/commit. |
| `SetOSVersion(major, minor uint16)` | Required OS version in the optional header. |
| `SetSubsystemVersion(major, minor uint16)` | Required subsystem version. |
| `SetLoadConfig(lc binpe.LoadConfig)` | Attach a load-configuration directory. |
| `AddDebugEntry(d binpe.DebugEntry)` | Append a debug data-directory entry. |
| `AddExport(er ExportRecord)` | Register an explicit export (DLL mode). Ordinal 0 assigns sequentially from 1. |

**Input**

```go
func (l *Linker) AddObject(o *ObjectFile)
func (l *Linker) AddArchive(a *Archive)
```

**Link**

```go
func (l *Linker) Link() (*LinkResult, error)
```

---

### Parsing

```go
func OpenObject(path string) (*ObjectFile, error)
func MustOpenObject(path string) *ObjectFile
func ParseObject(data []byte) (*ObjectFile, error)

func OpenArchive(path string) (*Archive, error)
func MustOpenArchive(path string) *Archive
func ParseArchive(data []byte) (*Archive, error)

func ParseShortImport(data []byte) (*ShortImport, error)
```

`ParseObject` rejects short import stubs (Sig1=`0x0000`, Sig2=`0xFFFF`);
use `ParseArchive` — stubs inside archives are detected and parsed automatically.

---

### LinkResult

`Link()` returns a `*LinkResult` on success. The two most common uses are:

```go
// Emit the finished PE image.
out, err := result.Emit()

// Get a pre-configured bin/pe.Builder for further customisation before emit.
b := result.Builder()
```

`LinkResult` also exposes the full intermediate state for inspection or
post-processing:

| Field | Type | Description |
|-------|------|-------------|
| `Layout` | `*Layout` | Merged sections with virtual addresses. |
| `Symtab` | `*SymbolTable` | Global symbol table (call `Lookup` by name). |
| `Imports` | `[]*CollectedImport` | Deduplicated DLL import list with IAT slot RVAs. |
| `Exports` | `[]ExportRecord` | Final export table (drectve + explicit). |
| `Thunks` | `*ThunkSection` | Synthesised import-thunk code, or nil. |
| `Synth` | `SyntheticLayout` | VAs for synthetic sections and IAT slots. |

---

### Symbol table

```go
func (st *SymbolTable) Lookup(name string) uint64
```

Returns the final virtual address of `name`, or 0 if unresolved.

---

### COMDAT selection

Both the symbol table (`symbol.go`) and section merger (`merge.go`) enforce the
full set of COFF COMDAT selection rules:

| Selector | Behaviour |
|----------|-----------|
| `SELECT_NODUPLICATES` (1) | Hard error on any duplicate. |
| `SELECT_ANY` (2) | First definition wins. |
| `SELECT_SAME_SIZE` (3) | Duplicate allowed only if byte lengths match. |
| `SELECT_EXACT_MATCH` (4) | Duplicate allowed only if content is identical. |
| `SELECT_ASSOCIATIVE` (5) | Governed by the associated leader section. |
| `SELECT_LARGEST` (6) | Largest definition wins. |

---

## Section output order

Sections are emitted in the following canonical order; any remaining sections
follow alphabetically:

`.text` → `.rdata` → `.data` → `.bss` → `.pdata` → `.xdata` → `.tls` →
`.debug` → *(other)* → `.reloc`

---

## Supported machines

| Constant | Value | Architecture |
|----------|-------|--------------|
| `MachineAMD64` | `0x8664` | x86-64 |
| `MachineARM64` | `0xAA64` | AArch64 |