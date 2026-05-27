// driver/analyze.go
package driver

import (
	"github.com/vertex-language/compiler/abi"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/wasm"
)

// RoutingTable groups wasm function indices by their destination backend ID.
type RoutingTable map[string][]int

// Analyze processes all import and export ABI strings, populates the shared
// BuildContext with pointer/handle masks, and returns a RoutingTable that 
// maps each function to its backend.
func Analyze(ctx *context.BuildContext, defaultArch string) (RoutingTable, error) {
	routes := make(RoutingTable)

	// 1. All local functions start on the default CPU target.
	numImports := int(ctx.Module.Imports.NumFuncs())
	cpuFuncs := make([]int, 0, ctx.Module.Functions.Len())
	for i := numImports; i < ctx.Module.Functions.Len()+numImports; i++ {
		cpuFuncs = append(cpuFuncs, i)
	}
	routes[defaultArch] = cpuFuncs

	// 2. Parse import signatures: populate ptr/hptr masks and detect memory.
	funcIdx := 0
	for _, imp := range ctx.Module.Imports.Entries {
		if imp.Kind != wasm.ImportFunc {
			continue
		}

		route := abi.Parse(imp.Module)
		if route.Kind == abi.VertexMemory {
			ctx.NeedsMemory = true
		}

		sig := abi.ParseSig(imp.Name)
		if sig.PtrMask != nil {
			ctx.ImportPtrMasks[funcIdx] = sig.PtrMask
		}
		if sig.HptrMask != nil {
			ctx.ImportHptrMasks[funcIdx] = sig.HptrMask
		}
		if sig.RetHptr {
			ctx.ReturnHptrMasks[funcIdx] = true
		}

		funcIdx++
	}

	// 3. Parse export suffixes: map kernel params.
	for _, exp := range ctx.Module.Exports.Entries {
		if exp.Kind != wasm.ExportFunc {
			continue
		}

		export := abi.ParseExport(exp.Name)

		if export.Kind == abi.ExportCPU {
			continue // stays on the default CPU target
		}

		if export.Params != nil {
			ctx.KernelParams[int(exp.Idx)] = export.Params
		}
	}

	return routes, nil
}