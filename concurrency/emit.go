package concurrency

import (
	"fmt"

	concasync "github.com/vertex-language/compiler/concurrency/async"
	"github.com/vertex-language/compiler/concurrency/process"
	"github.com/vertex-language/compiler/concurrency/thread"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// CompileOptions controls concurrency compilation behaviour.
// Extended by individual backends as they are implemented.
type CompileOptions struct{}

// CompileResult holds the compiled artifacts for all concurrent functions.
// Obj carries entry stubs, scheduler primitives, and runtime glue in .text,
// plus any data in .rodata, ready to be merged into the CPU object.
type CompileResult struct {
	Obj   *object.WasmObj
	Funcs []FuncInfo
}

// ErrBackendNotAvailable is returned when the relevant backend has not yet
// been fully implemented.
var ErrBackendNotAvailable = fmt.Errorf("concurrency backend not yet available")

// Compile compiles all concurrent functions in funcs.
// Each function is dispatched to the backend matching its Kind:
//
//	KindAsync   → concurrency/async  (stackful coroutines, ucontext-based)
//	KindThread  → concurrency/thread (clone3 + futex join)
//	KindProcess → concurrency/process (fork/clone + waitpid)
func Compile(m *wasm.Module, funcs []FuncInfo, opts CompileOptions) (*CompileResult, error) {
	if len(funcs) == 0 {
		return &CompileResult{Obj: &object.WasmObj{}}, nil
	}

	result := &CompileResult{
		Obj:   &object.WasmObj{},
		Funcs: funcs,
	}

	for _, f := range funcs {
		if err := compileFunc(m, f, result); err != nil {
			return nil, fmt.Errorf("concurrency: function %q (@%s, func %d): %w",
				f.Name, f.Kind, f.FuncIdx, err)
		}
	}

	return result, nil
}

func compileFunc(m *wasm.Module, f FuncInfo, result *CompileResult) error {
	switch f.Kind {
	case KindAsync:
		return compileAsync(m, f, result)
	case KindThread:
		return compileThread(m, f, result)
	case KindProcess:
		return compileProcess(m, f, result)
	default:
		return fmt.Errorf("unknown kind %q", f.Kind)
	}
}

func compileAsync(m *wasm.Module, f FuncInfo, result *CompileResult) error {
	obj, err := concasync.Compile(m, concasync.FuncInfo{
		FuncIdx: f.FuncIdx,
		Name:    f.Name,
		Params:  f.Params,
	}, concasync.CompileOptions{})
	if err != nil {
		return err
	}
	mergeObjects(result.Obj, obj)
	return nil
}

func compileThread(m *wasm.Module, f FuncInfo, result *CompileResult) error {
	obj, err := thread.Compile(m, thread.FuncInfo{
		FuncIdx: f.FuncIdx,
		Name:    f.Name,
		Params:  f.Params,
	}, thread.CompileOptions{})
	if err != nil {
		return err
	}
	mergeObjects(result.Obj, obj)
	return nil
}

func compileProcess(m *wasm.Module, f FuncInfo, result *CompileResult) error {
	obj, err := process.Compile(m, process.FuncInfo{
		FuncIdx: f.FuncIdx,
		Name:    f.Name,
		Params:  f.Params,
	}, process.CompileOptions{})
	if err != nil {
		return err
	}
	mergeObjects(result.Obj, obj)
	return nil
}

// mergeObjects appends src into dst, adjusting code-section offsets.
func mergeObjects(dst, src *object.WasmObj) {
	if src == nil || (len(src.Code) == 0 && len(src.Data) == 0) {
		return
	}
	codeBase := len(dst.Code)
	dst.Code = append(dst.Code, src.Code...)
	if len(src.Data) > 0 {
		dst.Data = append(dst.Data, src.Data...)
	}
	for _, sym := range src.Symbols {
		sym.Offset += codeBase
		dst.Symbols = append(dst.Symbols, sym)
	}
	for _, rel := range src.Relocs {
		rel.Offset += codeBase
		dst.Relocs = append(dst.Relocs, rel)
	}
}