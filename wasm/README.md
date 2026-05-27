# wasm

Package `wasm` provides a pure-Go, low-level builder and data model for WebAssembly modules. It covers the full WebAssembly 2.0 instruction set plus the reference-types and bulk-memory proposals. There is no runtime or interpreter — the package is purely concerned with constructing and representing the binary format.

```go
import "github.com/vertex-language/compiler/wasm"
```

---

## Overview

A WebAssembly module is represented by `Module`, which holds one struct field per standard section. You populate those sections using the typed helpers, then hand the completed `Module` to a separate encoder or decoder (not part of this package).

```
Module
 ├── TypeSection      – function signatures
 ├── ImportSection    – imported functions, tables, memories, globals
 ├── FunctionSection  – type-index for each locally-defined function
 ├── TableSection     – table definitions
 ├── MemorySection    – memory definitions
 ├── GlobalSection    – global variables with initialiser expressions
 ├── ExportSection    – exported names
 ├── Start            – optional start-function index
 ├── ElementSection   – element segments (active / passive / declarative)
 ├── CodeSection      – function bodies (locals + instructions)
 ├── DataSection      – data segments (active / passive)
 └── Customs          – arbitrary named custom sections
```

---

## Quick start

```go
m := wasm.NewModule()

// 1. Register a function type: () → (i32)
typeIdx := m.Types.AddFuncType(wasm.FuncType{
    Params:  nil,
    Results: []wasm.ValType{wasm.I32},
})

// 2. Declare the local function
funcIdx := m.Functions.Add(typeIdx)

// 3. Export it
m.Exports.Add("answer", wasm.ExportFunc, m.Imports.NumFuncs()+funcIdx)

// 4. Build the body
body := wasm.NewFunctionBody()
body.I32Const(42).End()
m.Codes.Add(body)

// Pass m to your encoder.
```

---

## Types

### Value types (`ValType`)

| Constant     | Byte   | Description                  |
|--------------|--------|------------------------------|
| `I32`        | `0x7F` | 32-bit integer               |
| `I64`        | `0x7E` | 64-bit integer               |
| `F32`        | `0x7D` | 32-bit float                 |
| `F64`        | `0x7C` | 64-bit float                 |
| `V128`       | `0x7B` | 128-bit SIMD vector          |
| `FuncRef`    | `0x70` | Function reference           |
| `ExternRef`  | `0x6F` | External (host) reference    |

### Composite types

```go
FuncType   { Params, Results []ValType }
TableType  { Element ValType; Lim Limits }
MemoryType { Lim Limits; Shared bool }
GlobalType { Val ValType; Mutable bool }
```

`Limits` carries a `Min uint32` and an optional `Max *uint32` (`nil` = unbounded).

### Block types

`BlockType` is the immediate of `block` / `loop` / `if`:

| Type         | Meaning                                               |
|--------------|-------------------------------------------------------|
| `BlockEmpty` | No params, no results (encodes as `0x40`)             |
| `BlockVal`   | Single result value type                              |
| `BlockIdx`   | Multi-value: references a `FuncType` by type index    |

### Constant expressions (`ConstExpr`)

Constant expressions are used in global initialisers, element offsets, and data offsets. Constructors:

```go
wasm.ConstI32(v int32)           // i32.const
wasm.ConstI64(v int64)           // i64.const
wasm.ConstF32(v float32)         // f32.const
wasm.ConstF64(v float64)         // f64.const
wasm.ConstGlobalGet(idx uint32)  // global.get
wasm.ConstRefNull(ht HeapType)   // ref.null
wasm.ConstRefFunc(idx uint32)    // ref.func
```

All constructors append the mandatory `end` opcode (`0x0B`). For decoder use, `NewConstExprRaw(b []byte)` wraps already-encoded bytes.

---

## Sections

Every section exposes an `Add*` helper that appends an entry and returns its `uint32` index, and a `Len() int` method.

### TypeSection

```go
idx := m.Types.AddFuncType(wasm.FuncType{...})
```

### ImportSection

```go
m.Imports.AddFunc("env", "print", typeIdx)
m.Imports.AddTable("env", "tbl", wasm.TableType{...})
m.Imports.AddMemory("env", "mem", wasm.MemoryType{...})
m.Imports.AddGlobal("env", "sp", wasm.GlobalType{...})

offset := m.Imports.NumFuncs() // first local function index
```

> **Important:** Locally-defined function indices start at `ImportSection.NumFuncs()`.

### FunctionSection

Maps each local function to a type index:

```go
localFuncIdx := m.Functions.Add(typeIdx)
```

### GlobalSection

