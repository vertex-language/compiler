// abi/resolve.go  (new file alongside abi.go)
package abi

import (
	"fmt"

	"github.com/vertex-language/compiler/abi/darwin"
	"github.com/vertex-language/compiler/abi/linux"
	"github.com/vertex-language/compiler/abi/windows"
	"github.com/vertex-language/compiler/object"
)

// SystemLibResult is returned by ResolveSystemLib.
type SystemLibResult struct {
	// Linux / FreeBSD: the resolved Entry with Soname + Candidates.
	Linux *linux.Entry

	// Darwin: the resolved Entry with InstallName + SDKStub.
	Darwin *darwin.Entry

	// Windows: the resolved Entry with ImportLib + DLLName.
	Windows *windows.Entry
}

// ResolveSystemLib resolves a wasm import to its platform-specific system
// library entry. imp must have a Kind of LinuxSystemLib, DarwinSystemLib,
// or WindowsSystemLib — all other kinds return an error.
//
// arch is the target object.Arch (AMD64 or ARM64).
// platform is the target object.Platform.
//
// The driver calls this for every import whose RouteKind IsSystemLib().
func ResolveSystemLib(imp Import, arch object.Arch, platform object.Platform) (SystemLibResult, error) {
	if !imp.Kind.IsSystemLib() {
		return SystemLibResult{}, fmt.Errorf(
			"abi: ResolveSystemLib called on non-SystemLib import %q (kind=%s)",
			imp.Vendor+"/"+imp.Sub, imp.Kind,
		)
	}

	switch imp.Kind {
	case LinuxSystemLib:
		la := toLinuxArch(arch)
		e, ok := linux.Resolve(imp.Sub, la)
		if !ok {
			return SystemLibResult{}, fmt.Errorf(
				"abi: unknown Linux system library %q — not in abi/linux table",
				imp.Sub,
			)
		}
		return SystemLibResult{Linux: &e}, nil

	case DarwinSystemLib:
		da := toDarwinArch(arch)
		// Framework sub-path: "darwin/framework/CoreFoundation"
		const fwPrefix = "framework/"
		if len(imp.Sub) > len(fwPrefix) && imp.Sub[:len(fwPrefix)] == fwPrefix {
			name := imp.Sub[len(fwPrefix):]
			fe, ok := darwin.ResolveFramework(name)
			if !ok {
				return SystemLibResult{}, fmt.Errorf(
					"abi: unknown Darwin framework %q — not in abi/darwin table",
					name,
				)
			}
			// Wrap framework as a synthetic Entry for the driver.
			e := darwin.Entry{
				InstallName: fe.InstallName,
				SDKStub:     fe.SDKStub,
			}
			return SystemLibResult{Darwin: &e}, nil
		}
		e, ok := darwin.Resolve(imp.Sub, da)
		if !ok {
			return SystemLibResult{}, fmt.Errorf(
				"abi: unknown Darwin system library %q — not in abi/darwin table",
				imp.Sub,
			)
		}
		return SystemLibResult{Darwin: &e}, nil

	case WindowsSystemLib:
		wa := toWindowsArch(arch)
		e, ok := windows.Resolve(imp.Sub, wa)
		if !ok {
			return SystemLibResult{}, fmt.Errorf(
				"abi: unknown Windows system library %q — not in abi/windows table",
				imp.Sub,
			)
		}
		return SystemLibResult{Windows: &e}, nil
	}

	return SystemLibResult{}, fmt.Errorf("abi: unhandled SystemLib kind %s", imp.Kind)
}

func toLinuxArch(a object.Arch) linux.Arch {
	switch a {
	case object.ARM64:
		return linux.ARM64
	default:
		return linux.AMD64
	}
}

func toDarwinArch(a object.Arch) darwin.Arch {
	switch a {
	case object.ARM64:
		return darwin.ARM64
	default:
		return darwin.AMD64
	}
}

func toWindowsArch(a object.Arch) windows.Arch {
	switch a {
	case object.ARM64:
		return windows.ARM64
	default:
		return windows.AMD64
	}
}