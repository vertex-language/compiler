package darwin

import "fmt"

// Arch is a GOARCH string understood by this package.
// Only "amd64" and "arm64" are supported.
type Arch string

const (
	AMD64 Arch = "amd64"
	ARM64 Arch = "arm64"
)

// Entry describes a single known Darwin system library.
type Entry struct {
	// InstallName is the canonical LC_LOAD_DYLIB path written into the
	// Mach-O binary. This is what dyld uses to resolve the library from
	// its shared cache at runtime. Since macOS 11 these files typically
	// do NOT exist on disk at this path — do not stat-check them.
	InstallName string

	// SDKStub is the relative path within the Xcode SDK to the .tbd stub
	// used for link-time symbol resolution, e.g. "usr/lib/libz.tbd".
	// Empty for entries that are not in the SDK (e.g. third-party).
	SDKStub string

	// WeakLink marks libraries that may not be present on older OS versions
	// and should be emitted as LC_LOAD_WEAK_DYLIB instead of LC_LOAD_DYLIB.
	WeakLink bool
}

// FrameworkEntry describes a system framework resolvable via LC_LOAD_DYLIB.
type FrameworkEntry struct {
	// InstallName is the full LC_LOAD_DYLIB path for the framework.
	InstallName string

	// SDKStub is the relative path to the .tbd stub in the Xcode SDK.
	SDKStub string
}

// Resolve returns the Entry for a named Darwin system dylib and true.
// name is the sub-path segment from the wasm import module, e.g.:
//
//	"darwin/libSystem"  → name = "libSystem"
//	"darwin/libz"       → name = "libz"
//	"darwin/libc++"     → name = "libc++"
//
// If the name is not a known system library it returns a zero Entry and false.
// Arch is accepted for API consistency with abi/linux but Darwin install names
// are architecture-neutral — the same dylib is a universal binary in the cache.
func Resolve(name string, _ Arch) (Entry, bool) {
	e, ok := registry[name]
	return e, ok
}

// ResolveFramework returns the FrameworkEntry for a named system framework.
// name should be the bare framework name without ".framework", e.g.
// "CoreFoundation", "Security", "IOKit".
func ResolveFramework(name string) (FrameworkEntry, bool) {
	e, ok := frameworks[name]
	return e, ok
}

// KnownLibs returns all library names recognised by this package.
func KnownLibs() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}

// KnownFrameworks returns all framework names recognised by this package.
func KnownFrameworks() []string {
	names := make([]string, 0, len(frameworks))
	for k := range frameworks {
		names = append(names, k)
	}
	return names
}

// ErrUnknown is returned for unrecognised library names.
type ErrUnknown struct {
	Name string
}

func (e *ErrUnknown) Error() string {
	return fmt.Sprintf(
		"darwin: unknown system library %q — not in the DarwinSystemLib table",
		e.Name,
	)
}

// registry is keyed by the bare library name (with or without "lib" prefix —
// both "libz" and "z" are valid keys). Populated by init() in each sub-file.
var registry = map[string]Entry{}

// frameworks is keyed by bare framework name. Populated by init() in frameworks.go.
var frameworks = map[string]FrameworkEntry{}

// registerLib adds an entry, accepting both the full name ("libz") and the
// bare name ("z") as keys so callers don't need to remember the prefix.
func registerLib(name string, e Entry) {
	registry[name] = e
	// If name starts with "lib", also register without the prefix.
	if len(name) > 3 && name[:3] == "lib" {
		registry[name[3:]] = e
	}
}

// registerFramework adds a framework entry keyed by bare name.
func registerFramework(name string, e FrameworkEntry) {
	frameworks[name] = e
}