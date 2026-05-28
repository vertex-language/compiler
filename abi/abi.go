// Package abi parses wasm import and export name fields and maps module paths
// to the compiler backend strategy described in the Vertex ABI reference.
//
// The import module path is the single source of truth for routing.
// Parse and ParseExport are the two entry points most callers need.
//
// Platform-specific system library resolution tables live in sub-packages:
//
//	abi/linux   — LinuxSystemLib candidate paths
//	abi/windows — WindowsSystemLib candidate paths
//	abi/darwin  — DarwinSystemLib candidate paths
package abi

import "strings"

// ─── Import routing ───────────────────────────────────────────────────────────

// RouteKind is the backend strategy the compiler uses for a wasm import.
type RouteKind int

const (
	// LinuxKernelSyscall: "linux/kernel/syscalls"
	// Emit an inline syscall instruction at the call site.
	// No PLT, no libc, no linker involvement.
	LinuxKernelSyscall RouteKind = iota

	// LinuxSystemLib: "linux/<lib>"
	// Link against a known Linux system library (glibc / musl).
	// Candidate paths resolved via abi/linux.
	LinuxSystemLib

	// WindowsSystemLib: "windows/<dll>"
	// Link against a known Windows system DLL via IAT entry.
	// Candidate paths resolved via abi/windows.
	// Not valid on non-Windows targets.
	WindowsSystemLib

	// DarwinSystemLib: "darwin/<lib>"
	// Link against a known macOS system library via LC_LOAD_DYLIB stub.
	// Candidate paths resolved via abi/darwin.
	// Not valid on non-Darwin targets.
	DarwinSystemLib

	// VcpkgLib: "lib/<name>"
	// Third-party library fetched and compiled via vcpkg before linking.
	VcpkgLib

	// MetalBIOS: "hw/bios/..."
	// Bare-metal BIOS interface. Emits inline int 0xNN instructions or
	// direct in/out port I/O. Intended for bootloader and firmware targets.
	MetalBIOS

	// MetalUEFI: "hw/uefi/..."
	// Bare-metal UEFI firmware interface. Resolves through EFI_SYSTEM_TABLE
	// vtable at the call site. Intended for bootloader and firmware targets.
	MetalUEFI

	// GPUIntrinsic: "gpu/cuda" | "gpu/msl" | "gpu/vulkan"
	// Maps to a PTX / MSL / SPIR-V built-in.
	// Only valid inside a function routed to the matching GPU backend via
	// a @cuda / @msl / @vulkan export suffix.
	GPUIntrinsic

	// VertexMemory: "memory"
	// Internal Vertex allocator. Resolved to __vertex_memory_* stubs.
	// malloc, free, and mmap imports are compile-time errors — use this instead.
	VertexMemory
)

// String returns a human-readable description of a RouteKind,
// useful in compiler diagnostics.
func (k RouteKind) String() string {
	switch k {
	case LinuxKernelSyscall:
		return "linux/kernel/syscalls"
	case LinuxSystemLib:
		return "linux/*"
	case WindowsSystemLib:
		return "windows/*"
	case DarwinSystemLib:
		return "darwin/*"
	case VcpkgLib:
		return "lib/*"
	case MetalBIOS:
		return "hw/bios/*"
	case MetalUEFI:
		return "hw/uefi/*"
	case GPUIntrinsic:
		return "gpu/*"
	case VertexMemory:
		return "memory"
	default:
		return "unknown"
	}
}

// IsMetal reports whether this route targets bare-metal hardware directly —
// either BIOS or UEFI. Useful for the driver to gate these on appropriate
// output types (e.g. raw binary, EFI application) and reject them for
// standard OS-linked executables.
func (k RouteKind) IsMetal() bool {
	return k == MetalBIOS || k == MetalUEFI
}

// IsSystemLib reports whether this route targets a hardcoded OS system library.
// The driver resolves the actual path via the platform-specific abi/* sub-package.
func (k RouteKind) IsSystemLib() bool {
	return k == LinuxSystemLib || k == WindowsSystemLib || k == DarwinSystemLib
}

// ─── Import ───────────────────────────────────────────────────────────────────

