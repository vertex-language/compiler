package driver

import (
	"strings"

	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/wasm"
)

// RoutingTable groups Wasm function indices by their destination backend ID.
type RoutingTable map[string][]int

// Analyze processes the ABI strings and populates the shared context.
func Analyze(ctx *context.BuildContext, defaultArch string) (RoutingTable, error) {
	routes := make(RoutingTable)

	// 1. Initial State: Assign all local functions to the default CPU target.
	var cpuFuncs []int
	numImports := int(ctx.Module.Imports.NumFuncs())
	for i := numImports; i < ctx.Module.Functions.Len()+numImports; i++ {
		cpuFuncs = append(cpuFuncs, i)
	}
	routes[defaultArch] = cpuFuncs

	// 2. Parse Import Signatures for Pointer Masks, Handles, and Memory
	funcIdx := 0
	for _, imp := range ctx.Module.Imports.Entries {
		if imp.Kind != wasm.ImportFunc {
			continue
		}

		// Detect memory imports
		if imp.Module == "memory" {
			ctx.NeedsMemory = true
		}

		parts := strings.Split(imp.Name, "@")
		if len(parts) == 2 {
			sigParts := strings.Split(parts[1], ":")
			
			// Parse parameters
			if sigParts[0] != "" {
				types := strings.Split(sigParts[0], ".")
				pMask := make([]bool, len(types))
				hMask := make([]bool, len(types))
				for i, t := range types {
					if t == "ptr" {
						pMask[i] = true
					} else if t == "hptr" {
						hMask[i] = true
					}
				}
				ctx.ImportPtrMasks[funcIdx] = pMask
				ctx.ImportHptrMasks[funcIdx] = hMask
			}

			// Parse returns
			if len(sigParts) == 2 && sigParts[1] == "hptr" {
				ctx.ReturnHptrMasks[funcIdx] = true
			}
		}
		funcIdx++
	}

	// 3. Parse Export Signatures for GPU Routing and Concurrency Detection
	for _, exp := range ctx.Module.Exports.Entries {
		if exp.Kind != wasm.ExportFunc {
			continue
		}

		parts := strings.Split(exp.Name, "@")
		if len(parts) != 2 {
			continue // Standard export, stays on default CPU
		}

		targetAndTypes := strings.Split(parts[1], ":")
		target := targetAndTypes[0]

		if len(targetAndTypes) == 2 {
			ctx.KernelParams[int(exp.Idx)] = strings.Split(targetAndTypes[1], ".")
		}

		// Detect concurrency models or route to GPU
		switch target {
		case "async":
			ctx.NeedsAsync = true
			ctx.ConcurrentFuncs[int(exp.Idx)] = "async"
			// Stays in the default CPU bucket (compiled under __vertex_fn_X)
		case "thread":
			ctx.NeedsThread = true
			ctx.ConcurrentFuncs[int(exp.Idx)] = "thread"
			// Stays in the default CPU bucket
		case "process":
			ctx.NeedsProcess = true
			ctx.ConcurrentFuncs[int(exp.Idx)] = "process"
			// Stays in the default CPU bucket
		default:
			// Route to GPU: Move function out of CPU bucket and into target bucket
			routes[target] = append(routes[target], int(exp.Idx))
			routes[defaultArch] = remove(routes[defaultArch], int(exp.Idx))
		}
	}

	// Note: Name custom section parsing for internal kernels would go here,
	// following the exact same logic as the exports above.

	return routes, nil
}

// remove is a helper to filter out an element from a slice.
func remove(s []int, val int) []int {
	var res []int
	for _, v := range s {
		if v != val {
			res = append(res, v)
		}
	}
	return res
}