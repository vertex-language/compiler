# Vertex ABI Reference

A complete reference for all import namespaces, export conventions, and callable
symbols available to a wasm frontend targeting the Vertex compiler.

---

## Import Path Grammar

The import module path is the single source of truth for how the compiler emits
a call. The first segment is the **emission prefix** — it routes the compiler to
the correct backend strategy. Everything after it identifies the specific target.

```
"linux/kernel/syscalls"   → inline syscall instruction — no PLT, no libc
"linux/<lib>"             → Linux system library — resolved via abi/linux table
"darwin/<lib>"            → macOS system library — LC_LOAD_DYLIB install name
"darwin/framework/<Name>" → macOS system framework — LC_LOAD_DYLIB framework path
"windows/<dll>"           → Windows system DLL — PE IAT entry via import lib
"lib/<name>"              → third-party library — fetched and compiled via vcpkg
"gpu/cuda"                → NVIDIA GPU kernel — PTX emission
"gpu/msl"                 → Apple Metal kernel — MSL emission (macOS only)
"gpu/vulkan"              → Vulkan compute kernel — SPIR-V emission
"memory"                  → Vertex allocator — resolved to __vertex_memory_* stubs
```

---

## Import Signature Syntax

All imports that pass pointers or handles carry a `@`-suffix on the function
name. To capture a native handle returned by the host, append `:<type>`.

```wasm
;; General syntax
(import "<path>" "<name>@<param_type>.<param_type>...:<return_type>" (func ...))
```

### Type Tokens

| Token | Meaning |
|-------|---------|
| `i32` | 32-bit integer — passed as-is |
| `i64` | 64-bit integer — passed as-is |
| `f32` | 32-bit float — passed as-is |
| `f64` | 64-bit float — passed as-is |
| `ptr` | Linear-memory i32 offset — auto-translated to native VA (`+ R15`) before call |
| `hptr` | Opaque native handle index — resolved via Handle Table before call, or registered on return |

Functions with no pointer or handle parameters need no `@` suffix.

```wasm
;; ptr param — translated to native address before call
(import "linux/kernel/syscalls" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))

;; hptr return — return value interned in Handle Table, wasm receives the index
(import "linux/libc" "fopen@ptr.ptr:hptr" (func (param i32 i32) (result i32)))

;; hptr param — resolved through Handle Table before the call
(import "linux/libc" "fwrite@ptr.i64.i64.hptr" (func (param i32 i64 i64 i32) (result i64)))

;; no pointers — no suffix needed
(import "linux/kernel/syscalls" "getpid" (func (result i32)))
```

---

## Export Suffix Syntax

Exports destined for a non-CPU backend carry a `@<kind>` suffix, with an
optional `:type.type...` list for parameter annotations across the dispatch
boundary.

```wasm
(export "<name>@<kind>" (func $name))
(export "<name>@<kind>:<type>.<type>..." (func $name))
```

| Suffix | Backend |
|--------|---------|
| `@cuda` | PTX — NVIDIA, Linux / Windows |
| `@vulkan` | SPIR-V — AMD + CPU fallback, Linux / Windows |
| `@msl` | MSL — Apple Metal, macOS only |
| `@async` | Stackful coroutines |
| `@thread` | OS threads via `clone(2)` |
| `@process` | Child processes via `fork(2)` |

---

## Import Modules

---

### `linux/kernel/syscalls` — Inlined Linux Syscalls

The entire syscall sequence is inlined at the call site. No PLT entry,
no relocation, no libc. Valid on amd64 and arm64. Any syscall in the
Linux 6.x table is addressable.

```wasm
(import "linux/kernel/syscalls" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))
(import "linux/kernel/syscalls" "read@i32.ptr.i32"  (func (param i32 i32 i32) (result i32)))
(import "linux/kernel/syscalls" "exit_group@i32"    (func (param i32)))
(import "linux/kernel/syscalls" "getpid"            (func (result i32)))
```

> `mmap`, `malloc`, and `brk` are compile-time errors — use `memory.*` instead.

---

### `linux/*` — Linux System Libraries

Linked against the system library resolved from the `abi/linux` table.
The compiler stat-walks candidate paths for the target architecture and
emits the resolved path as a `DT_NEEDED` entry in the ELF binary.

#### Supported libraries

