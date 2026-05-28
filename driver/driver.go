// driver/driver.go
package driver

import (
	"fmt"

	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"

	amd64mem "github.com/vertex-language/compiler/cpu/amd64/memory"
	arm64mem "github.com/vertex-language/compiler/cpu/arm64/memory"
)

// Driver orchestrates the compiler pipeline.
type Driver struct {
	targets map[string]Target
}

// New initialises a new compilation driver.
func New() *Driver {
	return &Driver{targets: make(map[string]Target)}
}

// Register adds a backend Target to the driver. The target is keyed by the
// string returned from its ID() method. Registering a second target with the
// same ID replaces the first.
func (d *Driver) Register(t Target) {
	d.targets[t.ID()] = t
}

// Compile executes the full compilation pipeline and returns the assembled
// object. Call Emit() on the result to obtain the native byte representation
// (ELF64, COFF .obj, or Mach-O MH_OBJECT).
func (d *Driver) Compile(m *wasm.Module, arch object.Arch, platform object.Platform) (object.Object, error) {
	obj := object.New(arch, platform)
	ctx := context.NewBuildContext(m, obj)

	defaultArch := archID(arch)

	routes, err := Analyze(ctx, defaultArch, arch, platform)
	if err != nil {
		return nil, err
	}

	switch arch {
	case object.AMD64:
		err = amd64mem.Emit(ctx)
	case object.ARM64:
		err = arm64mem.Emit(ctx)
	default:
		return nil, fmt.Errorf("driver: memory allocator not yet ported to %s", arch)
	}

	if err != nil {
		return nil, fmt.Errorf("driver: failed to emit memory stubs: %w", err)
	}

	for targetID, funcs := range routes {
		if len(funcs) == 0 {
			continue
		}
		t, ok := d.targets[targetID]
		if !ok {
			return nil, fmt.Errorf("driver: unsupported target backend %q", targetID)
		}
		if err := t.Emit(ctx, funcs); err != nil {
			return nil, fmt.Errorf("driver: %s compilation failed: %w", targetID, err)
		}
	}

	return ctx.Obj, nil
}

// CompileFull mirrors Compile but additionally returns the populated
// BuildContext so callers (e.g. linker.LinkResult) can inspect resolved
// system libraries and pointer masks without re-running analysis.
func (d *Driver) CompileFull(m *wasm.Module, arch object.Arch, platform object.Platform) (object.Object, *context.BuildContext, error) {
	obj := object.New(arch, platform)
	ctx := context.NewBuildContext(m, obj)

	defaultArch := archID(arch)

	routes, err := Analyze(ctx, defaultArch, arch, platform)
	if err != nil {
		return nil, nil, err
	}

	switch arch {
	case object.AMD64:
		err = amd64mem.Emit(ctx)
	case object.ARM64:
		err = arm64mem.Emit(ctx)
	default:
		return nil, nil, fmt.Errorf("driver: memory allocator not yet ported to %s", arch)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("driver: failed to emit memory stubs: %w", err)
	}

	for targetID, funcs := range routes {
		if len(funcs) == 0 {
			continue
		}
		t, ok := d.targets[targetID]
		if !ok {
			return nil, nil, fmt.Errorf("driver: unsupported target backend %q", targetID)
		}
		if err := t.Emit(ctx, funcs); err != nil {
			return nil, nil, fmt.Errorf("driver: %s compilation failed: %w", targetID, err)
		}
	}

	return ctx.Obj, ctx, nil
}

// archID maps an object.Arch to the string routing key used in RoutingTable.
func archID(a object.Arch) string {
	switch a {
	case object.AMD64:
		return "amd64"
	case object.ARM64:
		return "arm64"
	default:
		return a.String()
	}
}