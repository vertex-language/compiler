# linker

This directory contains the Vertex linker layer: a unified top-level facade
(`linker.go`) and three independent sub-packages for each supported binary
format. Most callers only need the facade — the sub-packages exist for advanced
use cases that require direct control over a specific format.

---

## Packages at a glance

| Package | Role |
|---------|------|
| `linker` _(this package)_ | Unified facade — compile + link in one call, automatic system library wiring |
| `linker/elf` | ELF64 linker — Linux / FreeBSD |
| `linker/pe` | PE32+ linker — Windows |
| `linker/macho` | Mach-O linker — macOS |

---

## The unified facade

### Why it exists

The three sub-linkers are deliberately low-level. They accept parsed object
files, resolved shared objects, and explicit import archives. That is the right
abstraction for a linker, but it puts unnecessary work on anyone who just wants
to compile a wasm module and get a working native binary out.

`linker.Build` closes the gap. It calls the compiler, reads the `BuildContext`
that the ABI analysis phase populates, deduplicates system library entries, and
configures whichever sub-linker matches the target platform — all without any
input from the caller beyond the module and an `Options` struct.

The frontend's contract is narrow: declare the right import module path
(`linux/libc`, `windows/kernel32`, `darwin/libsqlite3`, …) and the correct
`@`-suffix signature for each function. Everything else is automatic.

```
wasm.Module  ──►  linker.Build  ──►  []byte  (ELF64 / PE32+ / Mach-O)
                       │
                       ├── compiler.CompileFullFor    (wasm → native object + BuildContext)
                       ├── collectLinuxLibs / ...     (deduplicate SystemLibs from ctx)
                       ├── resolveELFShared / ...     (stat-walk candidate paths)
                       └── linker/{elf,pe,macho}      (link and emit)
```

### Quick start

```go
m := wasm.NewModule()

tWrite := m.Types.AddFuncType(wasm.FuncType{
    Params:  []wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
    Results: []wasm.ValType{wasm.I32},
})
tMain := m.Types.AddFuncType(wasm.FuncType{
    Results: []wasm.ValType{wasm.I32},
})

// Import write(2) directly from the kernel — no libc, no dynamic linker.
// The @-suffix declares that parameter 1 is a linear-memory pointer.
m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)

m.Functions.Add(tMain)
m.Memories.Add(wasm.MemoryType{Lim: wasm.Limits{Min: 1}})
m.Exports.Add("main", wasm.ExportFunc, 1)

msg := "Hello, world!\n"
m.Datas.Add(wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0)}, []byte(msg))

body := wasm.NewFunctionBody()
body.I32Const(1)           // fd = stdout
body.I32Const(0)           // buf = offset 0 in linear memory
body.I32Const(int32(len(msg)))
body.Call(0)               // call write
body.Drop()
body.I32Const(0)           // return 0
body.End()
m.Codes.Add(body)

bin, err := linker.Build(m, linker.Options{})
os.WriteFile("hello", bin, 0o755)
```

```bash
./hello
# Hello, world!
```

Switching from an inline syscall to a libc call is a one-line change in the
module — the linker call stays identical:

```go
// Before: inline syscall, zero dynamic dependencies, no PT_INTERP
m.Imports.AddFunc("linux/kernel/syscalls", "write@i32.ptr.i32", tWrite)

// After: linked against libc.so.6, DT_NEEDED and PT_INTERP emitted automatically
m.Imports.AddFunc("linux/libc", "write@i32.ptr.i32", tWrite)

// The linker.Build call is unchanged in both cases.
bin, err := linker.Build(m, linker.Options{})
```

### Options

```go
type Options struct {
    // Target. Defaults to the host architecture and OS.
    GoArch string // "amd64" | "arm64"   (default: runtime.GOARCH)
    GoOS   string // "linux" | "darwin" | "windows" | "freebsd"  (default: runtime.GOOS)

    // Output kind. Defaults to OutputExec.
    OutputType OutputType // OutputExec | OutputPIE | OutputShared

    // Entry-point symbol for executables.
    // Defaults: "_start" (ELF / Mach-O), "mainCRTStartup" (PE).
    // Set to "" when OutputType is OutputShared.
    Entry string

    // Compiler option: include wasm import module path in linker symbols.
    // Produces "linux/libc::printf" instead of "printf".
    QualifiedSymbols bool

    // ELF only: override the PT_INTERP dynamic linker path.
    // A sensible default is chosen when this is empty:
    //   amd64 → /lib64/ld-linux-x86-64.so.2
    //   arm64 → /lib/ld-linux-aarch64.so.1
    // PT_INTERP is only emitted when the module has at least one
    // linux/* or freebsd/* system library import.
    ELFInterp string

    // Windows only: path to the directory containing SDK import libraries
    // (.lib / MinGW .a files). Required when cross-linking against windows/*
    // imports from a non-Windows host.
    //
    // MSVC layout example:
    //   C:\Program Files (x86)\Windows Kits\10\Lib\10.0.22621.0\um\x64
    // MinGW cross layout on Linux:
    //   /usr/x86_64-w64-mingw32/lib
    SDKLibDir string

    // Darwin only: path to the directory containing Xcode SDK .tbd stubs.
    // Required when cross-linking against darwin/* imports from a non-Darwin host.
    // On a native Darwin host the stubs are found automatically.
    //
    // Example:
    //   /path/to/MacOSX.sdk/usr/lib
    DarwinSDKStubDir string
}
```

