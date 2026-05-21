package x86_64

import (
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/x86_64/asm"
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

type funcReloc struct {
	codeOff int
	funcIdx int
	isAbs64 bool
}

type funcCompiler struct {
	asm.Assembler // raw x86-64 emitter

	ctx            *context.BuildContext // Replaces m and importPtrMasks
	ft             wasm.FuncType
	locals         []wasm.ValType
	inlinedImports map[int]inlinedImport

	ctrl      []ctrlFrame
	depth     int
	dead      bool
	deadDepth int
	relocs    []funcReloc
}

func compileFuncBody(
	ctx *context.BuildContext,
	funcIdx int,
	inlinedImports map[int]inlinedImport,
) ([]byte, []funcReloc, error) {
	
	// Resolve the function type and body based on the index
	localIdx := funcIdx - int(ctx.Module.Imports.NumFuncs())
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
	frameSize := int32(totalLocals) * 8
	frameSize = (frameSize + 15) &^ 15

	fc.Push(RBP)
	fc.Emit(0x48, 0x89, 0xE5) // mov rbp, rsp
	if frameSize > 0 {
		fc.SubRI(RSP, int64(frameSize))
	}

	bound := nParams
	if bound > 6 { bound = 6 }
	for i := 0; i < bound; i++ {
		fc.StoreLocal64(i, ArgRegs[i])
	}

	if len(fc.locals) > 0 {
		fc.Emit(0x31, 0xC0) // xor eax, eax
		for i := 0; i < len(fc.locals); i++ {
			fc.StoreLocal64(nParams+i, RAX)
		}
	}

	fc.Emit(0x4C, 0x8D, 0x3D) // lea r15, [rip + ???]
	fc.relocs = append(fc.relocs, funcReloc{
		codeOff: fc.Pos(),
		funcIdx: -1, // sentinel → __wasm_data_base
	})
	fc.Emit(0, 0, 0, 0)
	fc.Emit(0x49, 0x81, 0xC7) // add r15, imm32
	fc.Emit32(65536)
}

func (fc *funcCompiler) emitEpilogue() {
	if len(fc.ft.Results) >= 1 {
		fc.emitPopR(RAX)
	}
	fc.Emit(0x48, 0x89, 0xEC) // mov rsp, rbp
	fc.Pop(RBP)
	fc.Ret()
}