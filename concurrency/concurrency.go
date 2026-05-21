// Package concurrency detects and compiles functions marked with a @kind
// suffix (@async, @thread, @process) and emits all platform machinery for
// them so the frontend only needs to mark intent.
//
// Detection mirrors the gpu package: export names and the wasm name custom
// section are scanned in a single pass; the CPU compiler receives a skip-set
// and never sees these function bodies.
//
// Integration point in compiler.go:
//
//	concFuncs, err := concurrency.Detect(m)
//	concSet  := concurrency.FuncSet(concFuncs)
//	concResult, err := concurrency.Compile(m, concFuncs, concurrency.CompileOptions{})
package concurrency