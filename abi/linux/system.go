package linux

// system.go registers OS-level system libraries that are present on
// virtually all glibc-based Linux distributions:
//
//   libz        — zlib compression (deflate/inflate)
//   libresolv   — DNS resolver
//   libnsl      — NIS / YP network services (legacy, glibc 2.32+ separate)
//   libcrypt    — password hashing / DES crypt(3)  [libxcrypt on modern distros]
//   libutil     — BSD utility functions (login, pty, etc.)
//   libpam      — Pluggable Authentication Modules
//   libcap      — POSIX capabilities
//   libseccomp  — seccomp syscall filtering
//   libselinux  — SELinux access control
//   libudev     — udev device events (systemd)
//   libsystemd  — systemd sd-daemon, sd-journal, etc.

func init() {
	// ── libz ────────────────────────────────────────────────────────────────
	register("z", Entry{
		Soname: "libz.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libz.so.1",
			"/lib/x86_64-linux-gnu/libz.so.1",
			"/lib64/libz.so.1",
			"/usr/lib64/libz.so.1",
			"/usr/lib/libz.so.1",
			// Alpine musl
			"/lib/libz.so.1",
		},
	}, Entry{
		Soname: "libz.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libz.so.1",
			"/lib/aarch64-linux-gnu/libz.so.1",
			"/lib64/libz.so.1",
			"/usr/lib64/libz.so.1",
			"/usr/lib/libz.so.1",
			"/lib/libz.so.1",
		},
	})

	// ── libresolv ───────────────────────────────────────────────────────────
	// DNS resolution: res_query, res_search, dn_expand, etc.
	// Merged into libc in glibc 2.34; stub symlink kept.
	register("resolv", Entry{
		Soname: "libresolv.so.2",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libresolv.so.2",
			"/lib/x86_64-linux-gnu/libresolv.so.2",
			"/lib64/libresolv.so.2",
			"/usr/lib64/libresolv.so.2",
			"/usr/lib/libresolv.so.2",
			"/lib/libresolv.so.2",
		},
	}, Entry{
		Soname: "libresolv.so.2",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libresolv.so.2",
			"/lib/aarch64-linux-gnu/libresolv.so.2",
			"/lib64/libresolv.so.2",
			"/usr/lib64/libresolv.so.2",
			"/usr/lib/libresolv.so.2",
			"/lib/libresolv.so.2",
		},
	})

	// ── libnsl ──────────────────────────────────────────────────────────────
	// NIS/YP: gethostbyname_r, getpwnam_r via NIS, rpc helpers.
	// Separated from glibc 2.32+ into a standalone libnsl package.
	// Absent on Alpine (musl does not implement NIS).
	register("nsl", Entry{
		Soname: "libnsl.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libnsl.so.1",
			"/lib/x86_64-linux-gnu/libnsl.so.1",
			"/lib64/libnsl.so.1",
			"/usr/lib64/libnsl.so.1",
			"/usr/lib/libnsl.so.1",
			"/lib/libnsl.so.1",
		},
	}, Entry{
		Soname: "libnsl.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libnsl.so.1",
			"/lib/aarch64-linux-gnu/libnsl.so.1",
			"/lib64/libnsl.so.1",
			"/usr/lib64/libnsl.so.1",
			"/usr/lib/libnsl.so.1",
			"/lib/libnsl.so.1",
		},
	})

	// ── libcrypt ────────────────────────────────────────────────────────────
	// crypt(3) password hashing. Replaced by libxcrypt on Fedora 30+,
	// Ubuntu 22.04+, Arch. Both use the same soname libcrypt.so.1.
	register("crypt", Entry{
		Soname: "libcrypt.so.1",
		Candidates: []string{
			// libxcrypt path on Debian/Ubuntu
			"/usr/lib/x86_64-linux-gnu/libcrypt.so.1",
			"/lib/x86_64-linux-gnu/libcrypt.so.1",
			// Fedora / RHEL libxcrypt
			"/lib64/libcrypt.so.1",
			"/usr/lib64/libcrypt.so.1",
			// Arch
			"/usr/lib/libcrypt.so.1",
			// Alpine (musl built-in — usually no separate .so)
			"/lib/libcrypt.so.1",
		},
	}, Entry{
		Soname: "libcrypt.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libcrypt.so.1",
			"/lib/aarch64-linux-gnu/libcrypt.so.1",
			"/lib64/libcrypt.so.1",
			"/usr/lib64/libcrypt.so.1",
			"/usr/lib/libcrypt.so.1",
			"/lib/libcrypt.so.1",
		},
	})

	// ── libutil ─────────────────────────────────────────────────────────────
	// BSD utility functions: openpty, login_tty, forkpty.
	// Merged into libc in glibc 2.34; stub symlink retained.
	register("util", Entry{
		Soname: "libutil.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libutil.so.1",
			"/lib/x86_64-linux-gnu/libutil.so.1",
			"/lib64/libutil.so.1",
			"/usr/lib64/libutil.so.1",
			"/usr/lib/libutil.so.1",
			"/lib/libutil.so.1",
		},
	}, Entry{
		Soname: "libutil.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libutil.so.1",
			"/lib/aarch64-linux-gnu/libutil.so.1",
			"/lib64/libutil.so.1",
			"/usr/lib64/libutil.so.1",
			"/usr/lib/libutil.so.1",
			"/lib/libutil.so.1",
		},
	})

	// ── libpam ──────────────────────────────────────────────────────────────
	// Linux PAM: pam_authenticate, pam_open_session, etc.
	// Present on all distros that support login/sudo/sshd.
	register("pam", Entry{
		Soname: "libpam.so.0",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libpam.so.0",
			"/lib/x86_64-linux-gnu/libpam.so.0",
			"/lib64/libpam.so.0",
			"/usr/lib64/libpam.so.0",
			"/usr/lib/libpam.so.0",
			"/lib/libpam.so.0",
		},
	}, Entry{
		Soname: "libpam.so.0",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libpam.so.0",
			"/lib/aarch64-linux-gnu/libpam.so.0",
			"/lib64/libpam.so.0",
			"/usr/lib64/libpam.so.0",
			"/usr/lib/libpam.so.0",
			"/lib/libpam.so.0",
		},
	})

	// ── libcap ──────────────────────────────────────────────────────────────
	// POSIX capabilities: cap_set_proc, cap_get_proc, etc.
	register("cap", Entry{
		Soname: "libcap.so.2",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libcap.so.2",
			"/lib/x86_64-linux-gnu/libcap.so.2",
			"/lib64/libcap.so.2",
			"/usr/lib64/libcap.so.2",
			"/usr/lib/libcap.so.2",
			"/lib/libcap.so.2",
		},
	}, Entry{
		Soname: "libcap.so.2",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libcap.so.2",
			"/lib/aarch64-linux-gnu/libcap.so.2",
			"/lib64/libcap.so.2",
			"/usr/lib64/libcap.so.2",
			"/usr/lib/libcap.so.2",
			"/lib/libcap.so.2",
		},
	})

	// ── libseccomp ──────────────────────────────────────────────────────────
	// seccomp BPF syscall filtering. Used by containers, sandboxes, systemd.
	register("seccomp", Entry{
		Soname: "libseccomp.so.2",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libseccomp.so.2",
			"/lib/x86_64-linux-gnu/libseccomp.so.2",
			"/lib64/libseccomp.so.2",
			"/usr/lib64/libseccomp.so.2",
			"/usr/lib/libseccomp.so.2",
			"/lib/libseccomp.so.2",
		},
	}, Entry{
		Soname: "libseccomp.so.2",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libseccomp.so.2",
			"/lib/aarch64-linux-gnu/libseccomp.so.2",
			"/lib64/libseccomp.so.2",
			"/usr/lib64/libseccomp.so.2",
			"/usr/lib/libseccomp.so.2",
			"/lib/libseccomp.so.2",
		},
	})

	// ── libselinux ──────────────────────────────────────────────────────────
	// SELinux mandatory access control. Present on Fedora/RHEL by default;
	// optional on Debian/Ubuntu; absent on Alpine.
	register("selinux", Entry{
		Soname: "libselinux.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libselinux.so.1",
			"/lib/x86_64-linux-gnu/libselinux.so.1",
			"/lib64/libselinux.so.1",
			"/usr/lib64/libselinux.so.1",
			"/usr/lib/libselinux.so.1",
			"/lib/libselinux.so.1",
		},
	}, Entry{
		Soname: "libselinux.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libselinux.so.1",
			"/lib/aarch64-linux-gnu/libselinux.so.1",
			"/lib64/libselinux.so.1",
			"/usr/lib64/libselinux.so.1",
			"/usr/lib/libselinux.so.1",
			"/lib/libselinux.so.1",
		},
	})

	// ── libudev ─────────────────────────────────────────────────────────────
	// udev device event monitoring. Part of systemd; absent on Alpine/musl.
	register("udev", Entry{
		Soname: "libudev.so.1",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libudev.so.1",
			"/lib/x86_64-linux-gnu/libudev.so.1",
			"/lib64/libudev.so.1",
			"/usr/lib64/libudev.so.1",
			"/usr/lib/libudev.so.1",
			"/lib/libudev.so.1",
		},
	}, Entry{
		Soname: "libudev.so.1",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libudev.so.1",
			"/lib/aarch64-linux-gnu/libudev.so.1",
			"/lib64/libudev.so.1",
			"/usr/lib64/libudev.so.1",
			"/usr/lib/libudev.so.1",
			"/lib/libudev.so.1",
		},
	})

	// ── libsystemd ──────────────────────────────────────────────────────────
	// sd-daemon, sd-journal, sd-bus. Absent on Alpine/musl and non-systemd distros.
	register("systemd", Entry{
		Soname: "libsystemd.so.0",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libsystemd.so.0",
			"/lib/x86_64-linux-gnu/libsystemd.so.0",
			"/lib64/libsystemd.so.0",
			"/usr/lib64/libsystemd.so.0",
			"/usr/lib/libsystemd.so.0",
			"/lib/libsystemd.so.0",
		},
	}, Entry{
		Soname: "libsystemd.so.0",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libsystemd.so.0",
			"/lib/aarch64-linux-gnu/libsystemd.so.0",
			"/lib64/libsystemd.so.0",
			"/usr/lib64/libsystemd.so.0",
			"/usr/lib/libsystemd.so.0",
			"/lib/libsystemd.so.0",
		},
	})
}