// Import is the result of parsing a wasm import module path.
type Import struct {
	Kind RouteKind

	// Vendor is the first path segment:
	// "linux", "windows", "darwin", "lib", "hw", "gpu", or "memory".
	Vendor string

	// Sub is everything after the first segment, e.g.:
	//   "kernel/syscalls", "libc", "kernel32", "libSystem",
	//   "bios/int10h", "uefi/boot_services", "cuda"
	// Empty for the "memory" vendor.
	Sub string
}

// Parse maps a wasm import module string to an Import.
// Unrecognised namespaces return a zero-value Import; callers should
// use IsUnrecognised to detect and surface this as a compile error.
func Parse(module string) Import {
	vendor, sub, _ := strings.Cut(module, "/")

	switch vendor {
	case "linux":
		if sub == "kernel/syscalls" {
			return Import{Kind: LinuxKernelSyscall, Vendor: "linux", Sub: sub}
		}
		return Import{Kind: LinuxSystemLib, Vendor: "linux", Sub: sub}

	case "windows":
		return Import{Kind: WindowsSystemLib, Vendor: "windows", Sub: sub}

	case "darwin":
		return Import{Kind: DarwinSystemLib, Vendor: "darwin", Sub: sub}

	case "lib":
		return Import{Kind: VcpkgLib, Vendor: "lib", Sub: sub}

	case "hw":
		if strings.HasPrefix(sub, "bios") {
			return Import{Kind: MetalBIOS, Vendor: "hw", Sub: sub}
		}
		if strings.HasPrefix(sub, "uefi") {
			return Import{Kind: MetalUEFI, Vendor: "hw", Sub: sub}
		}

	case "gpu":
		return Import{Kind: GPUIntrinsic, Vendor: "gpu", Sub: sub}

	case "memory":
		return Import{Kind: VertexMemory, Vendor: "memory", Sub: ""}
	}

	return Import{Vendor: vendor, Sub: sub}
}

// IsUnrecognised reports whether an Import came back from Parse with a
// namespace the compiler does not know about. Callers should surface
// this as a hard compile-time error.
func IsUnrecognised(i Import) bool {
	switch i.Vendor {
	case "linux", "windows", "darwin", "lib", "hw", "gpu", "memory":
		return false
	}
	return true
}

// GPUBackend returns the GPU backend identifier for a GPUIntrinsic import:
// "cuda", "msl", or "vulkan". Returns "" for any other RouteKind.
func (i Import) GPUBackend() string {
	if i.Kind != GPUIntrinsic {
		return ""
	}
	return i.Sub
}

// ─── Import signature parsing (@-suffix) ──────────────────────────────────────

// Sig is the parsed ABI signature suffix on a wasm import name field.
//
//	"write@i32.ptr.i32"       → Sig{Name:"write",  PtrMask:[F,T,F]}
//	"fopen@ptr.ptr:hptr"      → Sig{Name:"fopen",  PtrMask:[T,T],   RetHptr:true}
//	"fwrite@ptr.i64.i64.hptr" → Sig{Name:"fwrite", HptrMask:[F,F,F,T]}
//	"getpid"                  → Sig{Name:"getpid"}
type Sig struct {
	// Name is the function name with the @-suffix stripped.
	Name string

	// PtrMask[i] is true when parameter i is a linear-memory pointer (ptr).
	// The compiler adds R15 before the call to translate it to a native VA.
	PtrMask []bool

	// HptrMask[i] is true when parameter i is an opaque native handle (hptr).
	// The compiler resolves it through the Handle Table before the call.
	HptrMask []bool

	// RetHptr is true when the function returns an opaque native handle.
	// The compiler intercepts the return value and registers it in the
	// Handle Table, handing the 32-bit index back to wasm.
	RetHptr bool
}

// HasPointers reports whether the signature contains any ptr or hptr
// parameters, or an hptr return.
func (s Sig) HasPointers() bool {
	for _, v := range s.PtrMask {
		if v {
			return true
		}
	}
	for _, v := range s.HptrMask {
		if v {
			return true
		}
	}
	return s.RetHptr
}

