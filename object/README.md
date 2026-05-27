# object

Platform-neutral interface between the cpu backend and the native object-file
sub-packages.

`object` is the only thing the cpu backend ever imports when it needs to emit
code. It never touches `object/elf`, `object/pe`, or `object/macho` directly.
The driver resolves the concrete format at startup and hands the backend an
`object.Object`; from that point on, all codegen is format-blind.

---

## Position in the Pipeline

```
Your Language
     │
     ▼
wasm.Module                          (frontend IR)
     │
     ▼
driver + cpu/amd64                  (code generation)
     │
     ▼
object.New(AMD64, Linux)             (← this package: format selected here)
     │
     ├── object/elf                  (ELF64 ET_REL)
     ├── object/pe                   (COFF .obj)
     └── object/macho                (Mach-O MH_OBJECT)
     │
     ▼
linker/elf  linker/pe  linker/macho  (symbol resolution, relocation patching)
     │
     ▼
executable binary
```

The cpu backend sees only the `object.Object` interface. The sub-packages are
an implementation detail of this package, not of the code generator.

---

## Packages

| Package | Wraps | Platform |
|---------|-------|----------|
| `object` | — | all (this package) |
| `object/elf` | ELF64 `ET_REL` | Linux, \*BSD |
| `object/pe` | COFF `.obj` | Windows |
| `object/macho` | Mach-O `MH_OBJECT` | macOS, iOS |

---

## Quick start

```go
// driver — the only place that knows the target platform
obj := object.New(object.AMD64, object.Linux)

// hand it to the backend — no platform knowledge required past this point
if err := amd64.Compile(fn, obj); err != nil { … }

objBytes, err := obj.Emit()

// pass straight to the linker — no file needed
parsed, err := linkerelf.ParseObject(objBytes)
lnk.AddObject(parsed)
```

Or write to disk for inspection:

```go
os.WriteFile("main.o", objBytes, 0o644)
// readelf -a main.o   objdump -d main.o   nm main.o   all work
```

---

## What the cpu backend sees

### Selecting a calling convention

```go
cc := obj.Platform().CallingConv(obj.Arch())
```

| Platform | Arch | CallingConv |
|----------|------|-------------|
| Linux / Darwin / \*BSD | AMD64 | `SystemV` |
| Linux / Darwin / \*BSD | ARM64 | `SystemV` |
| Windows | AMD64 | `MicrosoftX64` |
| Windows | ARM64 | `WindowsARM64` |

### Writing sections

```go
text   := obj.Text()    // .text  / __TEXT,__text
data   := obj.Data()    // .data  / __DATA,__data
rodata := obj.Rodata()  // .rdata / __TEXT,__const
bss    := obj.Bss()     // .bss   / __DATA,__bss
```

`Text()`, `Data()`, and `Rodata()` return a `Section`. `Bss()` returns a
`BSSSection`, which adds `Grow(sz uint64)` for reserving zero-initialised
space. All accessors are idempotent — calling `Text()` ten times returns the
same section every time.

```go
// write machine code
symOff := text.Len()
text.Write(machineCode)

// reserve BSS space — never call Write on a BSS section
bss.Grow(256)
```

### Registering symbols

`Symbol.Section` uses canonical names regardless of the output format.

```go
obj.AddSymbol(object.Symbol{
    Name:       "main",
    Section:    ".text",    // canonical — not "__text" or ".text$mn"
    Offset:     uint64(symOff),
    Global:     true,
    IsFunction: true,
})
```

### Recording relocations

```go
obj.AddReloc(object.Reloc{
    Section: ".text",
    Offset:  uint32(callSite),
    Symbol:  "runtime.malloc",
    Kind:    object.RelocCall32,
    Addend:  -4,            // ELF only; ignored on PE and Mach-O
})
```

`Addend` is carried for ELF RELA and silently dropped on PE and Mach-O, which
encode addends inside the instruction stream.

---

## RelocKind mapping

Each `RelocKind` maps to the native relocation constant for the active format
and architecture. The mapping is resolved inside the format-specific adapter;
the cpu backend only ever uses the semantic name.