```go
m.Globals.Add(wasm.GlobalType{Val: wasm.I32, Mutable: true}, wasm.ConstI32(0))
```

### ExportSection

```go
m.Exports.Add("memory", wasm.ExportMem, 0)
m.Exports.Add("main",   wasm.ExportFunc, funcIdx)
```

Export kinds: `ExportFunc`, `ExportTable`, `ExportMem`, `ExportGlobal`.

### ElementSection

Three modes are supported:

```go
// Active – initialises a table at instantiation
seg := wasm.ElemSegment{
    Mode:     wasm.ElemModeActive,
    TableIdx: 0,
    Offset:   wasm.ConstI32(0),
    Items:    wasm.ElemFuncIndices{fnA, fnB},
}

// Passive – loaded at runtime via table.init
seg := wasm.ElemSegment{
    Mode:  wasm.ElemModePassive,
    Items: wasm.ElemFuncIndices{fnA},
}

// Declarative – forward-declares ref.func targets
seg := wasm.ElemSegment{
    Mode:  wasm.ElemModeDeclarative,
    Items: wasm.ElemFuncIndices{fnA},
}

m.Elements.Add(seg)
```

`Items` is either `ElemFuncIndices` (plain `[]uint32`) or `ElemExpressions` (typed constant-expression list).

### DataSection

```go
// Active data at memory offset 0x100
m.Datas.Add(
    wasm.DataModeActive{MemIdx: 0, Offset: wasm.ConstI32(0x100)},
    []byte("hello, wasm"),
)

// Passive (loaded via memory.init)
m.Datas.Add(wasm.DataModePassive{}, payload)
```

### CodeSection

```go
body := wasm.NewFunctionBody()
// … emit instructions …
m.Codes.Add(body)
```

### Custom sections

```go
m.Customs = append(m.Customs, wasm.CustomSection{
    Name: "name",
    Data: nameBytes,
})
```

---

## FunctionBody

`FunctionBody` is a fluent instruction builder. Every emit method returns `*FunctionBody`, so calls chain naturally.

### Locals

```go
body := wasm.NewFunctionBody()
body.AddLocals(2, wasm.I32) // two i32 locals
body.AddLocals(1, wasm.F64) // one f64 local
```

Locals are indexed after the function's parameters.

### Instruction reference

All methods map 1-to-1 to WebAssembly opcodes. The table below lists them by category.

#### Control

| Method | Opcode |
|---|---|
| `Unreachable()` | `0x00` |
| `Nop()` | `0x01` |
| `Block(bt)` | `0x02` |
| `Loop(bt)` | `0x03` |
| `If(bt)` | `0x04` |
| `Else()` | `0x05` |
| `End()` | `0x0B` |
| `Br(label)` | `0x0C` |
| `BrIf(label)` | `0x0D` |
| `BrTable(targets, default)` | `0x0E` |
| `Return()` | `0x0F` |
| `Call(funcIdx)` | `0x10` |
| `CallIndirect(typeIdx, tableIdx)` | `0x11` |
| `ReturnCall(funcIdx)` | `0x12` |
| `ReturnCallIndirect(typeIdx, tableIdx)` | `0x13` |

#### Parametric

| Method | Opcode |
|---|---|
| `Drop()` | `0x1A` |
| `Select()` | `0x1B` |
| `SelectTyped(vt)` | `0x1C` |

#### Variables

| Method | Opcode |
|---|---|
| `LocalGet(idx)` | `0x20` |
| `LocalSet(idx)` | `0x21` |
| `LocalTee(idx)` | `0x22` |
| `GlobalGet(idx)` | `0x23` |
| `GlobalSet(idx)` | `0x24` |
| `TableGet(idx)` | `0x25` |
| `TableSet(idx)` | `0x26` |

#### Memory load / store

All load and store methods take `(align, offset uint32)`.

Loads: `I32Load`, `I64Load`, `F32Load`, `F64Load`, and the sign/zero-extending variants `I32Load8S/U`, `I32Load16S/U`, `I64Load8S/U`, `I64Load16S/U`, `I64Load32S/U`.

Stores: `I32Store`, `I64Store`, `F32Store`, `F64Store`, `I32Store8`, `I32Store16`, `I64Store8`, `I64Store16`, `I64Store32`.

```go
body.I32Load(2, 0)   // align=4, offset=0
body.I32Store8(0, 4) // align=1, offset=4
```

#### Memory control

```go
body.MemorySize() // memory.size
body.MemoryGrow() // memory.grow
```

#### Numeric constants

```go
body.I32Const(42)
body.I64Const(-1)
body.F32Const(3.14)
body.F64Const(math.Pi)
```