| Import path | Soname | Notes |
|-------------|--------|-------|
| `linux/libc` | `libc.so.6` | Standard C library (glibc / musl) |
| `linux/libm` | `libm.so.6` | Math — merged into libc in glibc 2.31+, stub retained |
| `linux/libpthread` | `libpthread.so.0` | POSIX threads — merged into libc in glibc 2.34+, stub retained |
| `linux/libdl` | `libdl.so.2` | Dynamic linking — merged into libc in glibc 2.34+, stub retained |
| `linux/librt` | `librt.so.1` | POSIX realtime — merged into libc in glibc 2.34+, stub retained |
| `linux/libgcc_s` | `libgcc_s.so.1` | GCC low-level runtime: unwinding, soft-float, atomic helpers |
| `linux/libstdc++` | `libstdc++.so.6` | GNU C++ standard library — default on glibc distros |
| `linux/libc++` | `libc++.so.1` | LLVM C++ standard library — Alpine, Void Linux, clang toolchains |
| `linux/libz` | `libz.so.1` | zlib deflate / inflate |
| `linux/libresolv` | `libresolv.so.2` | DNS resolver — merged into libc in glibc 2.34+, stub retained |
| `linux/libnsl` | `libnsl.so.1` | NIS / YP network services — separated from glibc 2.32+ |
| `linux/libcrypt` | `libcrypt.so.1` | Password hashing / crypt(3) — libxcrypt on modern distros |
| `linux/libutil` | `libutil.so.1` | BSD utility: openpty, login_tty — merged into libc in glibc 2.34+ |
| `linux/libpam` | `libpam.so.0` | Pluggable Authentication Modules |
| `linux/libcap` | `libcap.so.2` | POSIX capabilities |
| `linux/libseccomp` | `libseccomp.so.2` | seccomp BPF syscall filtering |
| `linux/libselinux` | `libselinux.so.1` | SELinux access control — Fedora/RHEL default, optional elsewhere |
| `linux/libudev` | `libudev.so.1` | udev device events — systemd-based distros only |
| `linux/libsystemd` | `libsystemd.so.0` | sd-daemon, sd-journal, sd-bus — systemd-based distros only |
| `linux/libssl` | `libssl.so.3` | OpenSSL 3.x TLS — falls back to `libssl.so.1.1` |
| `linux/libcrypto` | `libcrypto.so.3` | OpenSSL 3.x crypto primitives — falls back to `libcrypto.so.1.1` |

```wasm
;; Standard C — printf with a ptr param
(import "linux/libc" "printf@ptr" (func (param i32) (result i32)))

;; File I/O using hptr to encapsulate FILE* inside the wasm sandbox
(import "linux/libc" "fopen@ptr.ptr:hptr"      (func (param i32 i32) (result i32)))
(import "linux/libc" "fwrite@ptr.i64.i64.hptr" (func (param i32 i64 i64 i32) (result i64)))
(import "linux/libc" "fclose@hptr"             (func (param i32) (result i32)))

;; Math
(import "linux/libm" "sqrt@f64" (func (param f64) (result f64)))
(import "linux/libm" "pow@f64.f64" (func (param f64 f64) (result f64)))

;; OpenSSL
(import "linux/libssl"    "SSL_CTX_new@ptr:hptr"   (func (param i32) (result i32)))
(import "linux/libcrypto" "SHA256@ptr.i32.ptr"      (func (param i32 i32 i32) (result i32)))
```

> `malloc` and `free` are compile-time errors on any `linux/*` import — use `memory.*` instead.

---

### `darwin/*` — macOS System Libraries

Emits an `LC_LOAD_DYLIB` load command with the canonical install name.
Since macOS 11 (Big Sur) these libraries live in the dyld shared cache —
the install name path will not exist on disk, but dyld resolves it at
runtime. The Xcode SDK provides `.tbd` stubs for link-time symbol resolution.

#### Supported libraries

