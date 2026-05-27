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

The linker drives `bin` the same way any other caller does — through builder
methods (`AddSection`, `AddSymbol`, `AddReloc`, …) and a final `Emit()`.
There is no special linker interface; the builder pattern is the interface.

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

| Package                                              | Format    | Platform            |
|------------------------------------------------------|-----------|---------------------|
| `github.com/vertex-language/compiler/bin/elf`        | ELF64     | Linux, *BSD         |
| `github.com/vertex-language/compiler/bin/pe`         | PE32+     | Windows             |
| `github.com/vertex-language/compiler/bin/macho`      | Mach-O 64 | macOS, iOS          |
| `github.com/vertex-language/compiler/bin/bootloader/raw`  | Flat bin  | Bare metal, stage 1 |
| `github.com/vertex-language/compiler/bin/bootloader/uefi` | PE32+     | UEFI firmware       |

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

**Supported:**
- ELF64 only
- Static (`ET_EXEC`) and dynamic (`ET_DYN`) executables, shared libraries
- PLT / GOT / RELA for dynamic imports
- TLS sections (`.tdata`, `.tbss`)
- Program header synthesis (`PT_LOAD`, `PT_DYNAMIC`, `PT_INTERP`, `PT_GNU_STACK`, `PT_TLS`)
- Custom program headers via `AddSegment`
- GNU hash, SysV hash, and symbol versioning tables
- Note sections (build ID, ABI tag, GNU properties)

**Architectures:**

| Constant          | `e_machine`  |
|-------------------|--------------|
| `elf.ArchAMD64`   | `EM_X86_64`  |
| `elf.ArchARM64`   | `EM_AARCH64` |
| `elf.ArchRISCV64` | `EM_RISCV`   |

---

## PE

```go
import "github.com/vertex-language/compiler/bin/pe"

b := pe.NewBuilder(pe.MachineAMD64)

b.AddSection(pe.Section{
    Name:  ".text",
    Chars: pe.ScnCode,
    Data:  machineCode,
})

b.SetEntry("main")

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

The builder synthesizes `.idata`, `.edata`, `.reloc`, `.pdata`/`.xdata`,
`.tls`, `.debug`, and `.didat` sections automatically from high-level
declarations.

**Supported:**
- PE32+ (64-bit) only
- Executables and DLLs
- Standard and delay-load imports (`.idata`, `.didat`)
- Export directory (`.edata`)
- Base relocations (`.reloc`)
- Exception unwind tables (`.pdata`/`.xdata`) via `PdataBuilder`
- Thread-local storage (`.tls`) via `TLSBuilder`
- Control Flow Guard load configuration
- Debug directories (CodeView PDB, repro hash, VC features)
- COFF object files via `ObjBuilder`
- Static archives via `ArchiveBuilder`
- Import libraries via `ImportLibBuilder`

**Defaults:**

| Setting              | Value                                                     |
|----------------------|-----------------------------------------------------------|
| Subsystem            | `SubsystemWindowsCUI` (console)                           |
| DllCharacteristics   | `HIGH_ENTROPY_VA \| DYNAMIC_BASE \| NX_COMPAT \| GUARD_CF` |
| Image base (EXE)     | `0x0000000140000000`                                      |
| Image base (DLL)     | `0x0000000180000000`                                      |
| Stack reserve/commit | 1 MiB / 4 KiB                                             |
| Heap reserve/commit  | 1 MiB / 4 KiB                                             |
| OS/Subsystem version | 6.0 (Windows Vista)                                       |

**Architectures:**

| Constant          | `Machine`                  |
|-------------------|----------------------------|
| `pe.MachineAMD64` | `IMAGE_FILE_MACHINE_AMD64` |
| `pe.MachineARM64` | `IMAGE_FILE_MACHINE_ARM64` |

---

## Mach-O

```go
import "github.com/vertex-language/compiler/bin/macho"