#### Integer comparisons

i32: `I32Eqz`, `I32Eq`, `I32Ne`, `I32LtS/U`, `I32GtS/U`, `I32LeS/U`, `I32GeS/U`

i64: `I64Eqz`, `I64Eq`, `I64Ne`, `I64LtS/U`, `I64GtS/U`, `I64LeS/U`, `I64GeS/U`

#### Float comparisons

f32: `F32Eq`, `F32Ne`, `F32Lt`, `F32Gt`, `F32Le`, `F32Ge`

f64: `F64Eq`, `F64Ne`, `F64Lt`, `F64Gt`, `F64Le`, `F64Ge`

#### Integer arithmetic

i32: `I32Clz`, `I32Ctz`, `I32Popcnt`, `I32Add`, `I32Sub`, `I32Mul`, `I32DivS/U`, `I32RemS/U`, `I32And`, `I32Or`, `I32Xor`, `I32Shl`, `I32ShrS/U`, `I32Rotl`, `I32Rotr`

i64: same shape, `I64*` prefix.

#### Float arithmetic

f32: `F32Abs`, `F32Neg`, `F32Ceil`, `F32Floor`, `F32Trunc`, `F32Nearest`, `F32Sqrt`, `F32Add`, `F32Sub`, `F32Mul`, `F32Div`, `F32Min`, `F32Max`, `F32Copysign`

f64: same shape, `F64*` prefix.

#### Conversions

`I32WrapI64`, `I32TruncF32S/U`, `I32TruncF64S/U`, `I64ExtendI32S/U`, `I64TruncF32S/U`, `I64TruncF64S/U`, `F32ConvertI32S/U`, `F32ConvertI64S/U`, `F32DemoteF64`, `F64ConvertI32S/U`, `F64ConvertI64S/U`, `F64PromoteF32`, `I32ReinterpretF32`, `I64ReinterpretF64`, `F32ReinterpretI32`, `F64ReinterpretI64`

#### Sign-extension (Wasm 2.0)

`I32Extend8S`, `I32Extend16S`, `I64Extend8S`, `I64Extend16S`, `I64Extend32S`

#### Reference types (Wasm 2.0)

```go
body.RefNull(wasm.HeapFunc)
body.RefIsNull()
body.RefFunc(idx)
```

#### Saturating truncation (`0xFC`)

`I32TruncSatF32S/U`, `I32TruncSatF64S/U`, `I64TruncSatF32S/U`, `I64TruncSatF64S/U`

#### Bulk memory / table (`0xFC`)

```go
body.MemoryInit(dataIdx)
body.DataDrop(dataIdx)
body.MemoryCopy()
body.MemoryFill()
body.TableInit(elemIdx, tableIdx)
body.ElemDrop(elemIdx)
body.TableCopy(dstIdx, srcIdx)
body.TableGrow(tableIdx)
body.TableSize(tableIdx)
body.TableFill(tableIdx)
```

### Decoder support

When the decoder reconstructs a `FunctionBody` from binary, it uses:

```go
body := wasm.NewFunctionBodyRaw(locals []wasm.LocalGroup, code []byte)
```

`body.Locals()` and `body.Code()` expose the raw data back to the encoder.

---

## Complete example

```go
m := wasm.NewModule()

// Type: (i32, i32) → (i32)
addType := m.Types.AddFuncType(wasm.FuncType{
    Params:  []wasm.ValType{wasm.I32, wasm.I32},
    Results: []wasm.ValType{wasm.I32},
})

// Local function
localIdx := m.Functions.Add(addType)
globalFuncIdx := m.Imports.NumFuncs() + localIdx

// Export
m.Exports.Add("add", wasm.ExportFunc, globalFuncIdx)

// Body: local.get 0 + local.get 1
body := wasm.NewFunctionBody()
body.LocalGet(0).LocalGet(1).I32Add().End()
m.Codes.Add(body)

// Encode m with your encoder of choice.
```

---

## Design notes

- **No serialisation logic lives here.** `Module` and its section types are pure data; encode and decode responsibility belongs to separate packages.
- **`FunctionBody` is append-only.** Instructions are written directly into a `[]byte` buffer using LEB128 helpers from `github.com/vertex-language/compiler/encode`. There is intentionally no IR or tree representation.
- **`ConstExpr` always includes the trailing `end` byte (`0x0B`).** This matches the binary format exactly and avoids off-by-one errors at the encoder boundary.
- **Index spaces follow the Wasm spec.** Imported functions occupy the lowest indices; `ImportSection.NumFuncs()` gives the base offset for local functions.