# GPU Kernel Backend

Internal reference for the `gpu` package in `vertex-language/compiler`.

---

## Overview

GPU kernel compilation is a parallel codegen path inside the Vertex compiler.
Functions are routed to the GPU backend by annotating their name with a
`@vendor` suffix — the same `@`-suffix grammar used for import calling
conventions. The compiler reads the suffix, validates the function, and sends
the entire body to the appropriate backend (PTX, SPIR-V, or MSL).

The output is the same artifact as any other Vertex compilation: a fully linked
ELF64 binary. GPU blobs are baked into `.rodata`. A small native stub in `.text`
probes for available hardware at runtime and dispatches to the right blob. No
SDK dependency, no external files, no Go runtime.

---

## Philosophy

No abstraction layer. No virtual intrinsics. No vendor-neutral shim.

The same way `@ptr.f32` on an import name self-describes its calling convention,
`@cuda`, `@vulkan`, and `@metal` on a function name self-describe where the
entire function body should be compiled to. The hint lives on the thing being
described. Nothing is inferred from callees. Nothing is tracked in a separate
index list.

```
Your Language → WebAssembly IR (functions marked @cuda / @metal / @vulkan) → PTX / MSL / SPIR-V → ELF Binary
```

A function marked `@cuda` is compiled entirely by the PTX backend. A function
marked `@metal` goes entirely to the MSL backend. A function marked `@vulkan`
goes entirely to the SPIR-V backend. Mixing vendor suffixes across a single
function's call tree that ends up in the same body is a compile error.

---

## The `@vendor` Naming Convention

### Syntax

```
"<name>@<vendor>"
"<name>@<vendor>:<type>.<type>.<type>..."
```

The vendor token routes the function to a backend. The optional type list after
`:` annotates the function's own parameters — same token set as import
signatures.

| Token  | Meaning                                                              |
|--------|----------------------------------------------------------------------|
| `i32`  | 32-bit integer — passed as-is                                        |
| `i64`  | 64-bit integer — passed as-is                                        |
| `f32`  | 32-bit float — passed as-is                                          |
| `f64`  | 64-bit float — passed as-is                                          |
| `ptr`  | Linear-memory i32 offset — auto-translated to native VA before call  |

| Vendor    | Backend  | Platforms          |
|-----------|----------|--------------------|
| `cuda`    | PTX      | Linux, Windows     |
| `vulkan`  | SPIR-V   | Linux, Windows     |
| `metal`   | MSL      | macOS only         |

### Examples

```go
// Kernel with no pointer params — vendor suffix only
m.Exports.Add("warpReduce@cuda",   wasm.ExportFunc, warpReduceIdx)
m.Exports.Add("histogram@vulkan",  wasm.ExportFunc, histogramIdx)
m.Exports.Add("tileConv@metal",    wasm.ExportFunc, tileConvIdx)

// Kernel whose first param is a linear-memory buffer pointer
m.Exports.Add("vectorAdd@cuda:ptr.ptr.i32",   wasm.ExportFunc, vectorAddIdx)
m.Exports.Add("scatter@vulkan:ptr.ptr.i32",   wasm.ExportFunc, scatterIdx)
m.Exports.Add("fillBuf@metal:ptr.f32.i32",    wasm.ExportFunc, fillBufIdx)
```

The wasm binary remains 100% spec-compliant. Export names are arbitrary UTF-8
strings; `@`, `:`, and `.` are all valid. `wasm-validate` passes,
`wasm2wat` renders the name intact, and any wasm runtime that does not know
about Vertex simply sees an export called `"vectorAdd@cuda:ptr.ptr.i32"`.

---

## Marking Functions That Are Not Exported

Not every GPU kernel needs to be a public export. For internal functions, use
the wasm **name custom section** with the same `@vendor` suffix. The compiler
reads name-section entries during the detection pass.

```go
import "github.com/vertex-language/compiler/gpu"

// Attach @vendor hints to non-exported functions via the name section
m.Customs = append(m.Customs, gpu.NameHints(map[uint32]string{
    warpReduceIdx: "warpReduce@cuda",
    histogramIdx:  "histogram@cuda:ptr.i32",
}))
```

`gpu.NameHints` writes a standard wasm `"name"` custom section subsection 1
(function names). The names are valid wasm name-section entries and will render
correctly in `wasm2wat` output.

---

## Detection