| Import path | Install name | Notes |
|-------------|-------------|-------|
| `darwin/libSystem` | `/usr/lib/libSystem.B.dylib` | Umbrella library — libc, libm, libpthread, libdl all re-exported from here |
| `darwin/libm` | `/usr/lib/libm.dylib` | Math — symlink into libSystem |
| `darwin/libpthread` | `/usr/lib/libpthread.dylib` | POSIX threads — symlink into libSystem |
| `darwin/libdl` | `/usr/lib/libdl.dylib` | Dynamic linking — symlink into libSystem |
| `darwin/libresolv` | `/usr/lib/libresolv.9.dylib` | DNS resolver — part of libSystem |
| `darwin/libutil` | `/usr/lib/libutil.dylib` | BSD utility: openpty, login_tty |
| `darwin/libz` | `/usr/lib/libz.1.dylib` | zlib — ships with macOS since 10.0 |
| `darwin/libiconv` | `/usr/lib/libiconv.2.dylib` | Character set conversion |
| `darwin/libxml2` | `/usr/lib/libxml2.2.dylib` | XML parser — supported Apple SDK library |
| `darwin/libxslt` | `/usr/lib/libxslt.1.dylib` | XSLT processor |
| `darwin/libcurl` | `/usr/lib/libcurl.4.dylib` | URL transfer |
| `darwin/libbz2` | `/usr/lib/libbz2.1.0.dylib` | bzip2 compression |
| `darwin/libpcap` | `/usr/lib/libpcap.A.dylib` | Packet capture |
| `darwin/libsqlite3` | `/usr/lib/libsqlite3.dylib` | SQLite — supported Apple SDK library |
| `darwin/libncurses` | `/usr/lib/libncurses.5.4.dylib` | Terminal control — also addressable as `libcurses` |
| `darwin/libicucore` | `/usr/lib/libicucore.A.dylib` | ICU Unicode core — only the public Apple subset |
| `darwin/libcompression` | `/usr/lib/libcompression.dylib` | lz4, zlib, lzma, lzfse — macOS 10.11+ |
| `darwin/libarchive` | `/usr/lib/libarchive.2.dylib` | Multi-format archive — macOS 10.9+ |
| `darwin/libedit` | `/usr/lib/libedit.3.dylib` | BSD line-editing (readline-compatible) |
| `darwin/libpam` | `/usr/lib/libpam.2.dylib` | Pluggable Authentication Modules |
| `darwin/liblzma` | `/usr/lib/liblzma.5.dylib` | XZ/LZMA — macOS 10.14+, weak-linked |
| `darwin/libc++` | `/usr/lib/libc++.1.dylib` | LLVM C++ standard library — default on all Apple platforms |
| `darwin/libc++abi` | `/usr/lib/libc++abi.dylib` | C++ ABI: exceptions, RTTI |
| `darwin/libobjc` | `/usr/lib/libobjc.A.dylib` | Objective-C runtime — required for Cocoa bridging |
| `darwin/libdispatch` | `/usr/lib/system/libdispatch.dylib` | Grand Central Dispatch (GCD) |

```wasm
;; Standard C via libSystem
(import "darwin/libSystem" "printf@ptr"            (func (param i32) (result i32)))
(import "darwin/libSystem" "write@i32.ptr.i32"     (func (param i32 i32 i32) (result i32)))

;; File I/O with hptr encapsulating FILE*
(import "darwin/libSystem" "fopen@ptr.ptr:hptr"      (func (param i32 i32) (result i32)))
(import "darwin/libSystem" "fwrite@ptr.i64.i64.hptr" (func (param i32 i64 i64 i32) (result i64)))
(import "darwin/libSystem" "fclose@hptr"             (func (param i32) (result i32)))

;; SQLite
(import "darwin/libsqlite3" "sqlite3_open@ptr.ptr:hptr"    (func (param i32 i32) (result i32)))
(import "darwin/libsqlite3" "sqlite3_exec@hptr.ptr.ptr.ptr.ptr" (func (param i32 i32 i32 i32 i32) (result i32)))
(import "darwin/libsqlite3" "sqlite3_close@hptr"           (func (param i32) (result i32)))
```

> `malloc` and `free` are compile-time errors on any `darwin/*` import — use `memory.*` instead.

---

### `darwin/framework/*` — macOS System Frameworks

Emits an `LC_LOAD_DYLIB` with the framework's canonical install name
(`/System/Library/Frameworks/<Name>.framework/Versions/A/<Name>`).
All listed frameworks are in the dyld shared cache on macOS 11+.

#### Supported frameworks

