# Vertex ABI Reference

A complete reference for all import namespaces, export conventions, and callable
symbols available to a wasm frontend targeting the Vertex compiler.

---

## Import Path Grammar

The import path is the single source of truth for how the compiler emits a call.
The first segment is the **emission prefix** — it routes the compiler to the
correct backend strategy. Everything after it identifies the specific target.

```
"linux/*"              → system lib — linked against Linux system library
"linux/kernel/*"       → inline syscall instruction — no linker, no PLT
"windows/*"            → Windows system DLL — IAT entry
"darwin/*"             → macOS system library — LC_LOAD_DYLIB stub
"lib/*"                → third-party library — fetched/compiled via vcpkg
"hw/bios/*"            → bare metal BIOS — inline int 0xNN instruction
"hw/uefi/*"            → UEFI firmware services — EFI_SYSTEM_TABLE vtable chase
"gpu/cuda"             → NVIDIA kernel — PTX emission
"gpu/msl"              → Apple Metal kernel — MSL emission (macOS only)
"gpu/vulkan"           → Vulkan compute kernel — SPIR-V emission
```

The path is the contract. No build tag modifiers, no extern qualifiers,
no extra annotations needed.

---

## Import Signature Syntax

All imports that pass pointers or handles carry a `@`-suffix signature on the
function name. To capture a native handle returned by the host, use the
optional `:<type>` suffix.

```wasm
;; General syntax
(import "<path>" "<name>@<param_type>.<param_type>...:<return_type>" (func ...))
```

### Type Tokens

| Token | Meaning |
| --- | --- |
| `i32` | 32-bit integer — passed as-is |
| `i64` | 64-bit integer — passed as-is |
| `f32` | 32-bit float — passed as-is |
| `f64` | 64-bit float — passed as-is |
| `ptr` | Linear-memory i32 offset — auto-translated to native VA (`+ r15`) before call |
| `hptr` | Opaque native handle index — auto-resolved via Handle Table before call, or registered on return |

Functions with no pointer or handle parameters/returns need no `@` suffix.