### System library resolution

`linker.Build` automatically handles system libraries declared via ABI import
paths. The behaviour varies by platform:

**Linux / FreeBSD**

The `abi/linux` table supplies a soname and a list of candidate paths for each
library. `linker.Build` stat-walks the candidates for the target architecture
and calls `AddShared` with the first parseable `.so` it finds. If none of the
candidate paths exist — for example, when cross-compiling from macOS — it falls
back to `AddNeeded(soname)`, which emits a bare `DT_NEEDED` entry and lets the
target system's dynamic linker resolve the symbol at runtime.

`PT_INTERP` is only emitted when at least one system library is present. A
module that imports only from `linux/kernel/syscalls` produces a fully static
binary with no dynamic dependencies and no interpreter entry.

**macOS**

The `abi/darwin` table supplies an install name and an SDK stub filename. Since
macOS 11 (Big Sur), system libraries live in the dyld shared cache and do not
exist as files on disk at the install name path. `linker.Build` resolves the
`.tbd` stub from `DarwinSDKStubDir` (or `/usr/lib` on a native host) and passes
it to the Mach-O linker for symbol resolution. If no stub is found, it falls
back to `AddDylibByInstallName`, which emits an `LC_LOAD_DYLIB` command that
dyld resolves at runtime.

**Windows**

The `abi/windows` table supplies an import library filename (e.g.
`kernel32.lib`). `linker.Build` looks for it in `SDKLibDir` first, then in the
MinGW sysroot (`/usr/x86_64-w64-mingw32/lib` or the ARM64 equivalent). If no
import library is found, `linker.Build` returns a descriptive error pointing at
`Options.SDKLibDir` rather than producing a binary that fails silently at load
time.

**Deduplication**

Multiple imports from the same library collapse to a single linker entry.
A module that imports `printf`, `fopen`, `fwrite`, and `fclose` from
`linux/libc` emits exactly one `DT_NEEDED libc.so.6` entry.

### Splitting compile and link

`linker.Build` compiles and links in one call. When you need to inspect or
post-process the object between those phases, use `compiler.CompileFullWith` and
`linker.LinkResult` separately:

```go
// Compile — returns the native object and the populated BuildContext.
result, err := compiler.CompileFullWith(m, compiler.Options{})

// Inspect the build context before linking.
for idx, syslib := range result.Ctx.SystemLibs {
    if syslib.Linux != nil {
        fmt.Printf("func %d links against %s\n", idx, syslib.Linux.Soname)
    }
}

// Link — picks the right sub-linker from result.Platform automatically.
bin, err := linker.LinkResult(result, linker.Options{})
```

---

## Sub-packages

The sub-packages are the right tool when you need control that the facade does
not expose: custom section layout, COMDAT rules, specific relocation handling,
additional load commands, or linker scripts. All three follow the same
conceptual pipeline and a consistent Go API, but each is self-contained with no
dependency on the others.

| Package | Target OS | Binary format | Import path |
|---------|-----------|---------------|-------------|
| [`elf`](./elf) | Linux / FreeBSD | ELF64 (`ET_EXEC` / `ET_DYN`) | `github.com/vertex-language/compiler/linker/elf` |
| [`macho`](./macho) | macOS | Mach-O (`MH_EXECUTE` / `MH_DYLIB` / `MH_BUNDLE`) | `github.com/vertex-language/compiler/linker/macho` |
| [`pe`](./pe) | Windows | PE32+ (`.exe` / `.dll`) | `github.com/vertex-language/compiler/linker/pe` |

### Supported architectures

| Architecture | `elf` | `macho` | `pe` |
|---|:---:|:---:|:---:|
| x86-64 | `EM_X86_64` | `ArchAMD64` | `MachineAMD64` |
| AArch64 | `EM_AARCH64` | `ArchARM64` | `MachineARM64` |
| RISC-V 64 | `EM_RISCV` | — | — |

### Common pipeline

