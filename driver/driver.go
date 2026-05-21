package driver

import (
	"fmt"

	"github.com/vertex-language/compiler/concurrency"
	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/memory"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Driver orchestrates the compiler pipeline.
type Driver struct {
	targets map[string]Target
}

// New initializes a new compilation driver.
func New() *Driver {
	return &Driver{
		targets: make(map[string]Target),
	}
}

// Register adds a code generation target to the driver.
func (d *Driver) Register(t Target) {
	d.targets[t.ID()] = t
}

// Compile executes the compilation pipeline for the given Wasm module.
func (d *Driver) Compile(m *wasm.Module, defaultArch string) (*object.WasmObj, error) {
	ctx := context.NewBuildContext(m)

	// 1. Discover hints and build the routing table
	routes, err := Analyze(ctx, defaultArch)
	if err != nil {
		return nil, err
	}

	// 2. Inject platform stubs (Memory and Concurrency)
	if ctx.NeedsMemory {
		if defaultArch != "amd64" {
			return nil, fmt.Errorf("driver: memory allocator not yet ported to %s", defaultArch)
		}
		if err := memory.Emit(ctx); err != nil {
			return nil, fmt.Errorf("driver: failed to emit memory stubs: %w", err)
		}
	}

	if ctx.NeedsAsync || ctx.NeedsThread || ctx.NeedsProcess {
		if defaultArch != "amd64" {
			return nil, fmt.Errorf("driver: concurrency not yet ported to %s", defaultArch)
		}
		if err := concurrency.Emit(ctx); err != nil {
			return nil, fmt.Errorf("driver: failed to emit concurrency stubs: %w", err)
		}
	}

	// 3. Dispatch to the registered targets
	for targetID, funcs := range routes {
		if len(funcs) == 0 {
			continue // Skip idle targets
		}

		target, exists := d.targets[targetID]
		if !exists {
			return nil, fmt.Errorf("driver: unsupported target backend %q", targetID)
		}

		// Targets write directly to ctx.Obj
		if err := target.Emit(ctx, funcs); err != nil {
			return nil, fmt.Errorf("driver: %s compilation failed: %w", targetID, err)
		}
	}

	return ctx.Obj, nil
}