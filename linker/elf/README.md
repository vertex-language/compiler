# elf — ELF64 Static Linker

Package `elf` implements a static linker for ELF64 executables, position-independent
executables (PIE), and shared libraries. It reads ELF64 relocatable object files (`.o`),
GNU/SysV ar static libraries (`.a`), and ELF64 shared objects (`.so`), resolves symbols,
merges and lays out sections, patches RELA relocations, and emits the final image through
[`github.com/vertex-language/compiler/bin/elf`](../bin/elf).

```go
import "github.com/vertex-language/compiler/linker/elf"
```

---

## Quick start

```go
lnk := elf.NewLinker(elf.EM_X86_64)
lnk.SetEntry("main")
lnk.SetInterp("/lib64/ld-linux-x86-64.so.2")
lnk.AddObject(elf.MustOpenObject("main.o"))
lnk.AddArchive(elf.MustOpenArchive("libc.a"))
lnk.AddShared(elf.MustOpenShared("/lib/x86_64-linux-gnu/libc.so.6"))

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
| 1 | **Shared dependency walk** | BFS over `DT_NEEDED` entries of all directly-added shared libraries, loading transitive dependencies from `DT_RPATH`/`DT_RUNPATH`, linker library paths, and system default paths. |
| 2 | **Symbol resolution** | Ingests all explicit objects, shared libraries, and runs a fixed-point archive-extraction loop until no new undefined symbols can be resolved. |
| 3 | **Undefined check** | Returns an error if any strong undefined symbol (referenced from a `.o`, not just a `.so`) remains unresolved. Weak undefineds silently resolve to zero. |
| 4 | **Section merging** | Combines input sections that share a name into `MergedSection` values. Metadata sections (`.symtab`, `.strtab`, `.rela.*`, group sections) are stripped. |
| 5 | **VA assignment** | Groups sections into exec / read-only / read-write PT_LOAD segments and assigns virtual addresses and file offsets. Position-dependent executables base at `0x400000`; PIE and shared libraries base at `0x0`. |
| 6 | **Symbol addresses** | Translates every defined symbol's section-relative offset into an absolute virtual address. |
| 7 | **Relocation patching** | Applies all RELA relocation records in-place against the merged section data using architecture-specific formulas. |

---

## API reference

### Linker

```go
func NewLinker(machine uint16) *Linker
```

Creates a linker for the given ELF machine value. The default output type is
`OutputExec`.

**Configuration setters**

| Method | Description |
|--------|-------------|
| `SetOutputType(t OutputType)` | Switch between `OutputExec`, `OutputPIE`, and `OutputShared`. |
| `SetEntry(name string)` | Entry-point symbol name. |
| `SetInterp(path string)` | ELF interpreter path written into `PT_INTERP` (e.g. `/lib64/ld-linux-x86-64.so.2`). |
| `SetSoname(name string)` | `DT_SONAME` for shared library output. |
| `SetRpath(path string)` | `DT_RPATH` / `DT_RUNPATH` to embed in the output. |
| `SetEFlags(f uint32)` | `e_flags` value forwarded to `bin/elf` (required for RISC-V ABI variants). |
| `AddLibraryPath(path string)` | Append a directory to the shared library search path. |

**Input**

```go
func (l *Linker) AddObject(o *ObjectFile)
func (l *Linker) AddArchive(a *Archive)
func (l *Linker) AddShared(s *SharedLib)
```

**Link**

```go
func (l *Linker) Link() (*LinkResult, error)
```

---

### Parsing

```go
// Relocatable objects
func OpenObject(path string) (*ObjectFile, error)
func MustOpenObject(path string) *ObjectFile
func ParseObject(data []byte) (*ObjectFile, error)

// Static libraries
func OpenArchive(path string) (*Archive, error)
func MustOpenArchive(path string) *Archive
func ParseArchive(data []byte) (*Archive, error)

// Shared libraries
func OpenShared(path string) (*SharedLib, error)
func MustOpenShared(path string) *SharedLib
func ParseShared(data []byte) (*SharedLib, error)
```

`ParseObject` accepts only `ET_REL` files. `ParseShared` accepts `ET_DYN` and
`ET_EXEC` files and extracts the dynamic symbol table, `DT_NEEDED` entries,
`DT_SONAME`, and `DT_RPATH`/`DT_RUNPATH`.

`ParseArchive` handles GNU/SysV ar format including the `/` (32-bit) and
`/SYM64/` (64-bit) symbol-index members and the `//` long-name table. Archives
without a pre-built symbol table are indexed by exhaustive member scan.

---

### Output types

| Constant | `e_type` | Description |
|----------|----------|-------------|
| `OutputExec` | `ET_EXEC` | Position-dependent executable; base VA `0x400000`. |
| `OutputPIE` | `ET_DYN` | Position-independent executable; base VA `0x0`. |
| `OutputShared` | `ET_DYN` | Shared library; base VA `0x0`. |

---

### LinkResult

`Link()` returns a `*LinkResult` on success. The most common use is:

```go
b := result.Builder()   // returns a pre-configured bin/elf.Builder
out, err := b.Emit()
```

`LinkResult` also exposes the full intermediate state for inspection or
post-processing:

| Field | Type | Description |
|-------|------|-------------|
| `Layout` | `*Layout` | Merged sections with virtual addresses and file offsets. |
| `Symtab` | `*SymbolTable` | Global symbol table; call `Lookup(name)` by name. |
| `Needed` | `[]string` | `DT_NEEDED` sonames in BFS load order, deduplicated. |
| `Arch` | `binelf.Arch` | Architecture forwarded to `bin/elf`. |
| `OutputType` | `OutputType` | Exec / PIE / shared. |
| `Entry` | `string` | Entry-point symbol name. |
| `Interp` | `string` | ELF interpreter path. |
| `Soname` | `string` | `DT_SONAME` (shared output only). |
| `Rpath` | `string` | `DT_RPATH` to embed. |
| `EFlags` | `uint32` | `e_flags` (RISC-V ABI variant flags). |

---

### Symbol table

```go
func (t *SymbolTable) Lookup(name string) *Symbol
func (t *SymbolTable) All() []*Symbol
```

`Lookup` returns the `*Symbol` entry for `name`, or `nil` if absent.
`All` returns every symbol regardless of kind; order is unspecified.

Each `Symbol` carries a `VAddr` field populated after `ResolveSymbolAddresses`
and a `Kind` that reflects the resolution outcome:

| Kind | Meaning |
|------|---------|
| `kindUndefined` | Referenced but unresolved. |
| `kindLazy` | Available in an archive not yet extracted. |
| `kindShared` | Satisfied by a shared library. |
| `kindCommon` | Tentative C common block. |
| `kindDefined` | Hard definition from a relocatable object. |

**Resolution rules** (highest precedence wins):

- `Defined` > `Common` > `Shared` > `Lazy` > `Undefined`
- `STB_GLOBAL` overrides `STB_WEAK` among `kindDefined` entries.
- Two `STB_GLOBAL` definitions → duplicate definition error.
- `STB_WEAK` undefined → resolves to zero, no error, no archive extraction.

---

### Supported relocations

#### AMD64 (`EM_X86_64 = 0x3E`)

| Type | Name | Formula |
|------|------|---------|
| 0 | `R_X86_64_NONE` | — |
| 1 | `R_X86_64_64` | `S + A` |
| 2 | `R_X86_64_PC32` | `S + A - P` |
| 4 | `R_X86_64_PLT32` | `S + A - P` (reduced to PC32 for local symbols) |
| 10 | `R_X86_64_32` | `S + A` (zero-extended) |
| 11 | `R_X86_64_32S` | `S + A` (sign-extended) |
| 24 | `R_X86_64_PC64` | `S + A - P` |

#### AArch64 (`EM_AARCH64 = 0xB7`)

| Type | Name | Formula |
|------|------|---------|
| 0 | `R_AARCH64_NONE` | — |
| 257 | `R_AARCH64_ABS64` | `S + A` |
| 258 | `R_AARCH64_ABS32` | `S + A` |
| 261 | `R_AARCH64_PREL32` | `S + A - P` |
| 275 | `R_AARCH64_ADR_PREL_PG_HI21` | `Page(S+A) - Page(P)` → ADRP |
| 277 | `R_AARCH64_ADD_ABS_LO12_NC` | `(S+A)[11:0]` → ADD imm12 |
| 278 | `R_AARCH64_LDST8_ABS_LO12_NC` | `(S+A)[11:0]` → load/store offset |
| 282 | `R_AARCH64_JUMP26` | `(S+A-P)[27:2]` → B |
| 283 | `R_AARCH64_CALL26` | `(S+A-P)[27:2]` → BL |
| 286 | `R_AARCH64_LDST64_ABS_LO12_NC` | `(S+A)[11:3]` → 64-bit load/store |

#### RISC-V (`EM_RISCV = 0xF3`)

| Type | Name | Formula |
|------|------|---------|
| 0 | `R_RISCV_NONE` | — |
| 1 | `R_RISCV_32` | `S + A` |
| 2 | `R_RISCV_64` | `S + A` |
| 17 | `R_RISCV_JAL` | `S + A - P` → J-type |
| 18/19 | `R_RISCV_CALL` / `R_RISCV_CALL_PLT` | `S + A - P` → AUIPC+JALR pair |
| 23 | `R_RISCV_PCREL_HI20` | `%pcrel_hi(S+A-P)` → AUIPC |
| 24 | `R_RISCV_PCREL_LO12_I` | `%pcrel_lo(S+A)` → I-type |
| 25 | `R_RISCV_PCREL_LO12_S` | `%pcrel_lo(S+A)` → S-type |
| 26 | `R_RISCV_HI20` | `%hi(S+A)` → LUI |
| 27 | `R_RISCV_LO12_I` | `%lo(S+A)` → I-type |
| 28 | `R_RISCV_LO12_S` | `%lo(S+A)` → S-type |

---

## Section layout

Sections are grouped into PT_LOAD segments in the following order, with a
page-aligned boundary between each group:

```
[exec]        .text, .plt, and any SHF_EXECINSTR sections
[read-only]   .rodata, .eh_frame, and other SHF_ALLOC sections without write or exec
[read-write]  .data, .bss, .got, .got.plt, and other SHF_ALLOC | SHF_WRITE sections
[file-only]   .debug_*, .symtab, .strtab, and other non-allocatable sections
```

The first PT_LOAD segment starts at file offset `0x1000` (one page). Virtual
addresses for position-dependent executables are offset by `0x400000`; PIE and
shared libraries start at `0x0`.

---

## Supported machines

| Constant | Value | Architecture |
|----------|-------|--------------|
| `EM_X86_64` | `0x3E` | x86-64 |
| `EM_AARCH64` | `0xB7` | AArch64 |
| `EM_RISCV` | `0xF3` | RISC-V (64-bit) |