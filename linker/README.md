# linker — Static Linkers for PE32+, Mach-O, and ELF64

This directory contains three independent static linker packages, one for each
supported target binary format. All three share the same conceptual pipeline
(symbol resolution → section merging → layout → relocation patching → emit) and
a consistent Go API, but each is self-contained and has no dependency on the
others.

| Package | Target OS | Binary format | Import path |
|---------|-----------|---------------|-------------|
| [`pe`](./pe) | Windows | PE32+ (`.exe` / `.dll`) | `github.com/vertex-language/compiler/linker/pe` |
| [`macho`](./macho) | macOS | Mach-O (`MH_EXECUTE` / `MH_DYLIB` / `MH_BUNDLE`) | `github.com/vertex-language/compiler/linker/macho` |
| [`elf`](./elf) | Linux / other Unix | ELF64 (`ET_EXEC` / `ET_DYN`) | `github.com/vertex-language/compiler/linker/elf` |

---

## Supported architectures

| Architecture | `pe` | `macho` | `elf` |
|---|:---:|:---:|:---:|
| x86-64 | `MachineAMD64` | `ArchAMD64` | `EM_X86_64` |
| AArch64 | `MachineARM64` | `ArchARM64` | `EM_AARCH64` |
| RISC-V 64 | — | — | `EM_RISCV` |

---

## Common pipeline

Each linker exposes a `NewLinker` constructor, a set of configuration setters,
`AddObject` / `AddArchive` input methods, and a `Link()` call that returns a
`*LinkResult`. The result carries the full intermediate state and a
pre-configured `Builder` that emits the final byte image.

```
NewLinker → SetEntry / SetOutputType / … → AddObject / AddArchive / Add* →
Link() → result.Builder() → Emit() → write file
```

The internal phase sequence is broadly the same across all three packages:

| Phase | pe | macho | elf |
|-------|----|-------|-----|
| Transitive dependency walk | import stub pre-scan | `LC_LOAD_DYLIB` BFS | `DT_NEEDED` BFS |
| Symbol resolution + archive extraction | ✓ | ✓ | ✓ |
| Undefined symbol check | hard error | validated at end | hard error (weak → 0) |
| Section merging (with COMDAT / duplicate rules) | ✓ | ✓ | ✓ |
| VA + file-offset assignment | ✓ | ✓ | ✓ |
| Symbol address translation | ✓ | ✓ | ✓ |
| Thunk / stub + GOT synthesis | `.text$thk` | `__stubs` + `__got` | — |
| Relocation patching | COFF | Mach-O | RELA |

---

## Quick-start examples

### PE32+ (Windows)

```go
lnk := pe.NewLinker(pe.MachineAMD64)
lnk.SetEntry("main")
lnk.AddObject(pe.MustOpenObject("main.obj"))
lnk.AddArchive(pe.MustOpenArchive("kernel32.lib"))

result, err := lnk.Link()
// ...
out, _ := result.Emit()
os.WriteFile("program.exe", out, 0o755)
```

### Mach-O (macOS)

```go
lnk := macho.NewLinker(macho.ArchARM64)
lnk.SetEntry("_main")
lnk.AddObject(macho.MustOpenObject("main.o"))
lnk.AddDylib(macho.MustOpenDylib("/usr/lib/libSystem.B.dylib"))

result, err := lnk.Link()
// ...
out, _ := result.Builder().Emit()
os.WriteFile("program", out, 0o755)
```

### ELF64 (Linux)

```go
lnk := elf.NewLinker(elf.EM_X86_64)
lnk.SetEntry("main")
lnk.SetInterp("/lib64/ld-linux-x86-64.so.2")
lnk.AddObject(elf.MustOpenObject("main.o"))
lnk.AddShared(elf.MustOpenShared("/lib/x86_64-linux-gnu/libc.so.6"))

result, err := lnk.Link()
// ...
out, _ := result.Builder().Emit()
os.WriteFile("program", out, 0o755)
```

---

## Package summaries

### `pe` — PE32+ linker

Reads COFF relocatable objects (`.obj`) and COFF/GNU ar import libraries
(`.lib` / `.a`). Supports COMDAT selection rules (`ANY`, `LARGEST`,
`EXACT_MATCH`, etc.), synthesises `jmp [__imp_*]` import thunks, and emits
through `bin/pe`. ASLR, NX, and CFG are enabled by default.

→ See [`pe/README.md`](./pe/README.md) for the full API reference.

### `macho` — Mach-O linker

Reads `MH_OBJECT` files (`.o`), static archives (`.a`), and `MH_DYLIB` dynamic
libraries. Walks transitive `LC_LOAD_DYLIB` dependencies, synthesises
`__stubs` and `__got` sections for dylib references, and supports both chained
fixups (default) and legacy dyld info bind/rebase tables.

→ See [`macho/README.md`](./macho/README.md) for the full API reference.

### `elf` — ELF64 linker

Reads `ET_REL` object files (`.o`), GNU/SysV ar static libraries (`.a`), and
`ET_DYN` shared objects (`.so`). BFS-walks `DT_NEEDED` dependencies, groups
sections into `PT_LOAD` segments, and supports position-dependent executables,
PIE, and shared library output. Handles AMD64, AArch64, and RISC-V RELA
relocations.

→ See [`elf/README.md`](./elf/README.md) for the full API reference.

---

## Output types at a glance

| Goal | Package | Key setter |
|------|---------|------------|
| Windows executable | `pe` | `SetEntry("main")` |
| Windows DLL | `pe` | `SetDLL("foo.dll")` |
| macOS executable | `macho` | `SetOutputType(macho.OutputExec)` *(default)* |
| macOS dynamic library | `macho` | `SetOutputType(macho.OutputDylib)` |
| macOS loadable bundle | `macho` | `SetOutputType(macho.OutputBundle)` |
| Linux executable | `elf` | `SetOutputType(elf.OutputExec)` *(default)* |
| Linux PIE | `elf` | `SetOutputType(elf.OutputPIE)` |
| Linux shared library | `elf` | `SetOutputType(elf.OutputShared)` |