```wasm
;; fd=i32, buf=ptr, count=i32 — Linux inline syscall
(import "linux/kernel/syscalls" "write@i32.ptr.i32" (func (param i32 i32 i32) (result i32)))

;; fopen returns a native FILE* — intercepted and returned to wasm as hptr
(import "linux/libc" "fopen@ptr.ptr:hptr" (func (param i32 i32) (result i32)))

;; fwrite receives the hptr — resolved to real FILE* before the call
(import "linux/libc" "fwrite@ptr.i64.i64.hptr" (func (param i32 i64 i64 i32) (result i64)))

;; no pointer params — no suffix needed
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

| Kind | Backend |
| --- | --- |
| `@cuda` | PTX — NVIDIA, Linux/Windows |
| `@vulkan` | SPIR-V — AMD + CPU fallback, Linux/Windows |
| `@msl` | MSL — Apple Metal, macOS only |
| `@async` | Stackful coroutines |
| `@thread` | OS threads via `clone(2)` |
| `@process` | Child processes via `fork(2)` |

---

## Import Modules

---

### `linux/kernel/syscalls` — Inlined Linux Syscalls

The entire syscall sequence is inlined at the call site. No PLT entry,
no relocation, no libc. `ptr` params have `R15` added before the syscall
instruction is emitted.

```wasm
(import "linux/kernel/syscalls" "write@i32.ptr.i32"   (func (param i32 i32 i32) (result i32)))
(import "linux/kernel/syscalls" "read@i32.ptr.i32"    (func (param i32 i32 i32) (result i32)))
(import "linux/kernel/syscalls" "open@ptr.i32.i32"    (func (param i32 i32 i32) (result i32)))
(import "linux/kernel/syscalls" "close@i32"           (func (param i32) (result i32)))
(import "linux/kernel/syscalls" "exit_group@i32"      (func (param i32)))
```

Any syscall from the Linux 6.x amd64/arm64 table is valid.

> `mmap` and `malloc` are compile-time errors — use `memory.*` instead.

---

### `linux/libc` — Linux C Standard Library

Linked against the system libc (glibc / musl). File I/O uses `hptr` to
safely encapsulate 64-bit `FILE*` native pointers inside the 32-bit wasm
sandbox.

```wasm
(import "linux/libc" "fopen@ptr.ptr:hptr"          (func (param i32 i32) (result i32)))
(import "linux/libc" "fwrite@ptr.i64.i64.hptr"     (func (param i32 i64 i64 i32) (result i64)))
(import "linux/libc" "fread@ptr.i64.i64.hptr"      (func (param i32 i64 i64 i32) (result i64)))
(import "linux/libc" "fclose@hptr"                 (func (param i32) (result i32)))
(import "linux/libc" "printf@ptr"                  (func (param i32) (result i32)))
(import "linux/libc" "strlen@ptr"                  (func (param i32) (result i32)))
(import "linux/libc" "memcpy@ptr.ptr.i32"          (func (param i32 i32 i32) (result i32)))
(import "linux/libc" "memset@ptr.i32.i32"          (func (param i32 i32 i32) (result i32)))
```

> `malloc` and `free` are compile-time errors — use `memory.*` instead.

---

### `windows/kernel32` — Windows System DLL

Emits an IAT entry. Same `@`-suffix signature convention.

```wasm
(import "windows/kernel32" "WriteFile@hptr.ptr.i32.ptr.ptr"  (func (param i32 i32 i32 i32 i32) (result i32)))
(import "windows/kernel32" "ReadFile@hptr.ptr.i32.ptr.ptr"   (func (param i32 i32 i32 i32 i32) (result i32)))
(import "windows/kernel32" "CreateFileA@ptr.i32.i32.ptr.i32.i32.ptr:hptr" (func (param i32 i32 i32 i32 i32 i32 i32) (result i32)))
(import "windows/kernel32" "CloseHandle@hptr"                (func (param i32) (result i32)))
(import "windows/kernel32" "ExitProcess@i32"                 (func (param i32)))
```

---

### `darwin/libSystem` — macOS System Library

Emits an `LC_LOAD_DYLIB` stub.

```wasm
(import "darwin/libSystem" "write@i32.ptr.i32"          (func (param i32 i32 i32) (result i32)))
(import "darwin/libSystem" "read@i32.ptr.i32"           (func (param i32 i32 i32) (result i32)))
(import "darwin/libSystem" "open@ptr.i32.i32"           (func (param i32 i32 i32) (result i32)))
(import "darwin/libSystem" "close@i32"                  (func (param i32) (result i32)))
(import "darwin/libSystem" "fopen@ptr.ptr:hptr"         (func (param i32 i32) (result i32)))
(import "darwin/libSystem" "fwrite@ptr.i64.i64.hptr"    (func (param i32 i64 i64 i32) (result i64)))
(import "darwin/libSystem" "fread@ptr.i64.i64.hptr"     (func (param i32 i64 i64 i32) (result i64)))
(import "darwin/libSystem" "fclose@hptr"                (func (param i32) (result i32)))
(import "darwin/libSystem" "printf@ptr"                 (func (param i32) (result i32)))
```

> `malloc` and `free` are compile-time errors — use `memory.*` instead.

---

### `lib/<name>` — Third-Party Libraries

Use the bare library name as the final path segment. Libraries under `lib/`
are not assumed to be present on the system — the toolchain fetches and
compiles them via vcpkg into a consistent location before linking.

```wasm
(import "lib/sdl2" "SDL_Init@i32"                                          (func (param i32) (result i32)))
(import "lib/sdl2" "SDL_CreateWindow@ptr.i32.i32.i32.i32.i32:hptr"        (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "lib/sdl2" "SDL_DestroyWindow@hptr"                                (func (param i32)))
(import "lib/sdl2" "SDL_Quit"                                              (func))

(import "lib/openssl" "SSL_CTX_new@ptr:hptr"                               (func (param i32) (result i32)))
(import "lib/openssl" "SSL_CTX_free@hptr"                                  (func (param i32)))
```

---

### `hw/bios/*` — Bare Metal BIOS Services

Bare metal imports target real-mode BIOS interrupt services and direct
hardware I/O. The compiler inlines the appropriate `int 0xNN` instruction,
pre-loading registers from the wasm operand stack according to the BIOS ABI
for each function.

`ptr` params are translated using `R15` — the wasm linear memory region is
placed at a known physical address by the stage 1 stub before any wasm code
runs. NULL (offset 0) is a valid buffer location; no NULL guard is applied.

**Sub-modules and emitted instructions:**

| Module | Emitted | Source |
| --- | --- | --- |
| `hw/bios/int10h` | `int 0x10` | BIOS video services |
| `hw/bios/int13h` | `int 0x13` | BIOS disk services (CHS) |
| `hw/bios/int13h_ext` | `int 0x13` | BIOS disk services (LBA / EDD extensions) |
| `hw/bios/int15h` | `int 0x15` | BIOS system services / memory map |
| `hw/bios/int16h` | `int 0x16` | BIOS keyboard services |
| `hw/bios/int1ah` | `int 0x1a` | BIOS RTC and PIT tick services |
| `hw/bios/io` | `in` / `out` | Direct hardware port I/O — no interrupt |

#### `hw/bios/int10h` — Video Services

```wasm
;; Source: IBM PS/2 and PC BIOS Interface Technical Reference, April 1987
;;         https://grandidierite.github.io/bios-interrupts
(import "hw/bios/int10h" "set_video_mode@i32"                                    (func (param i32)))
(import "hw/bios/int10h" "set_cursor_size@i32.i32"                               (func (param i32 i32)))
(import "hw/bios/int10h" "set_cursor_pos@i32.i32.i32"                            (func (param i32 i32 i32)))
(import "hw/bios/int10h" "get_cursor_pos@i32.ptr.ptr.ptr.ptr"                    (func (param i32 i32 i32 i32 i32)))
(import "hw/bios/int10h" "set_active_page@i32"                                   (func (param i32)))
(import "hw/bios/int10h" "scroll_up@i32.i32.i32.i32.i32.i32"                    (func (param i32 i32 i32 i32 i32 i32)))
(import "hw/bios/int10h" "scroll_down@i32.i32.i32.i32.i32.i32"                  (func (param i32 i32 i32 i32 i32 i32)))
(import "hw/bios/int10h" "read_char_attr@i32.ptr.ptr"                            (func (param i32 i32 i32)))
(import "hw/bios/int10h" "write_char_attr@i32.i32.i32.i32"                       (func (param i32 i32 i32 i32)))
(import "hw/bios/int10h" "write_char@i32.i32.i32"                                (func (param i32 i32 i32)))
(import "hw/bios/int10h" "set_palette@i32.i32"                                   (func (param i32 i32)))
(import "hw/bios/int10h" "write_pixel@i32.i32.i32.i32"                           (func (param i32 i32 i32 i32)))
(import "hw/bios/int10h" "read_pixel@i32.i32.i32"                                (func (param i32 i32 i32) (result i32)))
(import "hw/bios/int10h" "write_tty@i32.i32.i32"                                 (func (param i32 i32 i32)))
(import "hw/bios/int10h" "get_video_mode@ptr.ptr.ptr"                            (func (param i32 i32 i32)))
(import "hw/bios/int10h" "write_string@i32.i32.i32.i32.i32.ptr.i32"             (func (param i32 i32 i32 i32 i32 i32 i32)))
```

#### `hw/bios/int13h` — Disk Services (CHS)

```wasm
;; Source: IBM PS/2 and PC BIOS Interface Technical Reference, April 1987
;;         https://en.wikipedia.org/wiki/INT_13H
;; DL = drive: 0x00–0x7F floppy, 0x80+ hard disk
(import "hw/bios/int13h" "reset_disk@i32"                          (func (param i32) (result i32)))
(import "hw/bios/int13h" "get_disk_status@i32"                     (func (param i32) (result i32)))
(import "hw/bios/int13h" "read_sectors@i32.i32.i32.i32.i32.ptr"   (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "hw/bios/int13h" "write_sectors@i32.i32.i32.i32.i32.ptr"  (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "hw/bios/int13h" "verify_sectors@i32.i32.i32.i32.i32"     (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/bios/int13h" "get_disk_params@i32.ptr.ptr.ptr"        (func (param i32 i32 i32 i32) (result i32)))
(import "hw/bios/int13h" "get_disk_type@i32.ptr.ptr"              (func (param i32 i32 i32) (result i32)))
```

#### `hw/bios/int13h_ext` — Disk Services (LBA / EDD)

```wasm
;; Source: Enhanced Disk Drive Specification (EDD) 3.0
;;         https://en.wikipedia.org/wiki/INT_13H — Extensions section
;; Requires INT 13h extensions present — check_extensions must succeed first.
;; packet = pointer to a Disk Address Packet (DAP) struct in linear memory.
(import "hw/bios/int13h_ext" "check_extensions@i32"          (func (param i32) (result i32)))
(import "hw/bios/int13h_ext" "read_sectors_lba@i32.ptr"      (func (param i32 i32) (result i32)))
(import "hw/bios/int13h_ext" "write_sectors_lba@i32.ptr"     (func (param i32 i32) (result i32)))
(import "hw/bios/int13h_ext" "get_drive_params_lba@i32.ptr"  (func (param i32 i32) (result i32)))
```

#### `hw/bios/int15h` — System Services / Memory Map

```wasm
;; Source: IBM PS/2 and PC BIOS Interface Technical Reference, April 1987
;;         ACPI Specification — INT 15h E820h memory map
(import "hw/bios/int15h" "wait@i32"                        (func (param i32) (result i32)))
(import "hw/bios/int15h" "get_memory_map_e820@ptr.ptr"     (func (param i32 i32) (result i32)))
;; Pass continuation=0 on first call. Updated in-place each iteration.
;; CF set (non-zero result) signals the last entry.
(import "hw/bios/int15h" "get_extended_memory_size@ptr"    (func (param i32) (result i32)))
(import "hw/bios/int15h" "get_memory_above_1mb@ptr"        (func (param i32) (result i32)))
(import "hw/bios/int15h" "cpu_suspend@i32"                 (func (param i32) (result i32)))
```

#### `hw/bios/int16h` — Keyboard Services

```wasm
;; Source: IBM PS/2 and PC BIOS Interface Technical Reference, April 1987
;;         https://en.wikipedia.org/wiki/INT_16H
(import "hw/bios/int16h" "get_keystroke@ptr.ptr"       (func (param i32 i32)))
(import "hw/bios/int16h" "check_keystroke@ptr.ptr"     (func (param i32 i32) (result i32)))
(import "hw/bios/int16h" "get_shift_status"            (func (result i32)))
(import "hw/bios/int16h" "get_keystroke_ext@ptr.ptr"   (func (param i32 i32)))
(import "hw/bios/int16h" "check_keystroke_ext@ptr.ptr" (func (param i32 i32) (result i32)))
```

#### `hw/bios/int1ah` — RTC and PIT Timer

```wasm
;; Source: IBM PS/2 and PC BIOS Interface Technical Reference, April 1987
(import "hw/bios/int1ah" "get_tick_count@ptr.ptr"      (func (param i32 i32)))
(import "hw/bios/int1ah" "set_tick_count@i32"          (func (param i32)))
(import "hw/bios/int1ah" "get_rtc_time@ptr.ptr.ptr"    (func (param i32 i32 i32) (result i32)))
(import "hw/bios/int1ah" "set_rtc_time@i32.i32.i32"   (func (param i32 i32 i32) (result i32)))
(import "hw/bios/int1ah" "get_rtc_date@ptr.ptr.ptr"    (func (param i32 i32 i32) (result i32)))
(import "hw/bios/int1ah" "set_rtc_date@i32.i32.i32"   (func (param i32 i32 i32) (result i32)))
```

#### `hw/bios/io` — Direct Port I/O

No interrupt emitted. The compiler emits `in`/`out` port instructions
directly. `io_wait` emits `out 0x80, 0` — a ~1 µs delay required when
writing to slow ISA-bus devices.

```wasm
;; Source: Intel 64 and IA-32 Architectures Software Developer's Manual Vol. 1
(import "hw/bios/io" "out8@i32.i32"   (func (param i32 i32)))
(import "hw/bios/io" "out16@i32.i32"  (func (param i32 i32)))
(import "hw/bios/io" "out32@i32.i32"  (func (param i32 i32)))
(import "hw/bios/io" "in8@i32"        (func (param i32) (result i32)))
(import "hw/bios/io" "in16@i32"       (func (param i32) (result i32)))
(import "hw/bios/io" "in32@i32"       (func (param i32) (result i32)))
(import "hw/bios/io" "io_wait"        (func))
```

#### Unambiguous BIOS misc

```wasm
;; INT 11h — equipment flags bitmask (floppy count, video adapter, etc.)
;; INT 12h — conventional memory size below 640K in KB
(import "hw/bios" "get_equipment_flags"  (func (result i32)))
(import "hw/bios" "get_base_memory_kb"   (func (result i32)))
```

---

### `hw/uefi/*` — UEFI Firmware Services

UEFI imports route through `EFI_SYSTEM_TABLE` pointer-chasing rather than a
linker or PLT. The compiler resolves each function name to its vtable slot
offset in the appropriate UEFI protocol table, emitting a pointer-chase
sequence at the call site.

`ptr` translation works identically to other targets — the wasm linear memory
region is allocated via `hw/uefi/boot_services allocate_pool` by the runtime
stub before `EfiMain` is entered, and `R15` holds its base address throughout
execution. `hptr` encapsulates `EFI_HANDLE`, `EFI_EVENT`, and other opaque
firmware pointer types.

If two sub-modules expose a function with the same name, the full path is
required. The compiler errors on ambiguity rather than guessing.

**Sub-modules and their source tables:**

| Module | UEFI Table | Availability |
| --- | --- | --- |
| `hw/uefi/con_out` | `EFI_SIMPLE_TEXT_OUTPUT_PROTOCOL` | Before and after `ExitBootServices` |
| `hw/uefi/boot_services` | `EFI_BOOT_SERVICES` | Before `ExitBootServices` only |
| `hw/uefi/runtime_services` | `EFI_RUNTIME_SERVICES` | Before and after `ExitBootServices` |

> Calling a `hw/uefi/boot_services` function after `ExitBootServices` is
> undefined behavior. The compiler does not enforce this at compile time.

#### `hw/uefi/con_out` — Text Output

```wasm
;; EFI_SIMPLE_TEXT_OUTPUT_PROTOCOL
;; Source: UEFI Specification 2.11 §12.4
(import "hw/uefi/con_out" "reset@i32"                   (func (param i32) (result i32)))
(import "hw/uefi/con_out" "output_string@ptr"           (func (param i32) (result i32)))
(import "hw/uefi/con_out" "test_string@ptr"             (func (param i32) (result i32)))
(import "hw/uefi/con_out" "query_mode@i32.ptr.ptr"      (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/con_out" "set_mode@i32"                (func (param i32) (result i32)))
(import "hw/uefi/con_out" "set_attribute@i32"           (func (param i32) (result i32)))
(import "hw/uefi/con_out" "clear_screen"                (func (result i32)))
(import "hw/uefi/con_out" "set_cursor_position@i32.i32" (func (param i32 i32) (result i32)))
(import "hw/uefi/con_out" "enable_cursor@i32"           (func (param i32) (result i32)))
```

#### `hw/uefi/boot_services` — Boot Services

```wasm
;; EFI_BOOT_SERVICES
;; Source: UEFI Specification 2.11 §7, gnu-efi/inc/efiapi.h

;; Task Priority
(import "hw/uefi/boot_services" "raise_tpl@i32"                                      (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "restore_tpl@i32"                                    (func (param i32)))

;; Memory Allocation
(import "hw/uefi/boot_services" "allocate_pages@i32.i32.i32.ptr"                     (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "free_pages@i64.i32"                                 (func (param i64 i32) (result i32)))
(import "hw/uefi/boot_services" "get_memory_map@ptr.ptr.ptr.ptr.ptr"                 (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "allocate_pool@i32.i32.ptr"                          (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "free_pool@ptr"                                      (func (param i32) (result i32)))

;; Events & Timers
(import "hw/uefi/boot_services" "create_event@i32.i32.ptr.ptr.ptr"                   (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "set_timer@hptr.i32.i64"                             (func (param i32 i32 i64) (result i32)))
(import "hw/uefi/boot_services" "wait_for_event@i32.ptr.ptr"                         (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "signal_event@hptr"                                  (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "close_event@hptr"                                   (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "check_event@hptr"                                   (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "create_event_ex@i32.i32.ptr.ptr.ptr.ptr"            (func (param i32 i32 i32 i32 i32 i32) (result i32)))

;; Protocol Handler
(import "hw/uefi/boot_services" "install_protocol_interface@ptr.ptr.i32.ptr"         (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "reinstall_protocol_interface@hptr.ptr.ptr.ptr"      (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "uninstall_protocol_interface@hptr.ptr.ptr"          (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "handle_protocol@hptr.ptr.ptr"                       (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "register_protocol_notify@ptr.hptr.ptr"              (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "locate_handle@i32.ptr.ptr.ptr.ptr"                  (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "locate_device_path@ptr.ptr.ptr"                     (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "install_configuration_table@ptr.ptr"                (func (param i32 i32) (result i32)))
(import "hw/uefi/boot_services" "open_protocol@hptr.ptr.ptr.hptr.hptr.i32"           (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "close_protocol@hptr.ptr.hptr.hptr"                  (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "open_protocol_information@hptr.ptr.ptr.ptr"         (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "protocols_per_handle@hptr.ptr.ptr"                  (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "locate_handle_buffer@i32.ptr.ptr.ptr.ptr"           (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "locate_protocol@ptr.ptr.ptr"                        (func (param i32 i32 i32) (result i32)))

;; Image
(import "hw/uefi/boot_services" "load_image@i32.hptr.ptr.ptr.i32.ptr"               (func (param i32 i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "start_image@hptr.ptr.ptr"                           (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "exit@hptr.i32.i32.ptr"                              (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "unload_image@hptr"                                  (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "exit_boot_services@hptr.i32"                        (func (param i32 i32) (result i32)))

;; Driver Support
(import "hw/uefi/boot_services" "connect_controller@hptr.ptr.ptr.i32"                (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "disconnect_controller@hptr.hptr.hptr"               (func (param i32 i32 i32) (result i32)))

;; Misc
(import "hw/uefi/boot_services" "get_next_monotonic_count@ptr"                       (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "stall@i32"                                          (func (param i32) (result i32)))
(import "hw/uefi/boot_services" "set_watchdog_timer@i32.i64.i32.ptr"                 (func (param i32 i64 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "calculate_crc32@ptr.i32.ptr"                        (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/boot_services" "copy_mem@ptr.ptr.i32"                               (func (param i32 i32 i32)))
(import "hw/uefi/boot_services" "set_mem@ptr.i32.i32"                                (func (param i32 i32 i32)))
```

#### `hw/uefi/runtime_services` — Runtime Services

```wasm
;; EFI_RUNTIME_SERVICES
;; Source: UEFI Specification 2.11 §8

;; Time
(import "hw/uefi/runtime_services" "get_time@ptr.ptr"                   (func (param i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "set_time@ptr"                       (func (param i32) (result i32)))
(import "hw/uefi/runtime_services" "get_wakeup_time@ptr.ptr.ptr"        (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "set_wakeup_time@i32.ptr"            (func (param i32 i32) (result i32)))

;; Virtual Memory
(import "hw/uefi/runtime_services" "set_virtual_address_map@i32.i32.i32.ptr" (func (param i32 i32 i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "convert_pointer@i32.ptr"            (func (param i32 i32) (result i32)))

;; NVRAM Variables
(import "hw/uefi/runtime_services" "get_variable@ptr.ptr.ptr.ptr.ptr"   (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "get_next_variable_name@ptr.ptr.ptr" (func (param i32 i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "set_variable@ptr.ptr.i32.i32.ptr"   (func (param i32 i32 i32 i32 i32) (result i32)))
(import "hw/uefi/runtime_services" "query_variable_info@i32.ptr.ptr.ptr" (func (param i32 i32 i32 i32) (result i32)))

;; Misc
(import "hw/uefi/runtime_services" "get_next_high_monotonic_count@ptr"  (func (param i32) (result i32)))
(import "hw/uefi/runtime_services" "reset_system@i32.i32.i32.ptr"       (func (param i32 i32 i32 i32)))
(import "hw/uefi/runtime_services" "update_capsule@ptr.i32.i64"         (func (param i32 i32 i64) (result i32)))
(import "hw/uefi/runtime_services" "query_capsule_capabilities@ptr.i32.ptr.ptr" (func (param i32 i32 i32 i32) (result i32)))
```

---

### `gpu/*` — GPU Kernels

Imported from the `gpu` sub-module matching the target vendor. Used inside
functions marked with a `@cuda`, `@msl`, or `@vulkan` export suffix. A
function body may only import intrinsics matching its declared vendor —
mixing vendors across a single function's call tree is a compile error.

#### `gpu/cuda` — CUDA / PTX

All CUDA intrinsics are imported under the `gpu/cuda` path. Symbols map to
PTX instructions and may expand across compiler versions.

```wasm
(import "gpu/cuda" "threadIdx.x"  (func (result i32)))
(import "gpu/cuda" "threadIdx.y"  (func (result i32)))
(import "gpu/cuda" "threadIdx.z"  (func (result i32)))
(import "gpu/cuda" "blockIdx.x"   (func (result i32)))
(import "gpu/cuda" "blockIdx.y"   (func (result i32)))
(import "gpu/cuda" "blockIdx.z"   (func (result i32)))
(import "gpu/cuda" "blockDim.x"   (func (result i32)))
(import "gpu/cuda" "blockDim.y"   (func (result i32)))
(import "gpu/cuda" "blockDim.z"   (func (result i32)))
(import "gpu/cuda" "gridDim.x"    (func (result i32)))
(import "gpu/cuda" "gridDim.y"    (func (result i32)))
(import "gpu/cuda" "gridDim.z"    (func (result i32)))
(import "gpu/cuda" "syncThreads"  (func))
(import "gpu/cuda" "syncWarp@i32" (func (param i32)))
(import "gpu/cuda" "atomicAdd_f@ptr.f32"  (func (param i32 f32) (result f32)))
(import "gpu/cuda" "atomicAdd_i@ptr.i32"  (func (param i32 i32) (result i32)))
(import "gpu/cuda" "fmaf@f32.f32.f32"     (func (param f32 f32 f32) (result f32)))
(import "gpu/cuda" "rsqrtf@f32"           (func (param f32) (result f32)))
```

#### `gpu/msl` — Apple Metal (macOS only)

All Metal intrinsics are imported under the `gpu/msl` path. Symbols map to
MSL built-ins and may expand across compiler versions.

```wasm
(import "gpu/msl" "thread_position_in_grid.x"    (func (result i32)))
(import "gpu/msl" "thread_position_in_grid.y"    (func (result i32)))
(import "gpu/msl" "thread_position_in_grid.z"    (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.x" (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.y" (func (result i32)))
(import "gpu/msl" "threadgroup_position_in_grid.z" (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.x"    (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.y"    (func (result i32)))
(import "gpu/msl" "threads_per_threadgroup.z"    (func (result i32)))
(import "gpu/msl" "threadgroup_barrier"          (func))
(import "gpu/msl" "simd_sum_f@f32"               (func (param f32) (result f32)))
(import "gpu/msl" "simd_sum_i@i32"               (func (param i32) (result i32)))
(import "gpu/msl" "fast_fma@f32.f32.f32"         (func (param f32 f32 f32) (result f32)))
(import "gpu/msl" "fast_rsqrt@f32"               (func (param f32) (result f32)))
```

#### `gpu/vulkan` — Vulkan / SPIR-V

All Vulkan intrinsics are imported under the `gpu/vulkan` path. Symbols map
to SPIR-V opcodes and may expand across compiler versions.

```wasm
(import "gpu/vulkan" "GlobalInvocationId.x"   (func (result i32)))
(import "gpu/vulkan" "GlobalInvocationId.y"   (func (result i32)))
(import "gpu/vulkan" "GlobalInvocationId.z"   (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.x"    (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.y"    (func (result i32)))
(import "gpu/vulkan" "LocalInvocationId.z"    (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.x"          (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.y"          (func (result i32)))
(import "gpu/vulkan" "WorkgroupId.z"          (func (result i32)))
(import "gpu/vulkan" "WorkgroupSize.x"        (func (result i32)))
(import "gpu/vulkan" "WorkgroupSize.y"        (func (result i32)))
(import "gpu/vulkan" "WorkgroupSize.z"        (func (result i32)))
(import "gpu/vulkan" "barrier"                (func))
(import "gpu/vulkan" "subgroupAdd_f@f32"      (func (param f32) (result f32)))
(import "gpu/vulkan" "subgroupAdd_i@i32"      (func (param i32) (result i32)))
(import "gpu/vulkan" "fma@f32.f32.f32"        (func (param f32 f32 f32) (result f32)))
```

> Consult the Vertex intrinsic reference for the current symbol listing under
> each vendor path. The symbols above reflect the stable set at this revision.

---

### `memory` — Vertex Allocator

Direct `malloc`, `free`, and `mmap` imports are compile-time errors. All
allocation goes through this module.

#### `memory.heap.*`

| Import | Signature | Description |
| --- | --- | --- |
| `heap.alloc` | `(size i32) → i32` | Zeroed allocation |
| `heap.alloc_raw` | `(size i32) → i32` | Uninitialized — explicit opt-in |
| `heap.alloc_aligned` | `(size i32, align i32) → i32` | v1: alignment ignored; behaves as `heap.alloc` |
| `heap.free` | `(ptr i32)` | Return block to free list. Large blocks not reclaimed in v1. |
| `heap.realloc` | `(ptr i32, new_size i32) → i32` | `ptr==0` → alloc. `new_size==0` → free, return 0. |

#### `memory.ref.*`

| Import | Signature | Description |
| --- | --- | --- |
| `ref.alloc` | `(size i32) → i32` | Allocate with RC header. strong=1, weak=0, dtor=0. |
| `ref.retain` | `(ptr i32)` | Atomically increment strong count |
| `ref.release` | `(ptr i32)` | Decrement strong count; calls destructor and frees at zero |
| `ref.set_dtor` | `(ptr i32, fn i32)` | Store destructor function pointer into RC header |
| `ref.alloc_weak` | `(size i32) → i32` | v1: identical to `ref.alloc` |
| `ref.weak` | `(ptr i32) → i32` | Atomically increment weak count; returns same pointer |
| `ref.upgrade` | `(ptr i32) → i32` | Increment strong count if > 0; returns pointer or 0 if freed |

#### `memory.arena.*`

| Import | Signature | Description |
| --- | --- | --- |
| `arena.push` | `()` | Save bump pointer checkpoint (max depth: 64) |
| `arena.pop` | `()` | Restore checkpoint, reclaiming all allocations since `push` |
| `arena.alloc` | `(size i32) → i32` | Bump-allocate, 8-byte aligned. OOM exits with code 127. |

---

## Concurrency Exports

Mark an export with `@async`, `@thread`, or `@process` to opt into a
concurrency backend. An optional `:type...` list describes parameter passing
across the spawn boundary.

```wasm
(export "worker@thread:ptr.i32"  (func $worker))
(export "handler@async"          (func $handler))
(export "task@process:i32"       (func $task))
```

Once spawned, each model exposes callable wasm imports from the `"concurrency"`
module.

### `@async` — Coroutines

| Import | Signature | Description |
| --- | --- | --- |
| `coro.spawn` | `(fn i32) → i32` | Allocate handle + stack; return wasm handle |
| `coro.resume` | `(handle i32)` | Transfer control into coroutine; no-op if done |
| `coro.yield` | `(handle i32, value i32)` | Suspend, store value, return to caller |
| `coro.done` | `(handle i32) → i32` | 1 if finished, 0 if suspended |
| `coro.result` | `(handle i32) → i32` | Last yielded value or final return value |

Handle lifetime is managed by `memory.ref.*`. `ref.release` unmaps the
coroutine stack.

### `@thread` — OS Threads

| Import | Signature | Description |
| --- | --- | --- |
| `thread.spawn` | `(fn i32) → i32` | `clone(2)`, return handle |
| `thread.join` | `(handle i32) → i32` | Block until thread exits; returns exit code |
| `thread.detach` | `(handle i32)` | Mark as detached |
| `thread.self` | `() → i32` | `gettid(2)` — calling thread's TID |
| `thread.exit` | `(code i32)` | `SYS_exit` for the calling thread |

Handle lifetime is managed by `memory.ref.*`.

### `@process` — Child Processes

| Import | Signature | Description |
| --- | --- | --- |
| `process.spawn` | `(fn i32) → i32` | `fork(2)`, return handle |
| `process.wait` | `(handle i32) → i32` | `wait4(2)`; returns `WEXITSTATUS`; result cached |
| `process.pid` | `(handle i32) → i32` | Child PID |
| `process.exit` | `(code i32)` | `exit_group(2)` — valid from parent or child |

Handle must be freed by the caller with `memory.heap.free`. No RC destructor.

---

## GPU Kernel Exports

Mark a function export with `@cuda`, `@msl`, or `@vulkan`. The optional
`:type...` list describes the kernel's own parameters.

```wasm
;; no pointer params
(export "warpReduce@cuda"               (func $warpReduce))

;; two buffer pointers + element count
(export "vectorAdd@cuda:ptr.ptr.i32"    (func $vectorAdd))

(export "tileConv@msl:ptr.ptr.i32"      (func $tileConv))

(export "histogram@vulkan:ptr.i32"      (func $histogram))
```

For non-exported kernels, attach the hint via the name custom section instead
of the export table. The syntax is identical.

| Vendor | Platform | Output |
| --- | --- | --- |
| `cuda` | Linux, Windows | PTX text |
| `vulkan` | Linux, Windows | SPIR-V binary |
| `msl` | macOS only | MSL text |