The compiler detects GPU functions in a single pass over export entries and the
name custom section:

1. **Export name** — check for `@vendor` suffix; highest priority.
2. **Name custom section** — check function name entries for `@vendor` suffix.

A function with no `@vendor` suffix in either place is compiled as a normal CPU
function by the x86-64 emitter. Import module scanning and the `vertex.gpu`
custom section are no longer used.

```go
// gpu/detect.go — simplified
func Detect(m *wasm.Module) ([]KernelInfo, error) {
    var kernels []KernelInfo

    // pass 1: export names
    for _, e := range m.Exports.Entries {
        if e.Kind != wasm.ExportFunc {
            continue
        }
        if v, params, ok := parseVendorSuffix(e.Name); ok {
            kernels = append(kernels, KernelInfo{
                FuncIdx: e.Idx,
                Name:    stripSuffix(e.Name),
                Vendor:  v,
                Params:  params,
                Source:  SourceExport,
            })
        }
    }

    // pass 2: name custom section (catches non-exported kernels)
    for _, entry := range nameSection(m) {
        if alreadyDetected(kernels, entry.FuncIdx) {
            continue
        }
        if v, params, ok := parseVendorSuffix(entry.Name); ok {
            kernels = append(kernels, KernelInfo{
                FuncIdx: entry.FuncIdx,
                Name:    stripSuffix(entry.Name),
                Vendor:  v,
                Params:  params,
                Source:  SourceNameSection,
            })
        }
    }

    return validate(kernels)
}
```

---

## Vendor Backends

| Module    | Vendor                        | Platforms          | Output     |
|-----------|-------------------------------|--------------------|------------|
| `cuda`    | NVIDIA                        | Linux, Windows     | PTX text   |
| `vulkan`  | AMD + CPU software fallback   | Linux, Windows     | SPIR-V binary |
| `metal`   | Apple                         | macOS only         | MSL text   |

On Linux and Windows, both PTX and SPIR-V blobs are embedded. A native stub
probes for `libcuda.so.1` at runtime; if the probe fails it falls through to
Vulkan/SPIR-V. SwiftShader or llvmpipe will run the SPIR-V blob on machines
with no GPU.

On macOS, Metal is always present as a system framework. No probe is emitted —
the stub calls `MTLCreateSystemDefaultDevice` directly.

```
target          blobs embedded     probe
──────────────────────────────────────────────────────
linux-amd64     PTX + SPIR-V       dlopen libcuda.so.1 → Vulkan fallback
windows-amd64   PTX + SPIR-V       LoadLibrary nvcuda.dll → Vulkan fallback
macos-arm64     MSL                none — Metal always present
```

---

## Native GPU Intrinsics

GPU built-ins are imported using exact names from CUDA, Metal, or Vulkan. The
compiler lowers them to the corresponding PTX, MSL, or SPIR-V instruction.
Intrinsics that are built-in variables in C++ (e.g. `threadIdx.x`) are
zero-argument functions returning `i32` in the Wasm type system.

These imports are ordinary wasm imports with no special module name. The module
field is `"gpu"` — a neutral namespace that does not trigger vendor detection.
Vendor detection happens only via the `@vendor` suffix on the function being
compiled, as described above.

### CUDA (`gpu` module, function prefix `cuda.`)

```
Import name                          PTX
────────────────────────────────────────────────────────────────
cuda.threadIdx.x/y/z                 %tid.x / %tid.y / %tid.z
cuda.blockIdx.x/y/z                  %ctaid.x / %ctaid.y / %ctaid.z
cuda.blockDim.x/y/z                  %ntid.x / %ntid.y / %ntid.z
cuda.warpSize                        (constant 32)

cuda.__syncthreads                   bar.sync 0
cuda.__threadfence                   membar.gl
cuda.__threadfence_block             membar.cta

cuda.__shfl_down_sync.f32(mask,v,d)  shfl.sync.down.b32
cuda.__shfl_up_sync.f32(mask,v,d)    shfl.sync.up.b32
cuda.__shfl_xor_sync.f32(mask,v,m)   shfl.sync.bfly.b32
cuda.__ballot_sync(mask,pred)        vote.sync.ballot.b32
cuda.__activemask                    activemask.b32

cuda.atomicAdd.f32(ptr,v)            atom.global.add.f32
cuda.atomicAdd.i32(ptr,v)            atom.global.add.s32
cuda.atomicCAS.i32(ptr,cmp,v)        atom.global.cas.b32
cuda.atomicExch.i32(ptr,v)           atom.global.exch.b32
cuda.atomicMin.i32(ptr,v)            atom.global.min.s32
cuda.atomicMax.i32(ptr,v)            atom.global.max.s32

cuda.__shared__ (memory region)      .shared state space
```

