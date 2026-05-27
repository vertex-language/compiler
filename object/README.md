# object

Package `object` is the platform-neutral interface between the cpu backend and the
format-specific object-file sub-packages (`object/elf`, `object/pe`, `object/macho`).

The backend works exclusively against the `Object` and `Section` interfaces — it never
imports a sub-package directly. The driver calls `New()` with the target and hands the
result to the backend.

## Import

```go
import "github.com/vertex-language/compiler/object"
```

## Quick start

```go
obj := object.New(object.AMD64, object.Linux)

text := obj.Text()
text.Write(machineCode)

obj.AddSymbol(object.Symbol{
    Name:       "main",
    Section:    ".text",
    Offset:     0,
    Global:     true,
    IsFunction: true,
})

obj.AddReloc(object.Reloc{
    Section: ".text",
    Offset:  uint32(callSite),
    Symbol:  "puts",
    Kind:    object.RelocCall32,
    Addend:  -4, // used only for ELF RELA; ignored on PE and Mach-O
})

data, err := obj.Emit() // ELF64 ET_REL, COFF .obj, or Mach-O MH_OBJECT
```

## Target axes

### `Arch`

| Constant | Description |
|----------|-------------|
| `AMD64`  | x86-64      |
| `ARM64`  | AArch64     |

### `Platform`

| Constant  | Object format |
|-----------|---------------|
| `Linux`   | ELF64         |
| `FreeBSD` | ELF64         |
| `Darwin`  | Mach-O        |
| `Windows` | COFF PE32+    |

### `CallingConv`

Query the calling convention for the current target via `platform.CallingConv(arch)`:

| Result           | When                    |
|------------------|-------------------------|
| `SystemV`        | Linux, Darwin, FreeBSD  |
| `MicrosoftX64`   | Windows + AMD64         |
| `WindowsARM64`   | Windows + ARM64         |

## Sections

| Accessor    | Canonical name | Type         |
|-------------|----------------|--------------|
| `Text()`    | `.text`        | `Section`    |
| `Data()`    | `.data`        | `Section`    |
| `Rodata()`  | `.rodata`      | `Section`    |
| `Bss()`     | `.bss`         | `BSSSection` |

`Section` exposes `Len()`, `Write()`, and `WriteByte()`. `BSSSection` adds
`Grow(sz uint64)`, which extends the zero-initialized reservation idempotently.
Do not call `Write` on a BSS section.

## Symbols

```go
type Symbol struct {
    Name       string
    Section    string  // canonical name; "" = undefined external
    Offset     uint64
    Global     bool
    Weak       bool
    IsFunction bool
    Abs        bool    // absolute symbol; Section and Offset are ignored
    AbsValue   uint64
}
```

Undefined externals (imports) are expressed by leaving `Section` empty. The
sub-packages synthesize any undefined symbol referenced by a relocation that was
not explicitly registered, so `AddSymbol` calls for pure imports are optional.

## Relocations

`RelocKind` is a semantic type independent of object format. Each adapter maps it
to the native constant for the target:

| Kind                | ELF                        | PE                      | Mach-O          |
|---------------------|----------------------------|-------------------------|-----------------|
| `RelocCall32`       | `PLT32` / `CALL26`         | `Rel32` / `Branch26`    | `Branch` / `Branch26`   |
| `RelocAbs64`        | `64` / `ABS64`             | `Addr64`                | `Unsigned`      |
| `RelocAbs32NB`      | —                          | `Addr32NB`              | —               |
| `RelocPCRel32`      | `PC32`                     | `Rel32`                 | `Signed`        |
| `RelocGOTLoad`      | `REX_GOTPCRELX`            | —                       | `GotLoad`       |
| `RelocTLSIE`        | `GOTTPOFF`                 | —                       | `TLV`           |
| `RelocADRP`         | `ADR_PREL_PG_HI21`         | `PagebaseRel21`         | `Page21`        |
| `RelocADRPOff12Add` | `ADD_ABS_LO12_NC`          | `Pageoffset12A`         | `Pageoff12`     |
| `RelocADRPOff12Load`| `LDST64_ABS_LO12_NC`       | `Pageoffset12L`         | `Pageoff12`     |
| `RelocSEHUnwind`    | —                          | `Addr32NB` (`.pdata`)   | —               |

`Addend` is only meaningful for ELF RELA relocations. It is silently ignored on
PE and Mach-O, both of which embed the addend in the instruction stream.

Passing an unsupported `(RelocKind, Arch)` combination for a given format panics
with a descriptive message.

## Sub-packages

The sub-packages are not intended to be imported by the backend. Use them only
when you need format-specific features not exposed through this interface (e.g.
`pe.Object.AddExportDirective`, `pe.Object.Pdata`, `macho.Object.GrowBss`).

| Package        | Format             | Consumed by               |
|----------------|--------------------|---------------------------|
| `object/elf`   | ELF64 `ET_REL`     | `linker/elf.ParseObject`  |
| `object/pe`    | COFF PE32+ `.obj`  | `linker/pe.ParseObject`   |
| `object/macho` | Mach-O `MH_OBJECT` | `linker/macho.ParseObject`|

Each sub-package's `Emit()` produces bytes that the corresponding linker package
consumes directly — no intermediate format, no translation layer.