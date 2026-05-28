// Package linker is the single-call entry point for compiling a wasm.Module
// into a native binary. It wraps the compiler pipeline and all three platform
// linkers (ELF, Mach-O, PE32+), automatically resolving every system library
// and framework declared via ABI import paths in the module.
//
// Typical usage:
//
//	m := wasm.NewModule()
//	// ... build module with abi import paths ...
//	bin, err := linker.Build(m, linker.Options{})
//	os.WriteFile("out", bin, 0o755)
//
// The frontend is responsible only for function signatures (the @-suffix ABI
// syntax). Everything else — shared library resolution, DT_NEEDED / LC_LOAD_DYLIB
// / IAT wiring, interpreter path, deduplication — is handled here.
package linker

import (
	"fmt"
	"os"
	"runtime"

	"github.com/vertex-language/compiler"
	"github.com/vertex-language/compiler/abi"
	"github.com/vertex-language/compiler/abi/darwin"
	"github.com/vertex-language/compiler/abi/linux"
	"github.com/vertex-language/compiler/abi/windows"
	elflink "github.com/vertex-language/compiler/linker/elf"
	macholink "github.com/vertex-language/compiler/linker/macho"
	pelink "github.com/vertex-language/compiler/linker/pe"
	"github.com/vertex-language/compiler/object"
	"github.com/vertex-language/compiler/wasm"
)

// ────────────────────────────────────────────────────────────────────────────
// Public types
// ────────────────────────────────────────────────────────────────────────────

// OutputType selects the kind of binary produced.
type OutputType int

const (
	// OutputExec produces a position-dependent native executable (default).
	OutputExec OutputType = iota

	// OutputPIE produces a position-independent executable (Linux only).
	OutputPIE

	// OutputShared produces a shared library / dylib / DLL.
	OutputShared
)

// Options drives the compiler and the linker.
// Most fields have safe defaults; a zero-value Options works for simple
// Linux executables compiled on a Linux host.
type Options struct {
	// GoArch is the target architecture as a GOARCH string ("amd64", "arm64").
	// Defaults to runtime.GOARCH.
	GoArch string

	// GoOS is the target operating system as a GOOS string
	// ("linux", "darwin", "windows", "freebsd").
	// Defaults to runtime.GOOS.
	GoOS string

	// Entry is the entry-point symbol name for executables.
	// Defaults to "_start" for ELF/Mach-O and "mainCRTStartup" for PE.
	// Set to "" when OutputType is OutputShared.
	Entry string

	// OutputType selects the binary output kind. Defaults to OutputExec.
	OutputType OutputType

	// QualifiedSymbols passes through to compiler.Options.
	// When true, linker symbols include the wasm import module path
	// (e.g. "linux/libc::printf" instead of "printf").
	QualifiedSymbols bool

	// ELFInterp overrides the ELF PT_INTERP dynamic linker path.
	// Relevant only for Linux executables that import system libraries.
	// When empty a sensible default is chosen for the target architecture:
	//   amd64 → /lib64/ld-linux-x86-64.so.2
	//   arm64 → /lib/ld-linux-aarch64.so.1
	ELFInterp string

	// SDKLibDir is the directory containing Windows SDK import libraries
	// (.lib / MinGW .a files). Required when cross-linking against windows/*
	// imports from a non-Windows host.
	//
	// Example (MSVC layout):
	//   C:\Program Files (x86)\Windows Kits\10\Lib\10.0.22621.0\um\x64
	//
	// Example (MinGW cross layout on Linux):
	//   /usr/x86_64-w64-mingw32/lib
	SDKLibDir string

	// DarwinSDKStubDir is the directory containing Xcode SDK .tbd stubs,
	// used when cross-linking against darwin/* imports from a non-Darwin host.
	//
	// Example:
	//   /path/to/MacOSX.sdk/usr/lib
	//
	// On a native Darwin host this is optional; the linker falls back to
	// /usr/lib when the stub directory is empty.
	DarwinSDKStubDir string
}

// ────────────────────────────────────────────────────────────────────────────
// Entry points
// ────────────────────────────────────────────────────────────────────────────

// Build compiles m and links it into a native binary in a single call.
// System libraries and frameworks declared via ABI import paths are resolved
// and wired automatically — the frontend only needs to supply correct function
// signatures using the @-suffix syntax.
func Build(m *wasm.Module, opts Options) ([]byte, error) {
	opts = applyDefaults(opts)

	result, err := compiler.CompileFullFor(m, opts.GoArch, opts.GoOS, compiler.Options{
		QualifiedSymbols: opts.QualifiedSymbols,
	})
	if err != nil {
		return nil, fmt.Errorf("linker: compile: %w", err)
	}

	return LinkResult(result, opts)
}