### Metal (`gpu` module, function prefix `metal.`)

```
Import name                                  MSL
──────────────────────────────────────────────────────────────────────────
metal.thread_position_in_threadgroup.x/y/z   uint3 thread_position_in_threadgroup
metal.threadgroup_position_in_grid.x/y/z     uint3 threadgroup_position_in_grid
metal.threads_per_threadgroup.x/y/z          uint3 threads_per_threadgroup
metal.thread_position_in_grid.x/y/z          uint3 thread_position_in_grid
metal.simdgroup_size                          (constant 32)

metal.threadgroup_barrier                    threadgroup_barrier(mem_flags::mem_threadgroup)
metal.simd_shuffle_down.f32(v,d)             simd_shuffle_down(v,d)
metal.simd_shuffle_up.f32(v,d)               simd_shuffle_up(v,d)
metal.simd_shuffle_xor.f32(v,m)              simd_shuffle_xor(v,m)
metal.simd_ballot(pred)                      simd_ballot(pred)
metal.simd_active_threads_mask               simd_active_threads_mask()

metal.atomic_fetch_add_explicit.f32(ptr,v)   atomic_fetch_add_explicit(..., memory_order_relaxed)
metal.atomic_fetch_add_explicit.i32(ptr,v)   atomic_fetch_add_explicit(..., memory_order_relaxed)
metal.atomic_compare_exchange(ptr,cmp,v)     atomic_compare_exchange_weak_explicit(...)

metal.threadgroup (memory region)            threadgroup qualifier
```

### Vulkan (`gpu` module, function prefix `vulkan.`)

```
Import name                          SPIR-V
──────────────────────────────────────────────────────────────────────────
vulkan.gl_LocalInvocationID.x/y/z    LocalInvocationId built-in
vulkan.gl_WorkGroupID.x/y/z          WorkgroupId built-in
vulkan.gl_WorkGroupSize.x/y/z        WorkgroupSize built-in
vulkan.gl_SubgroupSize               subgroupSize built-in
vulkan.gl_SubgroupInvocationID       SubgroupLocalInvocationId built-in

vulkan.controlBarrier                OpControlBarrier (Workgroup, Workgroup, AcquireRelease)
vulkan.memoryBarrier                 OpMemoryBarrier (Workgroup, AcquireRelease)

vulkan.subgroupShuffleDown.f32(v,d)  OpGroupNonUniformShuffleDown
vulkan.subgroupShuffleUp.f32(v,d)    OpGroupNonUniformShuffleUp
vulkan.subgroupShuffleXor.f32(v,m)   OpGroupNonUniformShuffleXor
vulkan.subgroupBallot(pred)          OpGroupNonUniformBallot
vulkan.subgroupElect                 OpGroupNonUniformElect

vulkan.atomicAdd.f32(ptr,v)          OpAtomicFAddEXT (requires VK_EXT_shader_atomic_float)
vulkan.atomicAdd.i32(ptr,v)          OpAtomicIAdd
vulkan.atomicCompareExchange(p,c,v)  OpAtomicCompareExchange
vulkan.atomicExchange.i32(ptr,v)     OpAtomicExchange
vulkan.atomicMin.i32(ptr,v)          OpAtomicSMin
vulkan.atomicMax.i32(ptr,v)          OpAtomicSMax

vulkan.workgroup (memory region)     Workgroup storage class
```

---

## How a Frontend Targets GPU

Two things are required:

1. Import the GPU built-ins your kernel uses from the `"gpu"` module.
2. Mark the kernel function with a `@vendor` suffix on its export name or name-section entry.

The compiler does the rest.

### CUDA — Go