// ParseSig parses the name field of a wasm import declaration into a Sig.
// If no @ suffix is present the name is returned unchanged with empty masks.
func ParseSig(name string) Sig {
	base, suffix, hasSuffix := strings.Cut(name, "@")
	if !hasSuffix || suffix == "" {
		return Sig{Name: base}
	}

	paramStr, retStr, _ := strings.Cut(suffix, ":")

	sig := Sig{Name: base, RetHptr: retStr == "hptr"}

	if paramStr != "" {
		tokens := strings.Split(paramStr, ".")
		sig.PtrMask = make([]bool, len(tokens))
		sig.HptrMask = make([]bool, len(tokens))
		for i, t := range tokens {
			switch t {
			case "ptr":
				sig.PtrMask[i] = true
			case "hptr":
				sig.HptrMask[i] = true
			}
		}
	}
	return sig
}

// ─── Export suffix parsing (@kind:type.type...) ───────────────────────────────

// ExportKind identifies the backend or concurrency model declared by an
// export's @-suffix.
type ExportKind int

const (
	// ExportCPU: no @-suffix — compiled normally by the default CPU backend.
	ExportCPU ExportKind = iota

	// ExportCUDA: @cuda — emitted as PTX for NVIDIA GPUs (Linux, Windows).
	ExportCUDA

	// ExportMSL: @msl — emitted as Metal Shading Language (macOS only).
	ExportMSL

	// ExportVulkan: @vulkan — emitted as SPIR-V (AMD + CPU fallback, Linux, Windows).
	ExportVulkan

	// ExportAsync: @async — compiled as a stackful coroutine.
	ExportAsync

	// ExportThread: @thread — spawned as an OS thread via clone(2).
	ExportThread

	// ExportProcess: @process — spawned as a child process via fork(2).
	ExportProcess
)

// String returns the @-suffix token for an ExportKind.
func (k ExportKind) String() string {
	switch k {
	case ExportCPU:
		return "cpu"
	case ExportCUDA:
		return "cuda"
	case ExportMSL:
		return "msl"
	case ExportVulkan:
		return "vulkan"
	case ExportAsync:
		return "async"
	case ExportThread:
		return "thread"
	case ExportProcess:
		return "process"
	default:
		return "unknown"
	}
}

// IsGPU reports whether this export targets a GPU backend.
func (k ExportKind) IsGPU() bool {
	return k == ExportCUDA || k == ExportMSL || k == ExportVulkan
}

// IsConcurrent reports whether this export uses a concurrency backend.
func (k ExportKind) IsConcurrent() bool {
	return k == ExportAsync || k == ExportThread || k == ExportProcess
}

// Export is the result of parsing a wasm export name field.
type Export struct {
	// Name is the export name with the @-suffix stripped.
	// This is what the linker uses as the public symbol name.
	Name string

	Kind ExportKind

	// Params holds the parameter type tokens from the optional :type.type...
	// annotation, e.g. ["ptr", "i32"] from "@cuda:ptr.i32".
	// Nil for ExportCPU and for exports with no type list.
	Params []string
}

// ParseExport parses the name field of a wasm export declaration.
// Exports without an @-suffix are returned with Kind == ExportCPU.
// An unrecognised @-suffix also returns ExportCPU so the function stays
// on the CPU routing table; the driver will warn separately if needed.
func ParseExport(name string) Export {
	base, suffix, hasSuffix := strings.Cut(name, "@")
	if !hasSuffix || suffix == "" {
		return Export{Name: base, Kind: ExportCPU}
	}

	kindStr, paramStr, hasParams := strings.Cut(suffix, ":")

	var params []string
	if hasParams && paramStr != "" {
		params = strings.Split(paramStr, ".")
	}

	var kind ExportKind
	switch kindStr {
	case "cuda":
		kind = ExportCUDA
	case "msl":
		kind = ExportMSL
	case "vulkan":
		kind = ExportVulkan
	case "async":
		kind = ExportAsync
	case "thread":
		kind = ExportThread
	case "process":
		kind = ExportProcess
	default:
		kind = ExportCPU
	}

	return Export{Name: base, Kind: kind, Params: params}
}