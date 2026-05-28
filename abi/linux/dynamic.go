package linux

// dynamic.go registers the ELF dynamic linker / program interpreter.
//
// The dynamic linker is not a normal import — the driver uses it to populate
// the PT_INTERP segment of ELF executables, not as a DT_NEEDED entry.
// It is exposed here so the driver can look it up by the same Resolve
// mechanism rather than embedding paths in the linker directly.
//
// Use Resolve("ld-linux", arch) to obtain the interpreter path for the
// target architecture.

func init() {
	// ── ld-linux (glibc dynamic linker) ────────────────────────────────────
	register("ld-linux", Entry{
		// amd64: the soname IS the path for the interpreter — it has no DT_SONAME.
		Soname: "ld-linux-x86-64.so.2",
		Candidates: []string{
			// Standard path on all glibc distros (Debian, Fedora, Arch, etc.)
			"/lib64/ld-linux-x86-64.so.2",
			// Debian/Ubuntu multiarch alias
			"/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2",
			// Arch Linux (symlink to /lib64)
			"/usr/lib64/ld-linux-x86-64.so.2",
		},
	}, Entry{
		Soname: "ld-linux-aarch64.so.1",
		Candidates: []string{
			"/lib/ld-linux-aarch64.so.1",
			"/lib/aarch64-linux-gnu/ld-linux-aarch64.so.1",
			"/usr/lib/aarch64-linux-gnu/ld-linux-aarch64.so.1",
			"/usr/lib64/ld-linux-aarch64.so.1",
		},
	})

	// ── ld-musl (Alpine / Void musl dynamic linker) ─────────────────────────
	// musl uses a different interpreter path. Present only on musl-based distros.
	register("ld-musl", Entry{
		Soname: "ld-musl-x86_64.so.1",
		Candidates: []string{
			"/lib/ld-musl-x86_64.so.1",
		},
	}, Entry{
		Soname: "ld-musl-aarch64.so.1",
		Candidates: []string{
			"/lib/ld-musl-aarch64.so.1",
		},
	})
}