```go
// ── Import GPU built-ins from the "gpu" module ──────────────────────────────

tCoord := m.Types.AddFuncType(wasm.FuncType{
    Results: []wasm.ValType{wasm.I32},
})
tSync := m.Types.AddFuncType(wasm.FuncType{})
tShflDown := m.Types.AddFuncType(wasm.FuncType{
    Params:  []wasm.ValType{wasm.I32, wasm.F32, wasm.I32},
    Results: []wasm.ValType{wasm.F32},
})
tAtomicAddF32 := m.Types.AddFuncType(wasm.FuncType{
    Params: []wasm.ValType{wasm.I32, wasm.F32},
})

m.Imports.AddFunc("gpu", "cuda.threadIdx.x",         tCoord)
m.Imports.AddFunc("gpu", "cuda.blockIdx.x",          tCoord)
m.Imports.AddFunc("gpu", "cuda.blockDim.x",          tCoord)
m.Imports.AddFunc("gpu", "cuda.__syncthreads",        tSync)
m.Imports.AddFunc("gpu", "cuda.__shfl_down_sync.f32", tShflDown)
m.Imports.AddFunc("gpu", "cuda.atomicAdd.f32",        tAtomicAddF32)

// ── Define the kernel function ───────────────────────────────────────────────

tKernel := m.Types.AddFuncType(wasm.FuncType{
    Params: []wasm.ValType{wasm.I32, wasm.F32, wasm.I32},
})
kernelIdx := m.Functions.Add(tKernel)

b := wasm.NewFunctionBody()
// ... kernel body using the imported built-ins ...
b.End()
m.Codes.Add(b)

// ── Mark as CUDA via the @vendor suffix ─────────────────────────────────────
// First param is a linear-memory buffer pointer → ptr token
// Remaining params are f32 value and i32 count

m.Exports.Add("warpReduce@cuda:ptr.f32.i32", wasm.ExportFunc, kernelIdx)
```

### Metal — Go

```go
tCoord   := m.Types.AddFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.I32}})
tBarrier := m.Types.AddFuncType(wasm.FuncType{})
tAtomicF32 := m.Types.AddFuncType(wasm.FuncType{
    Params: []wasm.ValType{wasm.I32, wasm.F32},
})

m.Imports.AddFunc("gpu", "metal.thread_position_in_threadgroup.x", tCoord)
m.Imports.AddFunc("gpu", "metal.threadgroup_position_in_grid.x",   tCoord)
m.Imports.AddFunc("gpu", "metal.threads_per_threadgroup.x",        tCoord)
m.Imports.AddFunc("gpu", "metal.threadgroup_barrier",              tBarrier)
m.Imports.AddFunc("gpu", "metal.atomic_fetch_add_explicit.f32",    tAtomicF32)

// ...define function body...

m.Exports.Add("tileConv@metal:ptr.ptr.i32", wasm.ExportFunc, kernelIdx)
```

### Vulkan — Go

```go
tCoord    := m.Types.AddFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.I32}})
tBarrier  := m.Types.AddFuncType(wasm.FuncType{})
tAtomicI32 := m.Types.AddFuncType(wasm.FuncType{
    Params:  []wasm.ValType{wasm.I32, wasm.I32},
    Results: []wasm.ValType{wasm.I32},
})

m.Imports.AddFunc("gpu", "vulkan.gl_LocalInvocationID.x", tCoord)
m.Imports.AddFunc("gpu", "vulkan.gl_WorkGroupID.x",       tCoord)
m.Imports.AddFunc("gpu", "vulkan.gl_WorkGroupSize.x",     tCoord)
m.Imports.AddFunc("gpu", "vulkan.controlBarrier",         tBarrier)
m.Imports.AddFunc("gpu", "vulkan.atomicAdd.i32",          tAtomicI32)

// ...define function body...

m.Exports.Add("histogram@vulkan:ptr.i32", wasm.ExportFunc, kernelIdx)
```

### Non-exported kernel — name section hint

```go
import "github.com/vertex-language/compiler/gpu"

// Internal kernel, not exported — mark via name section
m.Customs = append(m.Customs, gpu.NameHints(map[uint32]string{
    internalKernelIdx: "prefixSum@cuda:ptr.i32",
}))
```

---

## Pointer Parameters

Buffer pointers use the `ptr` token in the `@vendor:...` type list, exactly as
they do on import signatures. The compiler emits the linear-memory-to-native-VA
translation before the kernel dispatch.

```go
// Two buffer pointers + an element count
m.Exports.Add("vectorAdd@cuda:ptr.ptr.i32", wasm.ExportFunc, vectorAddIdx)
```