b := macho.NewBuilder(macho.ArchARM64)
b.SetFileType(macho.FileTypeExecute)
b.SetBuildVersion(macho.BuildVersion{
    Platform: macho.PlatformMacOS,
    MinOS:    macho.PackVersion(14, 0, 0),
    SDK:      macho.PackVersion(14, 5, 0),
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

b.AddSymbol(macho.Symbol{Name: "_main", SegmentName: "__TEXT", SectionName: "__text", Global: true})
b.SetEntry("_main")

out, err := b.Emit()
os.WriteFile("program", out, 0o755)
```

`__PAGEZERO` and `__LINKEDIT` are synthesized automatically.

**Supported:**
- 64-bit Mach-O only
- Executables (`MH_EXECUTE`) and dynamic libraries (`MH_DYLIB`)
- Legacy dyld info (`LC_DYLD_INFO_ONLY`) via `DyldInfoBuilder`
- Modern chained fixups (`LC_DYLD_CHAINED_FIXUPS`, macOS 12+) via `ChainedFixupsBuilder`
- Export trie (`LC_DYLD_EXPORTS_TRIE`) via `BuildExportTrie`
- Function starts, data-in-code, code signature reservation
- Build version, source version, UUID, rpath, linker options
- Relocations for `MH_OBJECT` output

**Architectures:**

| Constant           | `cputype`         |
|--------------------|-------------------|
| `macho.ArchAMD64`  | `CPU_TYPE_X86_64` |
| `macho.ArchARM64`  | `CPU_TYPE_ARM64`  |

---

## Bootloader

The `bootloader` namespace covers binary output that targets hardware directly
rather than an operating system.

### raw

Raw output is flat machine code at a fixed origin address — no file format
header, no metadata, no wrapper of any kind. The CPU resets, jumps to a known
address, and starts executing bytes. This is always the format for stage 1
bootloaders regardless of what comes after.

```go
import "github.com/vertex-language/compiler/bin/bootloader/raw"

b := raw.NewBuilder()
b.SetOrigin(0x7C00)
b.AddSection(raw.Section{Data: machineCode, Align: 1})
b.AddSymbol(raw.Symbol{Name: "entry", Offset: 0})
b.SetBootSignature() // writes 0x55AA at bytes 510–511; implies PadSize(512)

out, err := b.Emit()
os.WriteFile("stage1.bin", out, 0o644)
```

`SetOrigin` is the physical load address. All absolute relocations are
resolved against it. `SetPadSize(n)` pads the output to exactly `n` bytes;
`Emit` returns an error if the unpadded binary exceeds that size.

### uefi

UEFI executables are PE32+ images loaded by platform firmware before any OS
is present. `uefi.NewBuilder` enforces all required UEFI constraints: EFI
subsystem, mandatory base relocations, W^X section policy, and a minimal
64-byte MZ stub.

```go
import "github.com/vertex-language/compiler/bin/bootloader/uefi"

b := uefi.NewBuilder(uefi.ArchAMD64, uefi.SubsystemEFIApplication)

b.AddSection(uefi.Section{
    Name:  ".text",
    Chars: uefi.IMAGE_SCN_CNT_CODE | uefi.IMAGE_SCN_MEM_EXECUTE | uefi.IMAGE_SCN_MEM_READ,
    Data:  machineCode,
    Relocs: []uefi.Reloc{
        {Offset: ptrOffset, Type: uefi.IMAGE_REL_BASED_DIR64},
    },
})

b.SetEntry("EfiMain")

out, err := b.Emit()
os.WriteFile("bootx64.efi", out, 0o755)
```

The `.reloc` section is always emitted — UEFI firmware requires the
base-relocation data directory entry to be present and valid even when there
are no relocations. UEFI images do not use DLL imports; all firmware services
arrive through the `EFI_SYSTEM_TABLE` pointer passed to the entry point.

| Subsystem constant                 | Use                                     |
|------------------------------------|-----------------------------------------|
| `uefi.SubsystemEFIApplication`     | Bootloaders, boot managers              |
| `uefi.SubsystemEFIBootService`     | Boot-time drivers                       |
| `uefi.SubsystemEFIRuntime`         | Drivers that survive `ExitBootServices` |

---

## Relocations

Relocations are format- and arch-specific. Each sub-package defines its own
relocation type constants. Relocations are only needed when managing
cross-section references directly — code coming through the `linker` package
arrives with relocations already applied.

```go
// ELF AMD64 — PC-relative call
elf.Reloc{Section: ".text", Offset: 12, Symbol: "puts", Type: elf.R_X86_64_PLT32, Addend: -4}

// PE AMD64 — PC-relative call
pe.COFFReloc{Offset: 8, SymbolIndex: 1, Type: pe.IMAGE_REL_AMD64_REL32}

// Mach-O ARM64 — branch
macho.Reloc{Section: "__text", Offset: 4, Symbol: "_printf",
    Type: uint8(macho.ARM64_RELOC_BRANCH26), PCRel: true, Length: 2, Extern: true}

// raw — absolute 16-bit
raw.Reloc{Section: ".text", Offset: 1, Symbol: "entry", Type: raw.R_ABS16}
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