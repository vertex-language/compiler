// cpu/arm64/func.go
package arm64

import (
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/arm64/asm"
	"github.com/vertex-language/compiler/decode"
	"github.com/vertex-language/compiler/wasm"
)

type ctrlKind int

const (
	ctrlBlock ctrlKind = iota
	ctrlLoop
	ctrlIf
)

type ctrlFrame struct {
	kind       ctrlKind
	arity      int
	paramArity int
	baseDepth  int
	loopTarget int
	endPatches []int
	elseJmpOff int
}

// funcRelocKind distinguishes how target.go resolves each funcReloc.
type funcRelocKind int8

const (
	// rLoadSym: emit ADRP (at codeOff) + LDR (at codeOff+4) for a data symbol.
	// The destination register is implied by the sentinel / always X9 for data,
	// MemBase for __wasm_mem_base.
	rLoadSym funcRelocKind = iota

	// rAddrSym: emit ADRP (at codeOff) + ADD (at codeOff+4) for symbol address.
	// Result lands in X9.
	rAddrSym

	// rCall: emit BL at codeOff for a named external symbol.
	rCall

	// rAddrFunc: emit ADRP (at codeOff) + ADD (at codeOff+4) for a function
	// symbol (ref.func).  The result is immediately pushed onto the operand stack
	// by the instruction at codeOff+8.
	rAddrFunc
)

// funcReloc is an unresolved symbol reference inside a compiled function body.
//
// Sentinel funcIdx values:
//
//	-1  __wasm_mem_base   rLoadSym  → MemBase (X28)
//	-2  __vertex_memory_init       rCall
//	-3  __vertex_handle_table      rAddrSym  → X9
//	-4  __vertex_handle_count      rAddrSym  → X9
type funcReloc struct {
	codeOff int
	kind    funcRelocKind
	funcIdx int  // wasm function index, or negative sentinel
}

type funcCompiler struct {
	asm.Assembler

	ctx            *context.BuildContext
	ft             wasm.FuncType
	locals         []wasm.ValType
	inlinedImports map[int]inlinedImport

	ctrl      []ctrlFrame
	depth     int
	dead      bool
	deadDepth int
	relocs    []funcReloc
}

// compileFuncBody compiles a single wasm function body into A64 machine code,
// returning the raw bytes and any unresolved symbol references.
func compileFuncBody(
	ctx *context.BuildContext,
	funcIdx int,
	inlinedImports map[int]inlinedImport,
) ([]byte, []funcReloc, error) {
	numImports := int(ctx.Module.Imports.NumFuncs())
	localIdx := funcIdx - numImports
	ftIdx := ctx.Module.Functions.TypeIndices[localIdx]
	ft := ctx.Module.Types.Entries[ftIdx]
	body := ctx.Module.Codes.Bodies[localIdx]

	var flatLocals []wasm.ValType
	for _, g := range body.Locals() {
		for i := uint32(0); i < g.Count; i++ {
			flatLocals = append(flatLocals, g.Type)
		}
	}

	fc := &funcCompiler{
		ctx:            ctx,
		ft:             ft,
		locals:         flatLocals,
		inlinedImports: inlinedImports,
	}
	fc.ctrl = append(fc.ctrl, ctrlFrame{
		kind:      ctrlBlock,
		arity:     len(ft.Results),
		baseDepth: 0,
	})

	fc.emitPrologue()
	if err := fc.emitBody(decode.NewReader(body.Code())); err != nil {
		return nil, nil, err
	}
	if !fc.dead {
		fc.emitEpilogue()
	}
	return fc.Bytes(), fc.relocs, nil
}

func (fc *funcCompiler) emitPrologue() {
	nParams := len(fc.ft.Params)
	totalLocals := nParams + len(fc.locals)
	// Frame must be 16-byte aligned.  STP already reserves 16 bytes for X29+X30,
	// so the locals sub needs to be a multiple of 16.
	frameSize := (int32(totalLocals)*8 + 15) &^ 15

	// ── Standard AArch64 frame setup ─────────────────────────────────────────
	// stp x29, x30, [sp, #-16]!   saves FP+LR, decrements SP by 16
	// mov x29, sp                  FP = SP (x29 points at saved FP)
	fc.STP(FP, LR, -2) // imm7=-2 → offset = -2*8 = -16
	fc.MovSP(FP, SP)

	// Allocate locals.
	if frameSize > 0 {
		if frameSize <= 4095 {
			fc.SUBSI(SP, SP, uint32(frameSize))
		} else {
			// Large frame: MOVZ + SUB.
			fc.MOVZ(X9, uint16(frameSize), 0)
			if frameSize > 0xFFFF {
				fc.MOVK(X9, uint16(uint32(frameSize)>>16), 1)
			}
			fc.SUB(SP, SP, X9)
		}
	}

	// Store incoming arguments (X0–X7) into local slots [X29-8*(i+1)].
	bound := nParams
	if bound > 8 {
		bound = 8
	}
	for i := 0; i < bound; i++ {
		fc.emitStoreLocal64(i, ArgRegs[i])
	}

	// Zero-initialise non-parameter locals.
	for i := range fc.locals {
		fc.emitStoreLocal64(nParams+i, XZR)
	}

	// ── Load MemBase (X28 = wasm linear-memory base) ──────────────────────────
	//
	// Fast path: X28 already set (common case after first wasm call).
	// Slow path: call __vertex_memory_init, which mmap's the address space
	// and writes __wasm_mem_base; then reload.
	//
	// adrp x9, __wasm_mem_base     [reloc: sentinel -1, rLoadSym ADRP half]
	// ldr  x28, [x9, :lo12:...]   [reloc: sentinel -1, rLoadSym LDR half]
	fc.emitLoadMemBase()

	// cbnz x28, .ready
	readyPatch := fc.CBNZ(MemBase)

	// Call __vertex_memory_init.  At this point the operand stack is empty
	// (depth==0) and we're past the locals sub, so SP is already 16-byte aligned.
	fc.Emit32(0x94000000) // bl placeholder
	fc.relocs = append(fc.relocs, funcReloc{
		codeOff: fc.Pos() - 4,
		kind:    rCall,
		funcIdx: -2,
	})

	// Reload X28 now that __wasm_mem_base is populated.
	fc.emitLoadMemBase()

	fc.PatchCondImm19(readyPatch, fc.Pos())
}

// emitLoadMemBase emits the ADRP+LDR pair that loads __wasm_mem_base into X28.
func (fc *funcCompiler) emitLoadMemBase() {
	adrpOff := fc.ADRP(X9)
	fc.relocs = append(fc.relocs, funcReloc{codeOff: adrpOff, kind: rLoadSym, funcIdx: -1})
	// ldr x28, [x9, :lo12:__wasm_mem_base]  (placeholder; linker patches lo12)
	fc.LDR64(MemBase, X9, 0)
}

func (fc *funcCompiler) emitEpilogue() {
	if len(fc.ft.Results) >= 1 {
		fc.emitPopR(X0) // return value in X0 per AAPCS64
	}
	// Collapse operand stack back to frame pointer.
	fc.MovSP(SP, FP) // mov sp, x29
	// ldp x29, x30, [sp], #16   restores FP+LR, increments SP
	fc.LDP(FP, LR, 2) // imm7=+2 → offset = +16
	fc.RET()
}