Each sub-linker exposes a `NewLinker` constructor, configuration setters,
`AddObject` / `AddArchive` / `Add*` input methods, and a `Link()` call that
returns a `*LinkResult`. The result carries the full intermediate state and a
pre-configured `Builder` that emits the final byte image.

```
NewLinker → SetEntry / SetOutputType / … → AddObject / AddArchive / Add* →
Link() → result.Builder() → Emit() → []byte
```

The internal phase sequence is the same across all three:

| Phase | `elf` | `macho` | `pe` |
|-------|-------|---------|------|
| Transitive dependency walk | `DT_NEEDED` BFS | `LC_LOAD_DYLIB` BFS | import stub pre-scan |
| Symbol resolution + archive extraction | ✓ | ✓ | ✓ |
| Undefined symbol check | hard error (weak → 0) | validated at end | hard error |
| Section merging | ✓ | ✓ | ✓ (COMDAT rules) |
| VA + file-offset assignment | ✓ | ✓ | ✓ |
| Thunk / stub + GOT synthesis | — | `__stubs` + `__got` | `.text$thk` |
| Relocation patching | RELA | Mach-O | COFF |

### ELF64 — Linux / FreeBSD

```go
lnk := elf.NewLinker(elf.EM_X86_64)
lnk.SetEntry("_start")
lnk.SetInterp("/lib64/ld-linux-x86-64.so.2")
lnk.SetOutputType(elf.OutputExec) // OutputExec | OutputPIE | OutputShared

lnk.AddObject(parsedObj)
lnk.AddShared(elf.MustOpenShared("/lib/x86_64-linux-gnu/libc.so.6"))

// When cross-compiling and the .so is not present on the host, emit a bare
// DT_NEEDED entry without symbol resolution:
lnk.AddNeeded("libssl.so.3")

result, err := lnk.Link()
bin, err := result.Builder().Emit()
```

### Mach-O — macOS

```go
lnk := macho.NewLinker(macho.ArchARM64)
lnk.SetEntry("_main")
lnk.SetOutputType(macho.OutputExec) // OutputExec | OutputDylib | OutputBundle

lnk.AddObject(parsedObj)

// Preferred: parse the .tbd stub from the Xcode SDK for symbol resolution.
lnk.AddDylib(macho.MustOpenDylib("/path/to/MacOSX.sdk/usr/lib/libSystem.B.tbd"))

// Fallback when cross-compiling without an SDK: emit LC_LOAD_DYLIB by name.
// dyld resolves the install name from the shared cache at runtime.
lnk.AddDylibByInstallName("/usr/lib/libSystem.B.dylib")

result, err := lnk.Link()
bin, err := result.Builder().Emit()
```

### PE32+ — Windows

```go
lnk := pe.NewLinker(pe.MachineAMD64)
lnk.SetEntry("mainCRTStartup")
lnk.SetOutputType(pe.OutputExec) // OutputExec | OutputDLL

lnk.AddObject(parsedObj)
lnk.AddArchive(pe.MustOpenArchive("kernel32.lib"))
lnk.AddArchive(pe.MustOpenArchive("ucrt.lib"))

result, err := lnk.Link()
bin, err := result.Emit()
```

### Output types

| Goal | Package | Setter |
|------|---------|--------|
| Linux executable | `elf` | `SetOutputType(elf.OutputExec)` _(default)_ |
| Linux PIE | `elf` | `SetOutputType(elf.OutputPIE)` |
| Linux shared library | `elf` | `SetOutputType(elf.OutputShared)` |
| macOS executable | `macho` | `SetOutputType(macho.OutputExec)` _(default)_ |
| macOS dynamic library | `macho` | `SetOutputType(macho.OutputDylib)` |
| macOS loadable bundle | `macho` | `SetOutputType(macho.OutputBundle)` |
| Windows executable | `pe` | `SetOutputType(pe.OutputExec)` _(default)_ |
| Windows DLL | `pe` | `SetOutputType(pe.OutputDLL)` |

→ See [`elf/README.md`](./elf/README.md), [`macho/README.md`](./macho/README.md),
and [`pe/README.md`](./pe/README.md) for the full sub-package API references.

---

## Choosing the right API

```
Do you need a native binary from a wasm module?
│
├── Yes, and I don't need to configure the linker manually.
│       └── linker.Build(m, linker.Options{})
│
├── Yes, but I need to inspect the BuildContext between compile and link.
│       └── compiler.CompileFullWith(m, opts)  →  linker.LinkResult(result, opts)
│
└── Yes, and I need direct control over the linker (custom sections,
    additional archives, specific load commands, etc.).
            └── compiler.CompileFor / CompileWith  →  linker/{elf,pe,macho} directly
```