Intrinsics that carry no pointer (thread coordinates, barriers, shuffles) use
no type list — the `@vendor` suffix alone is sufficient.

```go
m.Exports.Add("warpSum@cuda", wasm.ExportFunc, warpSumIdx)
```

---

## Compilation Pipeline

```go
// Call site is identical to a CPU-only compile.
obj, err := compiler.CompileWith(m, compiler.Options{})

bin, err := linker.Link([]*object.WasmObj{obj}, linker.Options{
    Output: linker.ELF,
    Entry:  "main",
})
```

Internally, `compiler.CompileWith` runs detection then splits:

```
wasm.Module
    │
    │  gpu/detect.go
    │    scan export names and name-section entries for @vendor suffix
    │    validate: no mixed vendors in one function, all indices in range
    │
    ├── cpu functions ──────────────────────────────────► x86-64 emitter → .text
    │
    └── gpu functions  (@cuda / @metal / @vulkan suffix detected)
              │
              ├── [linux / windows — @cuda]
              │       ptx/   → PTX text     (same output as nvcc -ptx)
              │       embed  → PTX blob into .rodata
              │       stub   → dlopen libcuda.so.1 / LoadLibrary nvcuda.dll
              │                   cuLink* compile PTX → cubin
              │                   write cubin to cache  (first run)
              │                   read cubin from cache (reruns)
              │                   fallthrough to Vulkan if no CUDA
              │
              ├── [linux / windows — @vulkan]
              │       spirv/ → SPIR-V binary (same output as glslangValidator)
              │       embed  → SPIR-V blob into .rodata
              │       stub   → vkCreateInstance / vkCreateShaderModule / vkCmdDispatch
              │
              └── [macos — @metal]
                      msl/   → MSL text     (same output as xcrun metal -S)
                      embed  → MSL blob into .rodata
                      stub   → MTLCreateSystemDefaultDevice → direct Metal calls
    │
    ▼
object.WasmObj
    → linker.Link → ELF64
         .text   — cpu code + probe stub + dispatch stub
         .rodata — PTX blob + SPIR-V blob (or MSL blob on macOS)
         (no SDK, no driver, no runtime dependency at build time)
```

---

## Runtime Detection

The detection logic is emitted as native machine code by `gpu/embed.go` directly
into `.text`. By the time the compiled binary runs it is a plain executable that
calls `dlopen` and the GPU vendor APIs exactly as any C program would.

```asm
; emitted into .text — x86-64 Linux
; runs once before the first kernel launch, result cached

    lea  rdi, [rip + libcuda_name]     ; "libcuda.so.1\0"
    mov  rsi, RTLD_LAZY|RTLD_GLOBAL
    call dlopen
    test rax, rax
    jz   .use_vulkan

.use_cuda:
    ; resolve cuInit, cuDeviceGet, cuModuleLoadData, cuLaunchKernel
    ; load PTX blob from .rodata
    jmp  .done

.use_vulkan:
    ; vkCreateInstance, vkCreateShaderModule, vkCmdDispatch
    ; load SPIR-V blob from .rodata

.done:
    ; subsequent launches skip the probe entirely
```

On Windows the identical flow runs with `LoadLibrary("nvcuda.dll")`. On macOS
no probe is emitted — the stub calls Metal directly.

---

## PTX Cubin Caching

PTX is a virtual ISA. The CUDA driver JIT-compiles it to a cubin on first use.
The runtime stub extracts the compiled cubin via the driver's linker API, writes
it to a local cache file, and reloads it directly on subsequent runs — zero JIT
overhead after the first launch. No CUDA toolkit, no `nvcc`, no `ptxas` required
on the user's machine.

### Cache Key

```
<sha256 of PTX text>-sm<major><minor>-drv<driver_version>.cubin

example:
  a3f8c2d1...-sm89-drv560.cubin
```

| Component      | Source                                                           | Purpose |
|----------------|------------------------------------------------------------------|---------|
| PTX hash       | `sha256(ptxBlob)`                                                | Detects new kernel output |
| SM version     | `cuDeviceGetAttribute(CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_*)` | Per-SM codegen differs |
| Driver version | `cuDriverGetVersion`                                             | Cubin ABI can shift across driver releases |

### Cache Directory (priority order)

```
$VERTEX_GPU_CACHE
$XDG_CACHE_HOME/vertex/gpu       Linux default
$HOME/Library/Caches/vertex/gpu  macOS
%LOCALAPPDATA%\vertex\gpu        Windows
```

