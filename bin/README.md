# bin

Construct and emit native executable binaries.

`bin` takes sections, symbols, and data — and serializes them into a valid
binary file. It has no knowledge of how the code was compiled, how symbols
were resolved, or whether a linker was involved at all. If you can describe
what goes in the binary, `bin` can write it.

---

## Pipeline

```
your compiler
      │
      ▼
   linker          (symbol resolution, section merging, relocation patching)
      │
      ▼
    bin/elf  (or bin/pe, bin/macho, …)
      │
      ▼
  executable binary
```

The linker drives `bin` the same way any other caller does — through the
builder methods (`AddSection`, `AddSymbol`, `AddReloc`, …) and a final
`Emit()`. There is no special linker interface; the builder pattern is the
interface.

---

## When you need bin directly

Not every program needs a linker. If your machine code is self-contained —
talking to the kernel directly via syscalls, with no external symbols to
resolve and no object files to merge — you can skip the linker entirely and
hand your sections straight to `bin`.

This is the common case for:
- Programs that call the kernel directly (Linux syscalls, Windows Native API)
- Single-translation-unit compilers with no external dependencies
- Embedded targets with no dynamic linking
- Any output where you control every byte of every section

---

## Formats

| Package                                                        | Format    | Platform              |
|----------------------------------------------------------------|-----------|-----------------------|
| `github.com/vertex-language/compiler/bin/elf`                  | ELF64     | Linux, *BSD           |
| `github.com/vertex-language/compiler/bin/pe`                   | PE32+     | Windows               |
| `github.com/vertex-language/compiler/bin/macho`                | Mach-O 64 | macOS, iOS            |
| `github.com/vertex-language/compiler/bin/bootloader/raw`       | Flat bin  | Bare metal, stage 1   |
| `github.com/vertex-language/compiler/bin/bootloader/uefi`      | PE32+     | UEFI firmware         |

---

## ELF

```go
import "github.com/vertex-language/compiler/bin/elf"

b := elf.NewBuilder(elf.ArchAMD64)

b.AddSection(elf.Section{
    Name:  ".text",
    Type:  elf.SHT_PROGBITS,
    Flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR,
    Data:  machineCode,
    Align: 16,
})

b.AddSection(elf.Section{
    Name:  ".data",
    Type:  elf.SHT_PROGBITS,
    Flags: elf.SHF_ALLOC | elf.SHF_WRITE,
    Data:  initializedData,
    Align: 8,
})

b.AddSymbol(elf.Symbol{
    Name:    "main",
    Section: ".text",
    Offset:  0,
    Global:  true,
})

b.SetEntry("main")

out, err := b.Emit()
os.WriteFile("program", out, 0o755)
```

For dynamic linking, set an interpreter path and declare library dependencies:

```go
b.SetInterp("/lib64/ld-linux-x86-64.so.2")
b.AddNeeded("libc.so.6")
```

PLT/GOT machinery is available via `elf.NewDynBuilder` for finer control over
lazy binding stubs and GOT slots.

### Supported
- ELF64 only
- Static (`ET_EXEC`) and dynamic (`ET_DYN`) executables, shared libraries
- PLT / GOT / RELA for dynamic imports
- TLS sections (`.tdata`, `.tbss`)
- Program header layout (PT_LOAD, PT_DYNAMIC, PT_INTERP, PT_GNU_STACK, PT_TLS)
- Section and symbol string tables (auto-generated)
- Custom program headers via `AddSegment`

### Arch
| Constant           | `e_machine`    |
|--------------------|----------------|
| `elf.ArchAMD64`    | `EM_X86_64`    |
| `elf.ArchARM64`    | `EM_AARCH64`   |
| `elf.ArchRISCV64`  | `EM_RISCV`     |

---

## PE

```go
import "github.com/vertex-language/compiler/bin/pe"

b := pe.NewBuilder(pe.ArchAMD64)

b.AddSection(pe.Section{
    Name:  ".text",
    Chars: pe.IMAGE_SCN_CNT_CODE | pe.IMAGE_SCN_MEM_EXECUTE | pe.IMAGE_SCN_MEM_READ,
    Data:  machineCode,
    Align: 16,
})

b.SetEntry("main")
b.SetSubsystem(pe.SubsystemConsole)

out, err := b.Emit()
os.WriteFile("program.exe", out, 0o755)
```

DLL mode, imports, and exports are all supported:

```go
// DLL with an export
b.SetDLL("mylib.dll")
b.AddExport(pe.Export{Name: "MyFunc", Symbol: "MyFunc", Ordinal: 1})

// Importing from another DLL
b.AddImport(pe.Import{
    DLL: "kernel32.dll",
    Symbols: []pe.ImportSymbol{
        {Name: "ExitProcess"},
        {Name: "GetStdHandle"},
    },
})
```

The builder synthesizes `.idata`, `.edata`, `.reloc`, and `.debug` sections
automatically.

