// compiler/compiler.go
package compiler

import (
	"fmt"
	"runtime"

	"github.com/vertex-language/compiler/context"
	"github.com/vertex-language/compiler/cpu/arm64"
	"github.com/vertex-language/compiler/cpu/amd64"
	"github.com/vertex-language/compiler/driver"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Options controls code generation behaviour.
type Options struct {
	// QualifiedSymbols includes the wasm import module name in every linker
	// symbol, producing "linux/libc::printf" instead of "printf".
	QualifiedSymbols bool
}

// Result is returned by the CompileFull* family of functions.
// It carries everything the linker needs: the native object, the build context
// (which holds resolved system library entries, pointer masks, etc.), and the
// target axes that were used for compilation.
//
// Pass a Result to linker.LinkResult to link without re-running the compiler.
type Result struct {
	// Object is the native relocatable object (ELF64 ET_REL, COFF .obj,
	// or Mach-O MH_OBJECT depending on the target platform).
	Object object.Object

	// Ctx is the build context populated during compilation. It holds the
	// resolved system library table (SystemLibs), pointer/handle masks, and
	// other metadata the linker uses to wire up dependencies automatically.
	Ctx *context.BuildContext

	// Arch is the target architecture used for this compilation.
	Arch object.Arch

	// Platform is the target operating system / binary format used for this
	// compilation.
	Platform object.Platform
}

// ────────────────────────────────────────────────────────────────────────────
// Original API — returns a bare object.Object for callers that manage their
// own linker setup. Unchanged from v0.
// ────────────────────────────────────────────────────────────────────────────

// Compile translates m into a native object for the current host
// architecture and OS.
func Compile(m *wasm.Module) (object.Object, error) {
	return CompileWith(m, Options{})
}

// CompileWith translates m into a native object for the current host
// architecture and OS using the supplied options.
func CompileWith(m *wasm.Module, opts Options) (object.Object, error) {
	return CompileFor(m, runtime.GOARCH, runtime.GOOS, opts)
}

// CompileFor translates m into a native object for the given GOARCH and GOOS
// strings. This is the primary entry point for cross-compilation and tests
// when the caller manages its own linker.
func CompileFor(m *wasm.Module, goarch, goos string, opts Options) (object.Object, error) {
	r, err := CompileFullFor(m, goarch, goos, opts)
	if err != nil {
		return nil, err
	}
	return r.Object, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Full API — returns a Result that includes the BuildContext so the caller
// (typically linker.Build / linker.LinkResult) can resolve system libraries
// without re-running analysis.
// ────────────────────────────────────────────────────────────────────────────

// CompileFull translates m into a Result for the current host architecture
// and OS.
func CompileFull(m *wasm.Module) (Result, error) {
	return CompileFullWith(m, Options{})
}

// CompileFullWith translates m into a Result for the current host architecture
// and OS using the supplied options.
func CompileFullWith(m *wasm.Module, opts Options) (Result, error) {
	return CompileFullFor(m, runtime.GOARCH, runtime.GOOS, opts)
}

// CompileFullFor translates m into a Result for the given GOARCH and GOOS
// strings. Used for cross-compilation and testing.
//
// This is the function linker.Build calls internally. Callers that need the
// BuildContext (e.g. to inspect SystemLibs or ImportPtrMasks before linking)
// should call this directly and pass the Result to linker.LinkResult.
func CompileFullFor(m *wasm.Module, goarch, goos string, opts Options) (Result, error) {
	arch, err := goarchToArch(goarch)
	if err != nil {
		return Result{}, err
	}
	plat, err := goosToPlatform(goos)
	if err != nil {
		return Result{}, err
	}
	return compileFullFor(m, arch, plat, opts)
}

// ────────────────────────────────────────────────────────────────────────────
// Internal implementation
// ────────────────────────────────────────────────────────────────────────────

// compileFullFor is the internal implementation shared by all public entry
// points. It constructs and registers the appropriate backend target, then
// delegates to driver.CompileFull which returns both the object and the
// populated BuildContext.
func compileFullFor(m *wasm.Module, arch object.Arch, plat object.Platform, opts Options) (Result, error) {
	drv := driver.New()

	switch arch {
	case object.AMD64:
		drv.Register(&amd64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	case object.ARM64:
		drv.Register(&arm64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	default:
		return Result{}, fmt.Errorf("compiler: no backend registered for %s", arch)
	}

	// driver.CompileFull mirrors driver.Compile but additionally returns the
	// BuildContext it populates during the Analyze and Emit phases.
	// The one-line change in driver.go: split the existing Compile method into
	// Compile (calls CompileFull, discards ctx) and CompileFull (returns both).
	obj, ctx, err := drv.CompileFull(m, arch, plat)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Object:   obj,
		Ctx:      ctx,
		Arch:     arch,
		Platform: plat,
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Conversion helpers
// ────────────────────────────────────────────────────────────────────────────

func goarchToArch(goarch string) (object.Arch, error) {
	switch goarch {
	case "amd64":
		return object.AMD64, nil
	case "arm64":
		return object.ARM64, nil
	default:
		return 0, fmt.Errorf("compiler: unsupported architecture %q", goarch)
	}
}

func goosToPlatform(goos string) (object.Platform, error) {
	switch goos {
	case "linux":
		return object.Linux, nil
	case "darwin":
		return object.Darwin, nil
	case "windows":
		return object.Windows, nil
	case "freebsd":
		return object.FreeBSD, nil
	default:
		return 0, fmt.Errorf("compiler: unsupported OS %q", goos)
	}
}