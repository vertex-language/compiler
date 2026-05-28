package windows

import "fmt"

// Arch is a GOARCH string understood by this package.
// Only "amd64" and "arm64" are supported.
type Arch string

const (
	AMD64 Arch = "amd64"
	ARM64 Arch = "arm64"
)

// Entry describes a single known Windows system library.
type Entry struct {
	// ImportLib is the .lib filename passed to the PE linker, e.g. "kernel32.lib".
	// The driver resolves the full SDK path; this is just the filename.
	ImportLib string

	// DLLName is the DLL filename written into the PE IAT, e.g. "KERNEL32.dll".
	// Windows loader searches for this name at runtime using the standard
	// DLL search order (System32 → SysWOW64 → app dir → PATH).
	DLLName string

	// MinGWLib is the MinGW/LLVM import library filename used when
	// cross-compiling from Linux or macOS, e.g. "libkernel32.a".
	// Empty if the library has no MinGW equivalent.
	MinGWLib string

	// SystemOnly marks DLLs that are not redistributable and must exist
	// on the target system (e.g. ntdll.dll). The driver should never
	// copy these into the app bundle.
	SystemOnly bool

	// MinVersion is the minimum Windows version required, using the
	// _WIN32_WINNT constant format (e.g. 0x0A00 for Windows 10).
	// Zero means available on all supported Windows versions (Vista+).
	MinVersion uint32
}

// Resolve returns the Entry for a named Windows system library and true.
// name is the sub-path segment from the wasm import module, e.g.:
//
//	"windows/kernel32"  → name = "kernel32"
//	"windows/ws2_32"    → name = "ws2_32"
//	"windows/ucrt"      → name = "ucrt"
//
// The lookup is case-insensitive: "Kernel32", "kernel32", and "KERNEL32"
// all resolve to the same entry.
// Arch is accepted for API consistency but Windows import lib names are
// architecture-neutral — the same filenames exist under um\x64\ and um\arm64\.
func Resolve(name string, _ Arch) (Entry, bool) {
	e, ok := registry[normalizeName(name)]
	return e, ok
}

// KnownNames returns all library names recognised by this package.
func KnownNames() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
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
		"windows: unknown system library %q — not in the WindowsSystemLib table",
		e.Name,
	)
}

// registry is keyed by lowercase library name. Populated by init() in each file.
var registry = map[string]Entry{}

// register adds an entry under the lowercase form of name.
func register(name string, e Entry) {
	registry[normalizeName(name)] = e
}

// normalizeName lowercases name and strips any trailing ".lib" or ".dll".
func normalizeName(name string) string {
	n := name
	// lowercase
	b := []byte(n)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	n = string(b)
	// strip suffix
	for _, sfx := range []string{".lib", ".dll", ".a"} {
		if len(n) > len(sfx) && n[len(n)-len(sfx):] == sfx {
			n = n[:len(n)-len(sfx)]
		}
	}
	return n
}