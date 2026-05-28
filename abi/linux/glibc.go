package linux

// glibc.go registers the core GNU C Library family:
//
//   libc       — standard C library (glibc soname libc.so.6)
//   libm       — math library
//   libpthread — POSIX threads (merged into libc in glibc 2.34+, symlink retained)
//   libdl      — dynamic linking (merged into libc in glibc 2.34+, symlink retained)
//   librt      — POSIX realtime extensions (merged into libc in glibc 2.34+)
//   libgcc_s   — GCC low-level runtime (unwinding, soft-float, etc.)
//   libstdc++  — GNU C++ standard library
//   libc++     — LLVM C++ standard library (Void Linux, some Alpine setups)

func init() {
	// ── libc ────────────────────────────────────────────────────────────────
	register("c", Entry{
		Soname: "libc.so.6",
		Candidates: []string{
			// Debian / Ubuntu amd64
			"/usr/lib/x86_64-linux-gnu/libc.so.6",
			"/lib/x86_64-linux-gnu/libc.so.6",
			// Fedora / RHEL / CentOS amd64
			"/lib64/libc.so.6",
			"/usr/lib64/libc.so.6",
			// Arch Linux
			"/usr/lib/libc.so.6",
			// Generic fallback
			"/lib/libc.so.6",
		},
	}, Entry{
		Soname: "libc.so.6",
		Candidates: []string{
			// Debian / Ubuntu arm64
			"/usr/lib/aarch64-linux-gnu/libc.so.6",
			"/lib/aarch64-linux-gnu/libc.so.6",
			// Fedora / RHEL arm64
			"/lib64/libc.so.6",
			"/usr/lib64/libc.so.6",
			// Arch Linux ARM
			"/usr/lib/libc.so.6",
			// Alpine musl arm64 (libc.musl-aarch64.so.1 symlinks to ld-musl)
			"/lib/libc.musl-aarch64.so.1",
			// Generic fallback
			"/lib/libc.so.6",
		},
	})

	// ── libm ────────────────────────────────────────────────────────────────
	// Note: in glibc 2.31+ libm is merged but the soname symlink is kept.
	register("m", Entry{
		Soname: "libm.so.6",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libm.so.6",
			"/lib/x86_64-linux-gnu/libm.so.6",
			"/lib64/libm.so.6",
			"/usr/lib64/libm.so.6",
			"/usr/lib/libm.so.6",
			"/lib/libm.so.6",
		},
	}, Entry{
		Soname: "libm.so.6",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libm.so.6",
			"/lib/aarch64-linux-gnu/libm.so.6",
			"/lib64/libm.so.6",
			"/usr/lib64/libm.so.6",
			"/usr/lib/libm.so.6",
			"/lib/libm.so.6",
		},
	})

	// ── libpthread ──────────────────────────────────────────────────────────
	// Merged into libc in glibc 2.34 (Ubuntu 22.04+, Fedora 35+).
	// The stub .so symlink is always present for backwards compat.
	register("pthread", Entry{
		Soname: "libpthread.so.0",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libpthread.so.0",
			"/lib/x86_64-linux-gnu/libpthread.so.0",
			"/lib64/libpthread.so.0",
			"/usr/lib64/libpthread.so.0",
			"/usr/lib/libpthread.so.0",
			"/lib/libpthread.so.0",
		},
	}, Entry{
		Soname: "libpthread.so.0",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libpthread.so.0",
			"/lib/aarch64-linux-gnu/libpthread.so.0",
			"/lib64/libpthread.so.0",
			"/usr/lib64/libpthread.so.0",
			"/usr/lib/libpthread.so.0",
			"/lib/libpthread.so.0",
		},
	})

	// ── libdl ───────────────────────────────────────────────────────────────
	// Merged into libc in glibc 2.34. Stub symlink retained.
	register("dl", Entry{
		Soname: "libdl.so.2",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libdl.so.2",
			"/lib/x86_64-linux-gnu/libdl.so.2",
			"/lib64/libdl.so.2",
			"/usr/lib64/libdl.so.2",
			"/usr/lib/libdl.so.2",
			"/lib/libdl.so.2",
		},
	}, Entry{
		Soname: "libdl.so.2",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libdl.so.2",
			"/lib/aarch64-linux-gnu/libdl.so.2",
			"/lib64/libdl.so.2",
			"/usr/lib64/libdl.so.2",
			"/usr/lib/libdl.so.2",
			"/lib/libdl.so.2",
		},
	})

	// ── librt ───────────────────────────────────────────────────────────────
	// POSIX realtime: shm_open, mq_*, clock_* (pre-2.34 separate lib).
	// Merged into libc in glibc 2.34. Stub symlink retained.
	register("rt", Entry{
		Soname: "librt.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/librt.so.1",
			"/lib/x86_64-linux-gnu/librt.so.1",
			"/lib64/librt.so.1",
			"/usr/lib64/librt.so.1",
			"/usr/lib/librt.so.1",
			"/lib/librt.so.1",
		},
	}, Entry{
		Soname: "librt.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/librt.so.1",
			"/lib/aarch64-linux-gnu/librt.so.1",
			"/lib64/librt.so.1",
			"/usr/lib64/librt.so.1",
			"/usr/lib/librt.so.1",
			"/lib/librt.so.1",
		},
	})

	// ── libgcc_s ────────────────────────────────────────────────────────────
	// GCC low-level runtime: stack unwinding, 64-bit arithmetic helpers,
	// atomic ops on older kernels. Required by libstdc++ and many C programs.
	register("gcc_s", Entry{
		Soname: "libgcc_s.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libgcc_s.so.1",
			"/lib/x86_64-linux-gnu/libgcc_s.so.1",
			"/lib64/libgcc_s.so.1",
			"/usr/lib64/libgcc_s.so.1",
			"/usr/lib/libgcc_s.so.1",
			"/lib/libgcc_s.so.1",
		},
	}, Entry{
		Soname: "libgcc_s.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libgcc_s.so.1",
			"/lib/aarch64-linux-gnu/libgcc_s.so.1",
			"/lib64/libgcc_s.so.1",
			"/usr/lib64/libgcc_s.so.1",
			"/usr/lib/libgcc_s.so.1",
			"/lib/libgcc_s.so.1",
		},
	})

	// ── libstdc++ ───────────────────────────────────────────────────────────
	// GNU C++ standard library. Default on glibc distros.
	// soname is libstdc++.so.6 across all modern distros.
	register("stdc++", Entry{
		Soname: "libstdc++.so.6",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libstdc++.so.6",
			"/lib/x86_64-linux-gnu/libstdc++.so.6",
			"/lib64/libstdc++.so.6",
			"/usr/lib64/libstdc++.so.6",
			"/usr/lib/libstdc++.so.6",
			"/lib/libstdc++.so.6",
		},
	}, Entry{
		Soname: "libstdc++.so.6",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libstdc++.so.6",
			"/lib/aarch64-linux-gnu/libstdc++.so.6",
			"/lib64/libstdc++.so.6",
			"/usr/lib64/libstdc++.so.6",
			"/usr/lib/libstdc++.so.6",
			"/lib/libstdc++.so.6",
		},
	})

	// ── libc++ ──────────────────────────────────────────────────────────────
	// LLVM C++ standard library. Default on Alpine with LLVM toolchain,
	// Void Linux musl+clang, and some Gentoo profiles.
	register("c++", Entry{
		Soname: "libc++.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libc++.so.1",
			"/usr/lib/libc++.so.1",
			"/lib/libc++.so.1",
		},
	}, Entry{
		Soname: "libc++.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libc++.so.1",
			"/usr/lib/libc++.so.1",
			"/lib/libc++.so.1",
		},
	})
}