| RelocKind | ELF AMD64 | ELF ARM64 | PE AMD64 | PE ARM64 | Mach-O AMD64 | Mach-O ARM64 |
|-----------|-----------|-----------|----------|----------|--------------|--------------|
| `RelocCall32` | `R_X86_64_PLT32` | `R_AARCH64_CALL26` | `Rel32` | `Branch26` | `BRANCH` | `BRANCH26` |
| `RelocAbs64` | `R_X86_64_64` | `R_AARCH64_ABS64` | `Addr64` | `Addr64` | `UNSIGNED` | `UNSIGNED` |
| `RelocAbs32NB` | — | — | `Addr32NB` | `Addr32NB` | — | — |
| `RelocPCRel32` | `R_X86_64_PC32` | — | `Rel32` | — | `SIGNED` | — |
| `RelocGOTLoad` | `R_X86_64_REX_GOTPCRELX` | — | — | — | `GOT_LOAD` | — |
| `RelocTLSIE` | `R_X86_64_GOTTPOFF` | — | — | — | `TLV` | — |
| `RelocADRP` | — | `R_AARCH64_ADR_PREL_PG_HI21` | — | `PagebaseRel21` | — | `PAGE21` |
| `RelocADRPOff12Add` | — | `R_AARCH64_ADD_ABS_LO12_NC` | — | `Pageoffset12A` | — | `PAGEOFF12` |
| `RelocADRPOff12Load` | — | `R_AARCH64_LDST64_ABS_LO12_NC` | — | `Pageoffset12L` | — | `PAGEOFF12` |
| `RelocSEHUnwind` | — | — | `Addr32NB` | `Addr32NB` | — | — |

`RelocAbs32NB` and `RelocSEHUnwind` are Windows-only. `RelocGOTLoad`,
`RelocTLSIE`, and `RelocPCRel32` are AMD64-only. The `RelocADRP*` family is
ARM64-only. Passing an unsupported combination panics immediately so errors
surface at compile time rather than producing a silent mis-link.

---

## Canonical section names

The cpu backend always uses the names in the left column. The adapters
translate them to the native spelling before writing into the sub-package.

| Canonical | ELF | COFF | Mach-O |
|-----------|-----|------|--------|
| `.text` | `.text` | `.text` | `__TEXT,__text` |
| `.data` | `.data` | `.data` | `__DATA,__data` |
| `.rodata` | `.rodata` | `.rdata` | `__TEXT,__const` |
| `.bss` | `.bss` | `.bss` | `__DATA,__bss` |

---

## Package layout

```
object/
    object.go          ← Object, Section, BSSSection interfaces; Symbol, Reloc,
    │                    RelocKind, Arch, Platform, CallingConv types; New() factory
    elf_adapter.go     ← elfObject   — wraps object/elf,   maps RelocKind → R_X86_64_* / R_AARCH64_*
    pe_adapter.go      ← peObject    — wraps object/pe,    maps RelocKind → RelAMD64* / RelARM64*
    macho_adapter.go   ← machoObject — wraps object/macho, maps RelocKind → RelocAMD64* / RelocARM64*

    elf/               ← ELF64 ET_REL implementation  (unchanged)
    pe/                ← COFF .obj   implementation   (unchanged)
    macho/             ← Mach-O MH_OBJECT implementation (unchanged)
```

The sub-packages are untouched. This package is purely an adapter layer: it
owns the neutral types, implements the `Object` interface three times, and
translates between the two vocabularies.

---

## BSS sections

BSS handling differs across formats, which is why `Bss()` returns a
`BSSSection` rather than a plain `Section`.

| Format | Underlying operation |
|--------|----------------------|
| ELF | `section.GrowNobits(sz uint64)` — method on the section |
| PE | `section.GrowBSS(sz uint32)` — method on the section |
| Mach-O | `obj.GrowBss(sectName, sz uint64)` — method on the Object |

The adapters hide this behind the uniform `Grow(sz uint64)` call. The cpu
backend calls only `Grow`; it never sees the format-specific methods.

---

## What this package is not

- **Not a code generator.** Machine code comes from `cpu/*`. This package
  only holds and serialises it.
- **Not a linker.** Symbol resolution, section merging, and relocation
  patching are `linker/*` concerns.
- **Not a file I/O layer.** `Emit()` returns a `[]byte`. Whether that slice
  is written to disk or passed directly to the linker is the driver's choice.
- **Not a replacement for the sub-packages.** Tools that need format-specific
  features (COMDAT groups, `.pdata`/`.xdata`, `.drectve` directives, DWARF
  sections, `e_flags` for RISC-V) should import the relevant sub-package
  directly. This package exposes only the intersection that every cpu backend
  needs.