### Stub Logic

```
first kernel launch
│
├── dlopen libcuda.so.1 / LoadLibrary nvcuda.dll
│       fail → fall through to Vulkan
│
├── cuInit(0)
├── cuDeviceGet(&dev, 0)
├── cuDeviceGetAttribute → major, minor
├── cuDriverGetVersion   → driverVer
│
├── sha256(ptxBlob) → ptxHash
├── cacheKey = ptxHash-sm<major><minor>-drv<driverVer>.cubin
├── cachePath = cacheDir / cacheKey
│
├── stat(cachePath)
│   ├── HIT  → read cachePath → cuModuleLoadData
│   └── MISS → cuLinkCreate
│              cuLinkAddData (CU_JIT_INPUT_PTX)
│              cuLinkComplete → cubinBlob
│              write to tmp, rename to cachePath   (atomic on POSIX / MoveFileEx on Windows)
│              cuModuleLoadData(cubinBlob)
│              cuLinkDestroy
│
└── cuModuleGetFunction → kernel handle
    subsequent launches → cuLaunchKernel directly
```

### Cache Hit Matrix

```
launch              PTX in binary   cubin on disk   work done
───────────────────────────────────────────────────────────────────
first ever          yes             no              cuLink* compile + write
same binary/driver  yes             yes (hit)       read file + cuModuleLoadData
new driver          yes             no  (key miss)  recompile + write new entry
new kernel          yes             no  (key miss)  recompile + write new entry
```

Old cubin files are left in the cache directory and can be swept by any standard
LRU pass. The stub only reads and writes — it does not evict.

---

## Package Layout

```
vertex-language/compiler/
│
└── gpu/
    ├── detect.go          ← scan export names + name section for @vendor suffix;
    │                         validate no mixing, all indices in range
    ├── embed.go           ← wrap compiled GPU output into object.WasmObj;
    │                         emit probe + dispatch + cubin-cache stub as native machine code
    ├── mark.go            ← gpu.NameHints(map[uint32]string) → "name" custom section
    │
    ├── cache/
    │   ├── key.go         ← build cache key: sha256(ptx) + SM version + driver version
    │   ├── dir.go         ← resolve cache directory from env / OS defaults
    │   └── io.go          ← atomic read/write (tmp + rename)
    │
    ├── ptx/               ← NVIDIA — Linux, Windows (@cuda functions)
    │   ├── compiler.go
    │   ├── func.go
    │   ├── body.go
    │   ├── control.go
    │   ├── arith.go
    │   ├── intrinsics.go  ← cuda.* imports → PTX instruction emission
    │   └── registers.go
    │
    ├── msl/               ← Apple — macOS only (@metal functions)
    │   ├── compiler.go
    │   ├── func.go
    │   ├── body.go
    │   ├── control.go
    │   ├── arith.go
    │   └── intrinsics.go  ← metal.* imports → MSL intrinsic emission
    │
    └── spirv/             ← AMD + CPU fallback — Linux, Windows (@vulkan functions)
        ├── compiler.go
        ├── func.go
        ├── body.go
        ├── control.go
        ├── arith.go
        └── intrinsics.go  ← vulkan.* imports → SPIR-V opcode emission
```

---

## Compiler Errors

```
error: vendor mismatch in function "warpReduce"
  @cuda suffix declared but function body imports metal.* intrinsics
  a gpu kernel may only use intrinsics matching its declared vendor

error: mixed vendor suffix not allowed
  export "warpReduce@cuda" and name-section entry "warpReduce@vulkan"
  refer to the same function index 7
  only one @vendor annotation per function is permitted

error: unknown gpu intrinsic "cuda.AtomicAddHalf"
  function "vectorAdd" (index 3, @cuda) references an unrecognised cuda.* import

error: @vendor suffix on import not allowed
  "@cuda" suffix found on import entry "warpReduce@cuda"
  @vendor suffixes are only valid on locally-defined function names

error: ptr annotation required for buffer parameter in "vectorAdd"
  export "vectorAdd@cuda" has i32 at position 0 but the wasm type is i32
  if this parameter is a linear-memory pointer use "vectorAdd@cuda:ptr.i32"
  to enable automatic linear-memory translation

error: function index out of range
  name-section hint references index 9
  module has 6 locally-defined functions
```