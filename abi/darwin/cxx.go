package darwin

// cxx.go registers the C++ runtime and Objective-C runtime dylibs.
//
// libc++ is Apple's default C++ standard library on all Apple platforms.
// Unlike glibc distros where libstdc++ dominates, on Darwin you always
// get LLVM libc++ — it ships with the OS and is in the dyld shared cache.

func init() {
	// ── libc++ ───────────────────────────────────────────────────────────────
	// LLVM C++ standard library. Default on all Apple platforms since Xcode 5
	// (macOS 10.9). Always present; the only C++ stdlib Apple supports.
	registerLib("libc++", Entry{
		InstallName: "/usr/lib/libc++.1.dylib",
		SDKStub:     "usr/lib/libc++.1.tbd",
	})

	// ── libc++abi ────────────────────────────────────────────────────────────
	// Low-level C++ ABI: exception handling, RTTI, dynamic_cast.
	// Required by libc++; rarely linked directly but valid to do so.
	registerLib("libc++abi", Entry{
		InstallName: "/usr/lib/libc++abi.dylib",
		SDKStub:     "usr/lib/libc++abi.tbd",
	})

	// ── libobjc ──────────────────────────────────────────────────────────────
	// Objective-C runtime: objc_msgSend, class registration, ARC.
	// Required for any code using Objective-C or bridging to Cocoa.
	registerLib("libobjc", Entry{
		InstallName: "/usr/lib/libobjc.A.dylib",
		SDKStub:     "usr/lib/libobjc.A.tbd",
	})

	// ── libdispatch ──────────────────────────────────────────────────────────
	// Grand Central Dispatch (GCD). Part of libSystem on modern macOS
	// but historically a separate dylib; install name reflects that.
	registerLib("libdispatch", Entry{
		InstallName: "/usr/lib/system/libdispatch.dylib",
		SDKStub:     "usr/lib/system/libdispatch.tbd",
	})

	// ── libBlocksRuntime ─────────────────────────────────────────────────────
	// Clang Blocks runtime (^{} closures). Part of libSystem; re-exported.
	registerLib("libBlocksRuntime", Entry{
		InstallName: "/usr/lib/libSystem.B.dylib",
		SDKStub:     "usr/lib/libSystem.B.tbd",
	})
}