### Supported
- PE32+ (64-bit) only
- Executables and DLLs
- Import Address Table (`.idata`) with name and ordinal imports
- Export directory (`.edata`)
- Base relocations (`.reloc`); required whenever `DYNAMIC_BASE` is set
- Debug directory passthrough (`.debug`)

### Defaults
| Setting              | Value                                          |
|----------------------|------------------------------------------------|
| Subsystem            | `SubsystemConsole`                             |
| DllCharacteristics   | `HIGH_ENTROPY_VA \| DYNAMIC_BASE \| NX_COMPAT` |
| Image base (EXE)     | `0x0000000140000000`                           |
| Image base (DLL)     | `0x0000000180000000`                           |
| Stack reserve/commit | 1 MiB / 4 KiB                                  |
| Heap reserve/commit  | 1 MiB / 4 KiB                                  |

### Arch
| Constant        | `Machine`                    |
|-----------------|------------------------------|
| `pe.ArchAMD64`  | `IMAGE_FILE_MACHINE_AMD64`   |
| `pe.ArchARM64`  | `IMAGE_FILE_MACHINE_ARM64`   |

---

## Mach-O

```go
import "github.com/vertex-language/compiler/bin/macho"

b := macho.NewBuilder(macho.ArchAMD64)

b.AddSegment(macho.Segment{
    Name: "__TEXT",
    Prot: macho.ProtRead | macho.ProtExec,
    Sections: []macho.Section{
        {Name: "__text", Data: machineCode, Align: 4},
    },
})

b.AddSymbol(macho.Symbol{
    Name:        "_main",
    SegmentName: "__TEXT",
    SectionName: "__text",
    Global:      true,
})

b.SetEntry("_main")

out, err := b.Emit()
os.WriteFile("program", out, 0o755)
```

Dynamic library dependencies are declared with `AddDylib`:

```go
b.AddDylib(macho.DylibRef{
    Path:           "/usr/lib/libSystem.B.dylib",
    CurrentVersion: 0x050F0400,
    CompatVersion:  0x00010000,
})
```

A `__PAGEZERO` segment `[0, 4 GiB)` and a `__LINKEDIT` segment (symbol table,
string table, relocations) are synthesized automatically. For dylib output,
call `b.SetDylib()` before `Emit`.

### Supported
- 64-bit Mach-O only (`MH_EXECUTE` and `MH_DYLIB`)
- Load commands: `LC_SEGMENT_64`, `LC_SYMTAB`, `LC_DYSYMTAB`,
  `LC_LOAD_DYLINKER`, `LC_LOAD_DYLIB`, `LC_MAIN`
- Symbol and string tables (`__LINKEDIT`)
- Section-level relocations

### Arch
| Constant             | `cputype`         |
|----------------------|-------------------|
| `macho.ArchAMD64`    | `CPU_TYPE_X86_64` |
| `macho.ArchARM64`    | `CPU_TYPE_ARM64`  |

---

## Bootloader

The `bootloader` namespace covers binary output that targets hardware directly
rather than an operating system. It is split into two independent packages:
`raw` for flat stage 1 binaries, and `uefi` for firmware-loaded UEFI images.

### raw

```go
import "github.com/vertex-language/compiler/bin/bootloader/raw"
```

Raw output is flat machine code at a fixed origin address — no file format
header, no metadata, no wrapper of any kind. The CPU resets, jumps to a known
address, and starts executing bytes. This is always the format for stage 1
bootloaders regardless of what comes after.

```go
b := raw.NewBuilder()
b.SetOrigin(0x7C00)

b.AddSection(raw.Section{
    Data:  machineCode,
    Align: 1,
})

b.AddSymbol(raw.Symbol{Name: "entry", Offset: 0})
b.SetBootSignature() // writes 0x55AA at bytes 510–511; implies PadSize(512)

out, err := b.Emit()
os.WriteFile("stage1.bin", out, 0o644)
```

`SetOrigin` is the physical load address. All absolute relocations are
resolved against it. `SetPadSize(n)` pads the output to exactly `n` bytes;
`Emit` returns an error if the unpadded binary exceeds that size.

### Relocations (raw)
| Type       | Width  | Formula                                   |
|------------|--------|-------------------------------------------|
| `R_ABS8`   | 1 byte | `sym + addend`                            |
| `R_ABS16`  | 2 byte | `sym + addend`                            |
| `R_ABS32`  | 4 byte | `sym + addend`                            |
| `R_REL8`   | 1 byte | `sym − (patch + 1) + addend`              |
| `R_REL16`  | 2 byte | `sym − (patch + 2) + addend`              |
| `R_REL32`  | 4 byte | `sym − (patch + 4) + addend`              |
| `R_SEG16`  | 2 byte | `(sym + addend) >> 4` (real-mode segment) |

---

### uefi

```go
import "github.com/vertex-language/compiler/bin/bootloader/uefi"
```

UEFI executables are PE32+ binaries loaded by the platform firmware before
any OS is present. `uefi.NewBuilder` produces a valid UEFI image with the
required constraints enforced: EFI subsystem, mandatory base relocations so
firmware can load the image at any address, and a minimal 64-byte MZ stub
(no MS-DOS program bytes).