| Import path | Notes |
|-------------|-------|
| `darwin/framework/CoreFoundation` | CFString, CFURL, CFArray, CFDictionary, run loops |
| `darwin/framework/CoreServices` | FSEvents, Launch Services, Search Kit |
| `darwin/framework/Security` | Keychain, SecKey, SecCertificate, CommonCrypto, TLS |
| `darwin/framework/SystemConfiguration` | Network reachability, interface info, dynamic store |
| `darwin/framework/IOKit` | Hardware device access, I/O Registry, USB, HID, power |
| `darwin/framework/DiskArbitration` | Mount / unmount callbacks, disk appearance events |
| `darwin/framework/CoreGraphics` | CGContext, CGImage, PDF rendering, display management |
| `darwin/framework/CoreText` | Font selection, glyph layout, attributed string rendering |
| `darwin/framework/CoreImage` | GPU-accelerated image filters |
| `darwin/framework/ImageIO` | Image decoding / encoding (JPEG, PNG, TIFF, HEIC) |
| `darwin/framework/CoreVideo` | CVPixelBuffer, CVDisplayLink |
| `darwin/framework/AVFoundation` | Audio / video capture, playback, editing, encoding |
| `darwin/framework/Metal` | GPU compute and rendering — amd64 and arm64 |
| `darwin/framework/MetalKit` | MTKView, MTKTextureLoader, mesh utilities |
| `darwin/framework/CoreAudio` | AudioHardware, audio device enumeration and I/O |
| `darwin/framework/AudioToolbox` | AudioQueue, AudioFile, MIDI |
| `darwin/framework/AudioUnit` | Audio plug-in hosting and AU graph |
| `darwin/framework/Network` | NWConnection, NWListener, NWPathMonitor — macOS 10.14+ |
| `darwin/framework/CFNetwork` | CFHTTPMessage, CFSocket, URL loading |
| `darwin/framework/AppKit` | NSWindow, NSView, NSApplication — macOS UI |
| `darwin/framework/Foundation` | NSString, NSArray, NSData, NSRunLoop |
| `darwin/framework/Accelerate` | BLAS, LAPACK, vDSP, vImage — vectorised maths |
| `darwin/framework/Hypervisor` | Hardware virtualisation API — macOS 10.10+ |
| `darwin/framework/vmnet` | Virtual network interfaces for VMs / containers — macOS 11+ |

```wasm
;; CoreFoundation string creation
(import "darwin/framework/CoreFoundation" "CFStringCreateWithCString@hptr.ptr.i32:hptr"
    (func (param i32 i32 i32) (result i32)))

;; Security keychain lookup
(import "darwin/framework/Security" "SecItemCopyMatching@hptr.ptr:hptr"
    (func (param i32 i32) (result i32)))

;; Metal device
(import "darwin/framework/Metal" "MTLCreateSystemDefaultDevice:hptr"
    (func (result i32)))
```

---

### `windows/*` — Windows System DLLs

Emits a PE IAT entry using the import library (`.lib`) from the Windows SDK.
The DLL name written into the binary is the canonical system DLL name resolved
at runtime from `System32`. The compiler never stat-checks DLL paths — it links
against the SDK import lib and the loader handles the rest.

When cross-compiling from Linux or macOS, the driver uses the equivalent MinGW
import archive (e.g. `libkernel32.a`).

#### Kernel and execution layer

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/ntdll` | `ntdll.lib` | `ntdll.dll` | NT native API — syscall stubs, RTL, loader. System only; prefer kernel32 |
| `windows/kernel32` | `kernel32.lib` | `KERNEL32.dll` | Core Win32: memory, files, processes, threads, synchronisation |
| `windows/kernelbase` | `kernelbase.lib` | `KERNELBASE.dll` | Win32 implementation layer since Windows 7 |
| `windows/advapi32` | `advapi32.lib` | `ADVAPI32.dll` | Registry, services, security, event log, legacy crypto |

#### C runtime

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/ucrt` | `ucrt.lib` | `ucrtbase.dll` | Universal CRT — C standard library. Inbox on Windows 10+, update for Vista–8.1 |
| `windows/vcruntime` | `vcruntime.lib` | `vcruntime140.dll` | MSVC runtime: SEH, C++ exceptions, RTTI |
| `windows/msvcrt` | `msvcrt.lib` | `msvcrt.dll` | Legacy OS CRT — system only, not a public API for new code |

