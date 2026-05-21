package platform

import "strings"

// RouteKind is what the linker should emit for a given import.
type RouteKind int

const (
	// SyscallTrampoline: first slot contains "/syscalls".
	// Linker emits: mov rax, <number> / syscall / ret
	SyscallTrampoline RouteKind = iota

	// PlatformLib: first slot has the form "platform:lib".
	// Linker emits an IAT entry (Windows), LC_LOAD_DYLIB stub (macOS),
	// or .so PLT entry (Linux) for the named lib on the named platform.
	PlatformLib

	// CrossPlatformLib: bare lib name, no platform prefix.
	// Linker prepends "lib" on Linux/macOS; uses name as-is on Windows.
	CrossPlatformLib
)

// Route is the parsed result of a wasm import module field (first slot).
type Route struct {
	Kind     RouteKind
	Platform string // "linux", "windows", "darwin" — empty for CrossPlatformLib
	Lib      string // "kernel/syscalls", "kernel32", "libSystem", "c", "sdl2", …
}

// Parse parses the first slot of a wasm import section into a Route.
//
//	"linux:kernel/syscalls" → {SyscallTrampoline, "linux",   "kernel/syscalls"}
//	"windows:kernel32"      → {PlatformLib,       "windows", "kernel32"}
//	"darwin:libSystem"      → {PlatformLib,       "darwin",  "libSystem"}
//	"c"                     → {CrossPlatformLib,  "",        "c"}
//	"sdl2"                  → {CrossPlatformLib,  "",        "sdl2"}
//	"gtk-4"                 → {CrossPlatformLib,  "",        "gtk-4"}
func Parse(module string) Route {
	plat, lib, hasPlatform := strings.Cut(module, ":")
	if !hasPlatform {
		return Route{Kind: CrossPlatformLib, Lib: module}
	}
	if strings.Contains(lib, "/syscalls") {
		return Route{Kind: SyscallTrampoline, Platform: plat, Lib: lib}
	}
	return Route{Kind: PlatformLib, Platform: plat, Lib: lib}
}

// LibName returns the native library filename for a Route on the given OS.
// os is "linux", "windows", or "darwin".
//
// For CrossPlatformLib and PlatformLib the name follows platform conventions:
//   - Linux/macOS: prepend "lib" if not already present → "libc.so", "libsdl2.so"
//   - Windows:     use as-is → "kernel32.dll", "d3d12.dll"
func LibName(r Route, os string) string {
	name := r.Lib
	switch os {
	case "linux", "darwin":
		if !strings.HasPrefix(name, "lib") {
			name = "lib" + name
		}
	}
	return name
}