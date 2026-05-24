# linker

Resolve symbols, merge sections, and produce format-native Images for use with the `bin` package.

`linker` is the step between object files and a finished binary. It ingests one or more object files, resolves every symbol reference, merges sections, assigns virtual addresses, and applies relocations. The result is a format-specific Image — patched, laid out, and ready to hand to `bin` for serialization.

---

## When you need linker

You need the linker when any of the following are true:

- You have more than one object file whose sections need to be merged
- Your code references external symbols (functions or data from shared libraries or system libraries)
- Your output format requires format-specific link-time work: PLT/GOT allocation for ELF, IAT construction for PE, bind opcode emission and two-level namespace resolution for Mach-O

You do **not** need the linker if your machine code is self-contained. A single-object program that talks to the kernel directly via syscalls with no external symbol references can skip this package entirely and hand sections straight to `bin`:

```
# No linker needed
machine code → bin/elf → executable

# Linker needed
object A + object B + libc.so → linker/elf → ELFImage → bin/elf → executable
```

---

## Architecture

```
linker/
├── linker.go       # Shared core: symbol table, section merging,
│                   # archive ingestion, relocation patching
├── elf/
│   ├── link.go     # PLT/GOT allocation, dynamic section,
│   │               # DT_NEEDED, symbol visibility, version scripts
│   └── image.go    # ELFImage — consumed by bin/elf
├── pe/
│   ├── link.go     # IAT construction, import lib (.lib) generation,
│   │               # delay-load, subsystem, stack/heap sizing
│   └── image.go    # PEImage — consumed by bin/pe
└── macho/
    ├── link.go     # Two-level namespace resolution, bind opcodes,
    │               # install name handling, rpath
    └── image.go    # MachOImage — consumed by bin/macho
```

The shared core handles everything format-agnostic. The format sub-packages call into it and layer on their own linking concerns on top.

---

## ELF

```go
import (
    linker_elf "github.com/vertex-language/compiler/linker/elf"
    "github.com/vertex-language/compiler/bin/elf"
)

img, err := linker_elf.Link(objects, linker_elf.Options{
    Entry:  "main",
    Rpath:  "/usr/local/lib",
})

out, err := elf.Emit(img)
os.WriteFile("program", out, 0o755)
```

### ELF-specific options
| Option          | Description                                      |
|-----------------|--------------------------------------------------|
| `Entry`         | Entry point symbol name                          |
| `Soname`        | Output soname (shared libraries)                 |
| `Rpath`         | Runtime library search path                      |
| `ExportDynamic` | Mark all globals as dynamic exports              |
| `VersionScript` | Symbol version assignments                       |
| `Static`        | Produce a static executable (no dynamic linker)  |

### What ELF link-time does
- Allocates PLT entries and GOT slots for every undefined dynamic symbol
- Builds the `.dynamic` section and `DT_NEEDED` entries for shared library imports
- Resolves symbol visibility (default / hidden / protected)
- Handles archive (`.a`) member pulling on demand

---

## PE

```go
import (
    linker_pe "github.com/vertex-language/compiler/linker/pe"
    "github.com/vertex-language/compiler/bin/pe"
)

img, err := linker_pe.Link(objects, linker_pe.Options{
    Entry:     "main",
    Subsystem: pe.SubsystemConsole,
    StackSize: 8 << 20,
})

out, err := pe.Emit(img)
os.WriteFile("program.exe", out, 0o755)
```

### PE-specific options
| Option        | Description                                           |
|---------------|-------------------------------------------------------|
| `Entry`       | Entry point symbol name                               |
| `Subsystem`   | `SubsystemConsole`, `SubsystemWindows`, `SubsystemEFI`|
| `StackSize`   | Stack reserve size in bytes                           |
| `HeapSize`    | Heap reserve size in bytes                            |
| `DLL`         | Emit a DLL instead of an executable                   |
| `DelayLoad`   | DLLs to delay-load                                    |

### What PE link-time does
- Constructs the Import Address Table (IAT) from all `__imp_*` references
- Groups imports by DLL; resolves by name or ordinal
- Generates import library (`.lib`) stubs if `EmitImportLib` is set
- Handles delay-load import thunks
- Applies base relocations if the image is not at its preferred base

---

## Mach-O

```go
import (
    linker_macho "github.com/vertex-language/compiler/linker/macho"
    "github.com/vertex-language/compiler/bin/macho"
)

img, err := linker_macho.Link(objects, linker_macho.Options{
    Entry:       "_main",
    InstallName: "@rpath/libfoo.dylib",
    Rpaths:      []string{"@executable_path/../lib"},
})

out, err := macho.Emit(img)
os.WriteFile("program", out, 0o755)
```

### Mach-O-specific options
| Option         | Description                                          |
|----------------|------------------------------------------------------|
| `Entry`        | Entry point symbol name                              |
| `InstallName`  | Dylib install name (shared libraries)                |
| `Rpaths`       | `@rpath` search paths added as `LC_RPATH`            |
| `DeadStrip`    | Remove unreachable sections                          |

### What Mach-O link-time does
- Resolves the two-level namespace: tracks which dylib each symbol came from
- Emits bind opcodes (`LC_DYLD_INFO`) for dynamic symbol fixups
- Handles `@rpath`, `@executable_path`, `@loader_path` in dylib paths
- Builds `LC_LOAD_DYLIB` commands for all referenced dylibs

---

## Shared core

The shared core in `linker/linker.go` handles everything that is the same regardless of output format:

- **Symbol table** — global, local, weak, and undefined symbol tracking
- **Section merging** — combine same-named sections across all input objects
- **Archive ingestion** — pull members from `.a` / `.lib` archives on demand as undefined symbols are encountered
- **Virtual address assignment** — lay sections out in memory with correct alignment
- **Relocation patching** — walk every relocation record and patch the section data in place once all addresses are known

Format sub-packages call into the core, then handle their own concerns on top.

---

## The Image types

Each format sub-package produces its own Image type. These are the bridge between `linker` and `bin` — they carry the fully resolved, patched, format-specific data that `bin` needs to serialize:

| Type                                                        | Consumed by  |
|-------------------------------------------------------------|--------------|
| `github.com/vertex-language/compiler/linker/elf.ELFImage`  | `bin/elf`    |
| `github.com/vertex-language/compiler/linker/pe.PEImage`    | `bin/pe`     |
| `github.com/vertex-language/compiler/linker/macho.MachOImage` | `bin/macho` |

By the time an Image is produced, all relocations are applied. `bin` sees only flat, patched section data and format metadata — no relocation records, no unresolved symbols.

---

## What linker is not

- **Not a compiler or assembler.** It does not generate or validate machine code.
- **Not required.** If your code is self-contained and kernel-direct, skip this package and use `bin` directly.
- **Not a loader.** It does not execute or map binaries into memory.

---

## License

MIT