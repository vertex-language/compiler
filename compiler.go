// compiler/compiler.go
package compiler

import (
	"fmt"
	"runtime"

	"github.com/vertex-language/compiler/cpu/arm64"
	"github.com/vertex-language/compiler/cpu/amd64"
	"github.com/vertex-language/compiler/driver"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// Options controls code generation behaviour.
type Options struct {
	// QualifiedSymbols includes the wasm import module name in every linker
	// symbol, producing "c::malloc" instead of "malloc".
	QualifiedSymbols bool
}

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
// strings (e.g. "amd64", "linux").  This is the primary entry point for
// cross-compilation and tests.
func CompileFor(m *wasm.Module, goarch, goos string, opts Options) (object.Object, error) {
	arch, err := goarchToArch(goarch)
	if err != nil {
		return nil, err
	}
	plat, err := goosToPlatform(goos)
	if err != nil {
		return nil, err
	}
	return compileFor(m, arch, plat, opts)
}

// compileFor is the internal implementation that works with typed object.Arch
// and object.Platform values.
func compileFor(m *wasm.Module, arch object.Arch, plat object.Platform, opts Options) (object.Object, error) {
	drv := driver.New()

	switch arch {
	case object.AMD64:
		drv.Register(&amd64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	case object.ARM64:
		drv.Register(&arm64.Target{QualifiedSymbols: opts.QualifiedSymbols})
	default:
		return nil, fmt.Errorf("compiler: no backend registered for %s", arch)
	}

	return drv.Compile(m, arch, plat)
}

// goarchToArch converts a GOARCH string to an object.Arch.
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

// goosToPlatform converts a GOOS string to an object.Platform.
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