#### Win32 subsystem

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/user32` | `user32.lib` | `USER32.dll` | Windows, menus, controls, messages, input |
| `windows/gdi32` | `gdi32.lib` | `GDI32.dll` | Graphics Device Interface: drawing, fonts, bitmaps |
| `windows/shell32` | `shell32.lib` | `SHELL32.dll` | Shell: known folders, ShellExecute, file operations, drag-drop |
| `windows/shlwapi` | `shlwapi.lib` | `SHLWAPI.dll` | Shell Lightweight API: path manipulation, string helpers |
| `windows/ole32` | `ole32.lib` | `ole32.dll` | COM: CoInitialize, CoCreateInstance, structured storage |
| `windows/oleaut32` | `oleaut32.lib` | `OLEAUT32.dll` | OLE Automation: BSTR, VARIANT, IDispatch, SafeArray |
| `windows/rpcrt4` | `rpcrt4.lib` | `RPCRT4.dll` | RPC runtime, UUID generation — required by COM / DCOM |
| `windows/comctl32` | `comctl32.lib` | `COMCTL32.dll` | Common Controls v6: ListView, TreeView, Toolbar |
| `windows/comdlg32` | `comdlg32.lib` | `COMDLG32.dll` | Common Dialogs: open, save, font, colour |
| `windows/pathcch` | `pathcch.lib` | `KERNELBASE.dll` | Safe path manipulation — Windows 8+ |
| `windows/imm32` | `imm32.lib` | `IMM32.dll` | Input Method Manager: CJK text input |
| `windows/version` | `version.lib` | `VERSION.dll` | File version info: GetFileVersionInfo, VerQueryValue |

#### Networking

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/ws2_32` | `ws2_32.lib` | `WS2_32.dll` | Windows Sockets 2 — required by all networking code |
| `windows/mswsock` | `mswsock.lib` | `MSWSOCK.dll` | WinSock extensions: AcceptEx, ConnectEx, TransmitFile |
| `windows/dnsapi` | `dnsapi.lib` | `DNSAPI.dll` | DNS client: DnsQuery, DnsQueryEx, DnsFree |
| `windows/iphlpapi` | `iphlpapi.lib` | `IPHLPAPI.dll` | IP Helper: adapter enumeration, routing tables, interface change notifications |
| `windows/winhttp` | `winhttp.lib` | `winhttp.dll` | WinHTTP — preferred for services and non-interactive clients |
| `windows/wininet` | `wininet.lib` | `WININET.dll` | WinINet — higher-level, handles proxy / cookies / cache |
| `windows/urlmon` | `urlmon.lib` | `urlmon.dll` | URL Moniker: URLDownloadToFile, security zone management |
| `windows/wldap32` | `wldap32.lib` | `WLDAP32.dll` | LDAP client |

#### Security and cryptography

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/bcrypt` | `bcrypt.lib` | `bcrypt.dll` | CNG: modern crypto API — BCryptHash, BCryptEncrypt, BCryptGenRandom. Vista+ |
| `windows/ncrypt` | `ncrypt.lib` | `ncrypt.dll` | CNG key storage: smart card and TPM-backed keys |
| `windows/crypt32` | `crypt32.lib` | `CRYPT32.dll` | CryptoAPI: X.509 certificates, PKCS, CMS, Authenticode |
| `windows/wintrust` | `wintrust.lib` | `WINTRUST.dll` | Authenticode verification: WinVerifyTrust |
| `windows/secur32` | `secur32.lib` | `secur32.dll` | SSPI: Kerberos, NTLM, TLS — AcquireCredentialsHandle |

#### System management and diagnostics

| Import path | Import lib | DLL | Notes |
|-------------|-----------|-----|-------|
| `windows/setupapi` | `setupapi.lib` | `SETUPAPI.dll` | Device setup and driver installation |
| `windows/cfgmgr32` | `cfgmgr32.lib` | `CFGMGR32.dll` | Plug and Play configuration manager |
| `windows/dbghelp` | `dbghelp.lib` | `dbghelp.dll` | Debug helpers: StackWalk64, MiniDumpWriteDump, symbol loading |
| `windows/psapi` | `psapi.lib` | `PSAPI.dll` | Process status: EnumProcesses, GetProcessMemoryInfo |
| `windows/pdh` | `pdh.lib` | `pdh.dll` | Performance Data Helper: CPU %, disk I/O counters |
| `windows/wevtapi` | `wevtapi.lib` | `wevtapi.dll` | Windows Event Log API (Vista+): EvtQuery, EvtSubscribe |
| `windows/userenv` | `userenv.lib` | `USERENV.dll` | User environment: LoadUserProfile, GetUserProfileDirectory |
| `windows/wtsapi32` | `wtsapi32.lib` | `wtsapi32.dll` | Terminal Services / Remote Desktop session info |
| `windows/powrprof` | `powrprof.lib` | `POWRPROF.dll` | Power management: battery info, sleep, hibernate |
| `windows/virtdisk` | `virtdisk.lib` | `virtdisk.dll` | Virtual Disk API: VHD/VHDX — Windows 7+ |
| `windows/onecore` | `OneCore.lib` | _(umbrella)_ | Win32 subset common to all Windows 10+ device families |

```wasm
;; CreateFile returning a native HANDLE as hptr
(import "windows/kernel32" "CreateFileA@ptr.i32.i32.ptr.i32.i32.ptr:hptr"
    (func (param i32 i32 i32 i32 i32 i32 i32) (result i32)))

