package x86_64

import (
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

// funcCompiler compiles a single wasm function body to x86-64 machine code.
// It embeds asm.Assembler so all instruction-emission methods are available
// directly (fc.Push, fc.Pop, fc.LoadMem64, etc.) without a named field.
type funcCompiler struct {
	asm.Assembler // raw x86-64 emitter — owns the output byte buffer

	m              *wasm.Module
	ft             wasm.FuncType
	locals         []wasm.ValType
	importPtrMasks map[int][]bool
	inlinedImports map[int]inlinedImport

	ctrl      []ctrlFrame
	depth     int  // current wasm operand-stack depth (in pushed 8-byte slots)
	dead      bool // true if current position is unreachable
	deadDepth int  // nested dead-scope counter
	relocs    []funcReloc
}

func compileFuncBody(
	m *wasm.Module,
	ft wasm.FuncType,
	body *wasm.FunctionBody,
	importPtrMasks map[int][]bool,
	inlinedImports map[int]inlinedImport,
) ([]byte, []funcReloc, error) {
	var flatLocals []wasm.ValType
	for _, g := range body.Locals() {
		for i := uint32(0); i < g.Count; i++ {
			flatLocals = append(flatLocals, g.Type)
		}
	}

	fc := &funcCompiler{
		m:              m,
		ft:             ft,
		locals:         flatLocals,
		importPtrMasks: importPtrMasks,
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

	// Spill register arguments into their local slots.
	bound := nParams
	if bound > 6 { bound = 6 }
	for i := 0; i < bound; i++ {
		fc.StoreLocal64(i, ArgRegs[i])
	}

	// Zero-initialise declared locals.
	if len(fc.locals) > 0 {
		fc.Emit(0x31, 0xC0) // xor eax, eax
		for i := 0; i < len(fc.locals); i++ {
			fc.StoreLocal64(nParams+i, RAX)
		}
	}

	// Load the wasm linear-memory base into R15:
	//   lea r15, [rip + __wasm_data_base]
	//   add r15, 65536
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