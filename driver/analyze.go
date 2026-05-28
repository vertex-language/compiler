// driver/analyze.go
package driver

import (
	"github.com/vertex-language/compiler/abi"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// RoutingTable groups wasm function indices by their destination backend ID.
type RoutingTable map[string][]int

// Analyze processes all import and export ABI strings, populates the shared
// BuildContext with pointer/handle masks and resolved system library entries,
// and returns a RoutingTable that maps each function to its backend.
//
// arch is the target object.Arch used for system library resolution.
// platform is the target object.Platform, also used for resolution and for
// validating that platform-specific imports (e.g. WindowsSystemLib on Linux)
// are caught early with a clear error.
func Analyze(ctx *context.BuildContext, defaultArch string, arch object.Arch, platform object.Platform) (RoutingTable, error) {
	routes := make(RoutingTable)

	// 1. All local functions start on the default CPU target.
	numImports := int(ctx.Module.Imports.NumFuncs())
	cpuFuncs := make([]int, 0, ctx.Module.Functions.Len())
	for i := numImports; i < ctx.Module.Functions.Len()+numImports; i++ {
		cpuFuncs = append(cpuFuncs, i)
	}
	routes[defaultArch] = cpuFuncs

	// 2. Parse import signatures: populate ptr/hptr masks, detect memory,
	//    and resolve system library entries for IsSystemLib() imports.
	funcIdx := 0
	for _, imp := range ctx.Module.Imports.Entries {
		if imp.Kind != wasm.ImportFunc {
			continue
		}

		route := abi.Parse(imp.Module)

		switch {
		case route.Kind == abi.VertexMemory:
			ctx.NeedsMemory = true

		case route.Kind.IsSystemLib():
			result, err := abi.ResolveSystemLib(route, arch, platform)
			if err != nil {
				return nil, err
			}
			ctx.SystemLibs[funcIdx] = result

		case abi.IsUnrecognised(route):
			return nil, &abi.ErrUnrecognisedImport{
				FuncIdx: funcIdx,
				Module:  imp.Module,
				Name:    imp.Name,
			}
		}

		// Signature mask parsing applies to all import kinds.
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

	// 3. Parse export suffixes: map kernel params and non-CPU routing.
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