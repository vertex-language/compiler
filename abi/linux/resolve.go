package linux

import (
	"fmt"
	"strings"
)

// Arch is a GOARCH string understood by this package.
// Only "amd64" and "arm64" are supported.
type Arch string

const (
	AMD64 Arch = "amd64"
	ARM64 Arch = "arm64"
)

// Entry describes a single known Linux system library.
type Entry struct {
	// Soname is the canonical DT_SONAME string embedded in the .so file,
	// e.g. "libc.so.6". This is what goes into the ELF DT_NEEDED field.
	Soname string

	// Candidates is the ordered list of filesystem paths to probe.
	// The driver walks this list and uses the first path that exists.
	// Paths are specific to the Arch passed to Resolve.
	Candidates []string
}

// Resolve returns the Entry for a named Linux system library on the given
// architecture, and true. If the name is not a known system library it
// returns a zero Entry and false.
//
// name is the sub-path segment from the wasm import module, e.g.:
//
//	"linux/libc"       → name = "libc"
//	"linux/libm"       → name = "libm"
//	"linux/libpthread" → name = "libpthread"
func Resolve(name string, arch Arch) (Entry, bool) {
	// Strip a redundant "lib" prefix if the caller included it.
	// "libc" and "c" both resolve to the same entry.
	key := strings.TrimPrefix(name, "lib")

	table, ok := registry[arch]
	if !ok {
		return Entry{}, false
	}
	e, ok := table[key]
	return e, ok
}

// KnownNames returns all library names recognised for the given arch,
// without the "lib" prefix. Useful for error messages and documentation.
func KnownNames(arch Arch) []string {
	table, ok := registry[arch]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(table))
	for k := range table {
		names = append(names, k)
	}
	return names
}

// ErrUnknown is returned by MustResolve for unknown library names.
type ErrUnknown struct {
	Name string
	Arch Arch
}

func (e *ErrUnknown) Error() string {
	return fmt.Sprintf(
		"linux: unknown system library %q for %s — not in the LinuxSystemLib table",
		e.Name, e.Arch,
	)
}

// registry is the top-level map keyed by Arch, then by bare library name
// (no "lib" prefix). Populated by the init() calls in the other files.
var registry = map[Arch]map[string]Entry{
	AMD64: {},
	ARM64: {},
}

// register adds an entry to both arch tables, using arch-specific candidate
// lists. Called from init() in each sub-file.
func register(name string, amd64Entry, arm64Entry Entry) {
	registry[AMD64][name] = amd64Entry
	registry[ARM64][name] = arm64Entry
}