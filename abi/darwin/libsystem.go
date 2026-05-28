package darwin

// libsystem.go registers libSystem.B.dylib and all the stub symlinks that
// re-export from it.
//
// On Darwin, libSystem is the single monolithic system library — it
// re-exports libc, libm, libpthread, libdl, librt, libutil, libresolv,
// and more. The stub symlinks (/usr/lib/libm.dylib → libSystem.B.dylib)
// exist so that -lm / -lpthread etc. work at link time, but the actual
// LC_LOAD_DYLIB path written into the binary should always use the real
// install name of the library the linker resolves against.
//
// We register both the canonical libSystem.B and all its alias stubs so
// that wasm IR can import either form.

func init() {
	// ── libSystem ────────────────────────────────────────────────────────────
	// The root umbrella library. Links to virtually all POSIX functionality.
	// Compatibility version 1.0.0 across all macOS releases.
	registerLib("libSystem", Entry{
		InstallName: "/usr/lib/libSystem.B.dylib",
		SDKStub:     "usr/lib/libSystem.B.tbd",
	})
	// Unversioned alias — some build systems pass -lSystem explicitly.
	registerLib("System", Entry{
		InstallName: "/usr/lib/libSystem.B.dylib",
		SDKStub:     "usr/lib/libSystem.B.tbd",
	})

	// ── libc ─────────────────────────────────────────────────────────────────
	// Re-exported from libSystem. The symlink /usr/lib/libc.dylib exists
	// purely for POSIX compat. LC_LOAD_DYLIB resolves to libSystem.
	registerLib("libc", Entry{
		InstallName: "/usr/lib/libSystem.B.dylib",
		SDKStub:     "usr/lib/libSystem.B.tbd",
	})

	// ── libm ─────────────────────────────────────────────────────────────────
	// Math: sin, cos, sqrt, etc. Merged into libSystem since 10.4.
	// /usr/lib/libm.dylib is a symlink → libSystem.B.dylib.
	registerLib("libm", Entry{
		InstallName: "/usr/lib/libm.dylib",
		SDKStub:     "usr/lib/libm.tbd",
	})

	// ── libpthread ───────────────────────────────────────────────────────────
	// POSIX threads. Merged into libSystem. Stub symlink retained.
	registerLib("libpthread", Entry{
		InstallName: "/usr/lib/libpthread.dylib",
		SDKStub:     "usr/lib/libpthread.tbd",
	})

	// ── libdl ────────────────────────────────────────────────────────────────
	// dlopen, dlsym, dlclose. Merged into libSystem.
	registerLib("libdl", Entry{
		InstallName: "/usr/lib/libdl.dylib",
		SDKStub:     "usr/lib/libdl.tbd",
	})

	// ── libresolv ────────────────────────────────────────────────────────────
	// DNS resolver: res_query, dn_expand, etc. Part of libSystem.
	registerLib("libresolv", Entry{
		InstallName: "/usr/lib/libresolv.9.dylib",
		SDKStub:     "usr/lib/libresolv.9.tbd",
	})

	// ── libutil ──────────────────────────────────────────────────────────────
	// BSD utility: openpty, login_tty, forkpty. Part of libSystem.
	registerLib("libutil", Entry{
		InstallName: "/usr/lib/libutil.dylib",
		SDKStub:     "usr/lib/libutil.tbd",
	})

	// ── libinfo ──────────────────────────────────────────────────────────────
	// NetInfo/Directory Services lookup. Part of libSystem.
	registerLib("libinfo", Entry{
		InstallName: "/usr/lib/libSystem.B.dylib",
		SDKStub:     "usr/lib/libSystem.B.tbd",
	})
}