// LinkResult links an already-compiled compiler.Result into a native binary.
// Use this when you need to inspect or post-process the object before linking,
// or when you want to separate compile and link phases explicitly.
func LinkResult(result compiler.Result, opts Options) ([]byte, error) {
	opts = applyDefaults(opts)

	objBytes, err := result.Object.Emit()
	if err != nil {
		return nil, fmt.Errorf("linker: emit object: %w", err)
	}

	switch result.Platform {
	case object.Linux, object.FreeBSD:
		return linkELF(objBytes, result, opts)
	case object.Darwin:
		return linkMachO(objBytes, result, opts)
	case object.Windows:
		return linkPE(objBytes, result, opts)
	default:
		return nil, fmt.Errorf("linker: unsupported platform %s", result.Platform)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ELF (Linux / FreeBSD)
// ────────────────────────────────────────────────────────────────────────────

func linkELF(objBytes []byte, result compiler.Result, opts Options) ([]byte, error) {
	parsedObj, err := elflink.ParseObject(objBytes)
	if err != nil {
		return nil, fmt.Errorf("linker/elf: parse object: %w", err)
	}

	machine := elflink.EM_X86_64
	if result.Arch == object.ARM64 {
		machine = elflink.EM_AARCH64
	}

	lnk := elflink.NewLinker(machine)

	switch opts.OutputType {
	case OutputPIE:
		lnk.SetOutputType(elflink.OutputPIE)
	case OutputShared:
		lnk.SetOutputType(elflink.OutputShared)
	default:
		lnk.SetOutputType(elflink.OutputExec)
	}

	if opts.OutputType != OutputShared {
		lnk.SetEntry(opts.Entry)
	}

	// Collect and deduplicate Linux system library entries from the build
	// context, then add each resolved .so as a shared dependency.
	if libs := collectLinuxLibs(result); len(libs) > 0 {
		// Only emit PT_INTERP when we actually have dynamic dependencies.
		lnk.SetInterp(opts.ELFInterp)

		for _, e := range libs {
			shared, sharedErr := resolveELFShared(e, result.Arch)
			if sharedErr != nil {
				// The .so was not found on the host (cross-compile scenario).
				// Fall back to a DT_NEEDED-only entry — the target system's
				// dynamic linker will resolve it at runtime.
				lnk.AddSONeeded(e.Soname)
				continue
			}
			lnk.AddShared(shared)
		}
	}

	lnk.AddObject(parsedObj)

	linkResult, err := lnk.Link()
	if err != nil {
		return nil, fmt.Errorf("linker/elf: link: %w", err)
	}

	bin, err := linkResult.Builder().Emit()
	if err != nil {
		return nil, fmt.Errorf("linker/elf: emit: %w", err)
	}

	return bin, nil
}

// collectLinuxLibs returns a deduplicated slice of linux.Entry values
// referenced by the module, preserving the order they were first seen.
func collectLinuxLibs(result compiler.Result) []linux.Entry {
	seen := make(map[string]struct{})
	var out []linux.Entry

	for _, syslib := range result.Ctx.SystemLibs {
		if syslib.Linux == nil {
			continue
		}
		e := *syslib.Linux
		if _, ok := seen[e.Soname]; ok {
			continue
		}
		seen[e.Soname] = struct{}{}
		out = append(out, e)
	}

	return out
}

// resolveELFShared stat-walks the candidate paths in e and returns the first
// parseable shared object it finds. Returns an error if none of the candidates
// exist — callers should fall back to AddSONeeded in that case.
func resolveELFShared(e linux.Entry, arch object.Arch) (*elflink.SharedLib, error) {
	_ = toLinuxArch(arch)

	for _, path := range e.Candidates {
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		shared, parseErr := elflink.OpenShared(path)
		if parseErr != nil {
			continue
		}
		return shared, nil
	}

	return nil, fmt.Errorf("shared object %q not found in candidate paths", e.Soname)
}

// defaultELFInterp returns the standard dynamic linker path for the target.
func defaultELFInterp(arch object.Arch) string {
	if arch == object.ARM64 {
		return "/lib/ld-linux-aarch64.so.1"
	}
	return "/lib64/ld-linux-x86-64.so.2"
}

// ────────────────────────────────────────────────────────────────────────────
// Mach-O (macOS)
// ────────────────────────────────────────────────────────────────────────────

func linkMachO(objBytes []byte, result compiler.Result, opts Options) ([]byte, error) {
	parsedObj, err := macholink.ParseObject(objBytes)
	if err != nil {
		return nil, fmt.Errorf("linker/macho: parse object: %w", err)
	}

	arch := macholink.ArchAMD64
	if result.Arch == object.ARM64 {
		arch = macholink.ArchARM64
	}

	lnk := macholink.NewLinker(arch)

	switch opts.OutputType {
	case OutputShared:
		lnk.SetOutputType(macholink.OutputDylib)
	default:
		lnk.SetOutputType(macholink.OutputExec)
		lnk.SetEntry(opts.Entry)
	}

	lnk.AddObject(parsedObj)

	// Wire up deduplicated Darwin system libraries and frameworks.
	// We resolve the .tbd stub path and hand it to the Mach-O linker;
	// dyld resolves the actual install name at runtime from the shared cache.
	for _, e := range collectDarwinLibs(result) {
		stubPath := darwinStubPath(e, opts.DarwinSDKStubDir)
		if stubPath != "" {
			dylib, dlErr := macholink.OpenDylib(stubPath)
			if dlErr == nil {
				lnk.AddDylib(dylib)
				continue
			}
			// Stub not parseable — fall through to install-name-only.
		}
		// No stub found (cross-compile or SDK not configured): emit an
		// LC_LOAD_DYLIB with the canonical install name. The binary will
		// resolve at runtime as long as the target macOS has the library.
		lnk.AddDylibByInstallName(e.InstallName)
	}

	linkResult, err := lnk.Link()
	if err != nil {
		return nil, fmt.Errorf("linker/macho: link: %w", err)
	}

	bin, err := linkResult.Builder().Emit()
	if err != nil {
		return nil, fmt.Errorf("linker/macho: emit: %w", err)
	}

	return bin, nil
}

// collectDarwinLibs returns a deduplicated slice of darwin.Entry values,
// keyed by install name to avoid emitting duplicate LC_LOAD_DYLIB commands.
func collectDarwinLibs(result compiler.Result) []darwin.Entry {
	seen := make(map[string]struct{})
	var out []darwin.Entry

	for _, syslib := range result.Ctx.SystemLibs {
		if syslib.Darwin == nil {
			continue
		}
		e := *syslib.Darwin
		if _, ok := seen[e.InstallName]; ok {
			continue
		}
		seen[e.InstallName] = struct{}{}
		out = append(out, e)
	}

	return out
}

// darwinStubPath resolves the .tbd stub file for e.
//
// Resolution order:
//  1. stubDir/e.SDKStub  (explicit SDK stub directory)
//  2. /usr/lib/e.SDKStub (native Darwin host fallback)
//
// Returns "" when neither location exists.
func darwinStubPath(e darwin.Entry, stubDir string) string {
	candidates := []string{}

	if stubDir != "" {
		candidates = append(candidates, stubDir+"/"+e.SDKStub)
	}
	// On a native Darwin host the SDK stubs live under Xcode, but many
	// system libraries also have .tbd files accessible via the command-line
	// tools. Try the install name path as a last resort.
	candidates = append(candidates, "/usr/lib/"+e.SDKStub)

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// PE32+ (Windows)
// ────────────────────────────────────────────────────────────────────────────

func linkPE(objBytes []byte, result compiler.Result, opts Options) ([]byte, error) {
	parsedObj, err := pelink.ParseObject(objBytes)
	if err != nil {
		return nil, fmt.Errorf("linker/pe: parse object: %w", err)
	}

	machine := pelink.MachineAMD64
	if result.Arch == object.ARM64 {
		machine = pelink.MachineARM64
	}

	lnk := pelink.NewLinker(machine)

	switch opts.OutputType {
	case OutputShared:
		lnk.SetDLL("out.dll") // caller can override by wrapping LinkResult
	default:
		lnk.SetEntry(opts.Entry)
	}

	lnk.AddObject(parsedObj)

	// Wire up deduplicated Windows import libraries.
	// Each windows.Entry carries the import lib filename (e.g. "kernel32.lib").
	// We locate it in SDKLibDir, or in the MinGW sysroot when cross-compiling.
	for _, e := range collectWindowsLibs(result) {
		libPath, libErr := resolveImportLib(e, opts.SDKLibDir, result.Arch)
		if libErr != nil {
			// Import lib not found — surface a clear error rather than
			// producing a binary that silently fails at load time.
			return nil, fmt.Errorf(
				"linker/pe: import library for %q (%s) not found: %w\n"+
					"  set Options.SDKLibDir to your Windows SDK Lib directory",
				e.DLLName, e.ImportLib, libErr,
			)
		}

		archive, archErr := pelink.OpenArchive(libPath)
		if archErr != nil {
			return nil, fmt.Errorf("linker/pe: open import lib %q: %w", libPath, archErr)
		}
		lnk.AddArchive(archive)
	}

	linkResult, err := lnk.Link()
	if err != nil {
		return nil, fmt.Errorf("linker/pe: link: %w", err)
	}

	bin, err := linkResult.Emit()
	if err != nil {
		return nil, fmt.Errorf("linker/pe: emit: %w", err)
	}

	return bin, nil
}

// collectWindowsLibs returns a deduplicated slice of windows.Entry values,
// keyed by ImportLib filename.
func collectWindowsLibs(result compiler.Result) []windows.Entry {
	seen := make(map[string]struct{})
	var out []windows.Entry

	for _, syslib := range result.Ctx.SystemLibs {
		if syslib.Windows == nil {
			continue
		}
		e := *syslib.Windows
		if _, ok := seen[e.ImportLib]; ok {
			continue
		}
		seen[e.ImportLib] = struct{}{}
		out = append(out, e)
	}

	return out
}

// resolveImportLib searches for the .lib (or MinGW .a) file for e.
//
// Search order:
//  1. sdkLibDir/e.ImportLib              (MSVC SDK layout)
//  2. sdkLibDir/lib<baseName>.a          (MinGW archive naming)
//  3. /usr/x86_64-w64-mingw32/lib/...   (MinGW cross-toolchain on Linux)
//  4. /usr/aarch64-w64-mingw32/lib/...  (MinGW cross-toolchain on Linux, arm64)
func resolveImportLib(e windows.Entry, sdkLibDir string, arch object.Arch) (string, error) {
	candidates := []string{}

	if sdkLibDir != "" {
		candidates = append(candidates,
			sdkLibDir+"/"+e.ImportLib,
			sdkLibDir+"/lib"+stripLibExt(e.ImportLib)+".a",
		)
	}

	mingwTriple := "x86_64-w64-mingw32"
	if arch == object.ARM64 {
		mingwTriple = "aarch64-w64-mingw32"
	}
	candidates = append(candidates,
		"/usr/"+mingwTriple+"/lib/"+e.ImportLib,
		"/usr/"+mingwTriple+"/lib/lib"+stripLibExt(e.ImportLib)+".a",
	)

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("not found in any candidate path")
}

// stripLibExt removes the ".lib" extension from an import library filename,
// used to derive the equivalent MinGW archive name (e.g. "kernel32.lib" → "kernel32").
func stripLibExt(name string) string {
	if len(name) > 4 && name[len(name)-4:] == ".lib" {
		return name[:len(name)-4]
	}
	return name
}

// ────────────────────────────────────────────────────────────────────────────
// Arch helpers (mirrors abi/resolve.go, kept local to avoid circular imports)
// ────────────────────────────────────────────────────────────────────────────

func toLinuxArch(a object.Arch) linux.Arch {
	if a == object.ARM64 {
		return linux.ARM64
	}
	return linux.AMD64
}

func toDarwinArch(a object.Arch) darwin.Arch {
	if a == object.ARM64 {
		return darwin.ARM64
	}
	return darwin.AMD64
}

// ────────────────────────────────────────────────────────────────────────────
// Defaults
// ────────────────────────────────────────────────────────────────────────────

func applyDefaults(opts Options) Options {
	if opts.GoArch == "" {
		opts.GoArch = runtime.GOARCH
	}
	if opts.GoOS == "" {
		opts.GoOS = runtime.GOOS
	}

	if opts.Entry == "" && opts.OutputType != OutputShared {
		switch opts.GoOS {
		case "windows":
			opts.Entry = "mainCRTStartup"
		default:
			opts.Entry = "_start"
		}
	}

	if opts.ELFInterp == "" {
		arch, _ := goarchToObjectArch(opts.GoArch)
		opts.ELFInterp = defaultELFInterp(arch)
	}

	return opts
}

func goarchToObjectArch(goarch string) (object.Arch, error) {
	switch goarch {
	case "amd64":
		return object.AMD64, nil
	case "arm64":
		return object.ARM64, nil
	default:
		return 0, fmt.Errorf("unsupported GOARCH %q", goarch)
	}
}

// Ensure abi and toDarwinArch are used.
var _ = abi.SystemLibResult{}
var _ = toDarwinArch