```go
b := uefi.NewBuilder(uefi.ArchAMD64, uefi.SubsystemEFIApplication)

b.AddSection(uefi.Section{
    Name:  ".text",
    Chars: uefi.IMAGE_SCN_CNT_CODE | uefi.IMAGE_SCN_MEM_EXECUTE | uefi.IMAGE_SCN_MEM_READ,
    Data:  machineCode,
    // Annotate every 64-bit absolute pointer for .reloc generation:
    Relocs: []uefi.Reloc{
        {Offset: ptrOffset, Type: uefi.IMAGE_REL_BASED_DIR64},
    },
})

b.SetEntry("EfiMain")

out, err := b.Emit()
os.WriteFile("bootx64.efi", out, 0o755)
```

The `.reloc` section is always emitted, even when there are no annotated
relocations, because UEFI firmware requires the base-relocation data
directory entry to be present and valid.

The builder always enforces:
- `DYNAMIC_BASE | NX_COMPAT | NO_SEH` in `DllCharacteristics`
- `SectionAlignment` fixed at 4096 (UEFI CA memory-mitigation requirement)
- No section may combine `IMAGE_SCN_MEM_WRITE` and `IMAGE_SCN_MEM_EXECUTE`

UEFI images do not use DLL imports — all firmware services are accessed
through the `EFI_SYSTEM_TABLE` pointer passed to the entry point.

#### Subsystems
| Constant                              | Use                                        |
|---------------------------------------|--------------------------------------------|
| `uefi.SubsystemEFIApplication`        | Bootloaders, boot managers                 |
| `uefi.SubsystemEFIBootService`        | Boot-time drivers                          |
| `uefi.SubsystemEFIRuntime`            | Drivers that survive `ExitBootServices`    |

#### Arch
| Constant           | Machine                        |
|--------------------|--------------------------------|
| `uefi.ArchAMD64`   | `IMAGE_FILE_MACHINE_AMD64`     |
| `uefi.ArchARM64`   | `IMAGE_FILE_MACHINE_ARM64`     |

---

## Relocations

Relocations are format- and arch-specific. Each sub-package defines its own
relocation type constants. Relocations are only needed if you are managing
cross-section references yourself — if you came through the `linker` package,
relocations are already applied and the sections it hands to `bin` carry
patched data.

```go
// ELF AMD64
elf.Reloc{Section: ".text", Offset: 12, Symbol: "puts", Type: elf.R_X86_64_PLT32, Addend: -4}

// PE AMD64
pe.Reloc{Section: ".text", Offset: 8, Symbol: "ExitProcess", Type: pe.IMAGE_REL_AMD64_REL32}

// Mach-O ARM64
macho.Reloc{Section: "__text", Offset: 4, Symbol: "_printf", Type: uint8(macho.ARM64_RELOC_BRANCH26),
    PCRel: true, Length: 2, Extern: true}

// raw (flat binary)
raw.Reloc{Section: ".text", Offset: 1, Symbol: "entry", Type: raw.R_REL8}
```

---

## Package layout

```
github.com/vertex-language/compiler/bin/
├── elf/
│   ├── builder.go      # Builder, Emit
│   ├── sections.go     # Section, Symbol, Reloc, all ELF constants
│   ├── program.go      # Segment / custom program header
│   ├── dynamic.go      # DynBuilder: PLT, GOT, RELA, .dynamic
│   ├── reloc_amd64.go  # R_X86_64_* constants
│   └── reloc_arm64.go  # R_AARCH64_* constants
├── pe/
│   ├── builder.go      # Builder, Emit
│   ├── sections.go     # Section, Symbol, Reloc, all PE constants
│   ├── iat.go          # buildIDATA — Import Address Table
│   └── reloc_amd64.go  # IMAGE_REL_AMD64_* constants
├── macho/
│   ├── builder.go      # Builder, Emit
│   ├── segments.go     # Segment, Section, Symbol, Reloc, DylibRef, Prot, S_* flags
│   ├── commands.go     # Load command serialization helpers
│   ├── reloc_arm64.go  # ARM64_RELOC_* and X86_64_RELOC_* constants
└── bootloader/
    ├── raw/
    │   ├── builder.go  # Builder, Emit
    │   ├── sections.go # Section, Symbol
    │   └── reloc.go    # Reloc, RelocType, R_ABS*, R_REL*, R_SEG16
    └── uefi/
        ├── builder.go  # Builder, Emit
        ├── sections.go # Section, Reloc, Subsystem, IMAGE_SCN_* flags
        └── reloc.go    # buildReloc, IMAGE_REL_BASED_* constants
```

---

## What bin is not

- **Not a linker.** Symbol resolution, section merging, and relocation
  patching across object files are not bin's concern. If you need those,
  see the `linker` package.
- **Not an assembler.** bin does not generate or validate machine code.
- **Not a loader.** bin does not execute or map binaries into memory.

---

## License

MIT