;; WriteFile using the hptr handle
(import "windows/kernel32" "WriteFile@hptr.ptr.i32.ptr.ptr"
    (func (param i32 i32 i32 i32 i32) (result i32)))

;; CloseHandle
(import "windows/kernel32" "CloseHandle@hptr"
    (func (param i32) (result i32)))

;; Sockets
(import "windows/ws2_32" "WSAStartup@i32.ptr"     (func (param i32 i32) (result i32)))
(import "windows/ws2_32" "socket@i32.i32.i32"     (func (param i32 i32 i32) (result i32)))
(import "windows/ws2_32" "connect@i32.ptr.i32"    (func (param i32 i32 i32) (result i32)))

;; CNG random bytes
(import "windows/bcrypt" "BCryptGenRandom@ptr.ptr.i32.i32" (func (param i32 i32 i32 i32) (result i32)))

;; Process exit
(import "windows/kernel32" "ExitProcess@i32" (func (param i32)))
```

---

### `lib/<name>` — Third-Party Libraries

Third-party libraries are not assumed to be present on the system. The toolchain
fetches and compiles them via vcpkg before linking. Use the bare library name as
the final path segment.

```wasm
(import "lib/sdl2"    "SDL_Init@i32"                                         (func (param i32) (result i32)))
(import "lib/sdl2"    "SDL_CreateWindow@ptr.i32.i32.i32.i32.i32:hptr"        (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "lib/sdl2"    "SDL_DestroyWindow@hptr"                               (func (param i32)))
(import "lib/sdl2"    "SDL_Quit"                                             (func))

(import "lib/openssl" "SSL_CTX_new@ptr:hptr"                                 (func (param i32) (result i32)))
(import "lib/openssl" "SSL_CTX_free@hptr"                                    (func (param i32)))
```

---

### `gpu/*` — GPU Kernels

Used inside functions marked with a `@cuda`, `@msl`, or `@vulkan` export
suffix. A function body may only import intrinsics matching its declared
vendor — mixing vendors in a single function's call tree is a compile error.

#### `gpu/cuda` — CUDA / PTX

```wasm
(import "gpu/cuda" "threadIdx.x" (func (result i32)))
(import "gpu/cuda" "threadIdx.y" (func (result i32)))
(import "gpu/cuda" "threadIdx.z" (func (result i32)))
(import "gpu/cuda" "blockIdx.x"  (func (result i32)))
(import "gpu/cuda" "blockIdx.y"  (func (result i32)))
(import "gpu/cuda" "blockIdx.z"  (func (result i32)))
(import "gpu/cuda" "blockDim.x"  (func (result i32)))
(import "gpu/cuda" "blockDim.y"  (func (result i32)))
(import "gpu/cuda" "blockDim.z"  (func (result i32)))
(import "gpu/cuda" "syncThreads" (func))
(import "gpu/cuda" "syncWarp@i32"          (func (param i32)))
(import "gpu/cuda" "atomicAdd_f@ptr.f32"   (func (param i32 f32) (result f32)))
(import "gpu/cuda" "atomicAdd_i@ptr.i32"   (func (param i32 i32) (result i32)))
(import "gpu/cuda" "fmaf@f32.f32.f32"      (func (param f32 f32 f32) (result f32)))
(import "gpu/cuda" "rsqrtf@f32"            (func (param f32) (result f32)))
```

#### `gpu/msl` — Apple Metal (macOS only)

```wasm
(import "gpu/msl" "thread_position_in_grid.x"      (func (result i32)))
(import "gpu/msl" "thread_position_in_grid.y"      (func (result i32)))
(import "gpu/msl" "thread_position_in_grid.z"      (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.x" (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.y" (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.z" (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.x"      (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.y"      (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.z"      (func (result i32)))
(import "gpu/msl" "threadgroup_barrier"            (func))
(import "gpu/msl" "simd_sum_f@f32"                 (func (param f32) (result f32)))
(import "gpu/msl" "simd_sum_i@i32"                 (func (param i32) (result i32)))
(import "gpu/msl" "fast_fma@f32.f32.f32"           (func (param f32 f32 f32) (result f32)))
(import "gpu/msl" "fast_rsqrt@f32"                 (func (param f32) (result f32)))
```

#### `gpu/vulkan` — Vulkan / SPIR-V

```wasm
(import "gpu/vulkan" "GlobalInvocationId.x" (func (result i32)))
(import "gpu/vulkan" "GlobalInvocationId.y" (func (result i32)))
(import "gpu/vulkan" "GlobalInvocationId.z" (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.x"  (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.y"  (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.z"  (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.x"        (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.y"        (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.z"        (func (result i32)))
(import "gpu/vulkan" "barrier"              (func))
(import "gpu/vulkan" "subgroupAdd_f@f32"    (func (param f32) (result f32)))
(import "gpu/vulkan" "subgroupAdd_i@i32"    (func (param i32) (result i32)))
(import "gpu/vulkan" "fma@f32.f32.f32"      (func (param f32 f32 f32) (result f32)))
```

---

### `memory` — Vertex Allocator

Direct `malloc`, `free`, and `mmap` imports are compile-time errors on all
platforms. All allocation goes through this module. The compiler resolves
these to `__vertex_memory_*` stubs injected at compile time.

#### `memory.heap.*`

| Import | Signature | Description |
|--------|-----------|-------------|
| `memory` / `heap.alloc` | `(size i32) → i32` | Zeroed allocation |
| `memory` / `heap.alloc_raw` | `(size i32) → i32` | Uninitialised — explicit opt-in |
| `memory` / `heap.alloc_aligned` | `(size i32, align i32) → i32` | Alignment hint — v1 behaves as `heap.alloc` |
| `memory` / `heap.free` | `(ptr i32)` | Return block to free list |
| `memory` / `heap.realloc` | `(ptr i32, new_size i32) → i32` | `ptr==0` → alloc. `new_size==0` → free, return 0 |

#### `memory.ref.*`

| Import | Signature | Description |
|--------|-----------|-------------|
| `memory` / `ref.alloc` | `(size i32) → i32` | Allocate with RC header. strong=1, weak=0, dtor=0 |
| `memory` / `ref.retain` | `(ptr i32)` | Atomically increment strong count |
| `memory` / `ref.release` | `(ptr i32)` | Decrement strong count; calls destructor and frees at zero |
| `memory` / `ref.set_dtor` | `(ptr i32, fn i32)` | Store destructor function pointer into RC header |
| `memory` / `ref.weak` | `(ptr i32) → i32` | Atomically increment weak count; returns same pointer |
| `memory` / `ref.upgrade` | `(ptr i32) → i32` | Increment strong count if > 0; returns pointer or 0 if freed |

#### `memory.arena.*`

| Import | Signature | Description |
|--------|-----------|-------------|
| `memory` / `arena.push` | `()` | Save bump pointer checkpoint (max depth: 64) |
| `memory` / `arena.pop` | `()` | Restore checkpoint, reclaiming all allocations since `push` |
| `memory` / `arena.alloc` | `(size i32) → i32` | Bump-allocate, 8-byte aligned. OOM exits with code 127 |

---

## Concurrency Exports

Mark an export with `@async`, `@thread`, or `@process` to opt into a
concurrency backend.

```wasm
(export "worker@thread:ptr.i32" (func $worker))
(export "handler@async"         (func $handler))
(export "task@process:i32"      (func $task))
```

### `@async` — Coroutines

| Import | Signature | Description |
|--------|-----------|-------------|
| `coro.spawn` | `(fn i32) → i32` | Allocate handle + stack; return wasm handle |
| `coro.resume` | `(handle i32)` | Transfer control into coroutine; no-op if done |
| `coro.yield` | `(handle i32, value i32)` | Suspend, store value, return to caller |
| `coro.done` | `(handle i32) → i32` | 1 if finished, 0 if suspended |
| `coro.result` | `(handle i32) → i32` | Last yielded or final return value |

### `@thread` — OS Threads

| Import | Signature | Description |
|--------|-----------|-------------|
| `thread.spawn` | `(fn i32) → i32` | `clone(2)`, return handle |
| `thread.join` | `(handle i32) → i32` | Block until thread exits; returns exit code |
| `thread.detach` | `(handle i32)` | Mark as detached |
| `thread.self` | `() → i32` | `gettid(2)` — calling thread's TID |
| `thread.exit` | `(code i32)` | `SYS_exit` for the calling thread |

### `@process` — Child Processes

| Import | Signature | Description |
|--------|-----------|-------------|
| `process.spawn` | `(fn i32) → i32` | `fork(2)`, return handle |
| `process.wait` | `(handle i32) → i32` | `wait4(2)`; returns `WEXITSTATUS`; result cached |
| `process.pid` | `(handle i32) → i32` | Child PID |
| `process.exit` | `(code i32)` | `exit_group(2)` — valid from parent or child |

---

## GPU Kernel Exports

```wasm
(export "warpReduce@cuda"            (func $warpReduce))
(export "vectorAdd@cuda:ptr.ptr.i32" (func $vectorAdd))
(export "tileConv@msl:ptr.ptr.i32"   (func $tileConv))
(export "histogram@vulkan:ptr.i32"   (func $histogram))
```

| Vendor | Platform | Output |
|--------|----------|--------|
| `cuda` | Linux, Windows | PTX text |
| `vulkan` | Linux, Windows | SPIR-V binary |
| `msl` | macOS only | MSL text |

---

### `hw/bios/*` — Bare-Metal BIOS (Not yet implemented)

Bare-metal BIOS interrupt services and direct hardware I/O. Intended for
bootloader and firmware targets only. Not valid when the output type is a
standard OS executable.

> **Status:** Routing and ABI parsing are implemented (`MetalBIOS`). Code
> emission is not yet implemented — imports under `hw/bios/*` will produce
> a compile error in the current release.

| Sub-module | Emitted | Description |
|------------|---------|-------------|
| `hw/bios/int10h` | `int 0x10` | BIOS video services |
| `hw/bios/int13h` | `int 0x13` | BIOS disk services (CHS) |
| `hw/bios/int13h_ext` | `int 0x13` | BIOS disk services (LBA / EDD extensions) |
| `hw/bios/int15h` | `int 0x15` | BIOS system services / memory map |
| `hw/bios/int16h` | `int 0x16` | BIOS keyboard services |
| `hw/bios/int1ah` | `int 0x1a` | BIOS RTC and PIT timer |
| `hw/bios/io` | `in` / `out` | Direct hardware port I/O — no interrupt |

```wasm
;; Examples — will not compile until emission is implemented
(import "hw/bios/int10h" "set_video_mode@i32"          (func (param i32)))
(import "hw/bios/int13h" "read_sectors@i32.i32.i32.i32.i32.ptr" (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "hw/bios/int15h" "get_memory_map_e820@ptr.ptr"  (func (param i32 i32) (result i32)))
(import "hw/bios/int16h" "get_keystroke@ptr.ptr"        (func (param i32 i32)))
(import "hw/bios/io"     "out8@i32.i32"                 (func (param i32 i32)))
(import "hw/bios/io"     "in8@i32"                      (func (param i32) (result i32)))
```

---

### `hw/uefi/*` — Bare-Metal UEFI (Not yet implemented)

UEFI firmware services resolved through `EFI_SYSTEM_TABLE` pointer-chasing.
Intended for EFI application targets only. Not valid for standard OS executables.

> **Status:** Routing and ABI parsing are implemented (`MetalUEFI`). Code
> emission is not yet implemented — imports under `hw/uefi/*` will produce
> a compile error in the current release.

| Sub-module | UEFI table | Availability |
|------------|-----------|--------------|
| `hw/uefi/con_out` | `EFI_SIMPLE_TEXT_OUTPUT_PROTOCOL` | Before and after `ExitBootServices` |
| `hw/uefi/boot_services` | `EFI_BOOT_SERVICES` | Before `ExitBootServices` only |
| `hw/uefi/runtime_services` | `EFI_RUNTIME_SERVICES` | Before and after `ExitBootServices` |

```wasm
;; Examples — will not compile until emission is implemented
(import "hw/uefi/con_out"         "output_string@ptr"              (func (param i32) (result i32)))
(import "hw/uefi/con_out"         "clear_screen"                   (func (result i32)))
(import "hw/uefi/boot_services"   "allocate_pool@i32.i32.ptr"      (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services"   "exit_boot_services@hptr.i32"    (func (param i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "get_time@ptr.ptr"              (func (param i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "reset_system@i32.i32.i32.ptr"  (func (param i32 i32 i32 i32)))
```