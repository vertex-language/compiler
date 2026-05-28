package linux

// crypto.go registers OpenSSL / system TLS libraries.
//
// These are not part of glibc but ship by default on most server-oriented
// distros (Ubuntu, Debian, Fedora, RHEL, Arch). Alpine includes them in
// the base image when openssl is installed, which is near-universal.
//
// soname versioning note:
//
//   libssl.so.3 / libcrypto.so.3   — OpenSSL 3.x  (Ubuntu 22.04+, Fedora 36+,
//                                    Arch, Alpine 3.17+)
//   libssl.so.1.1 / libcrypto.so.1.1 — OpenSSL 1.1 (Ubuntu 20.04, Debian 11,
//                                    older Alpine)
//
// Both are registered. The driver walks candidates in order so OpenSSL 3
// is preferred where both are present.

func init() {
	// ── libssl ──────────────────────────────────────────────────────────────
	register("ssl", Entry{
		Soname: "libssl.so.3",
		Candidates: []string{
			// OpenSSL 3.x — Debian/Ubuntu amd64
			"/usr/lib/x86_64-linux-gnu/libssl.so.3",
			"/lib/x86_64-linux-gnu/libssl.so.3",
			// Fedora / RHEL / CentOS Stream amd64
			"/lib64/libssl.so.3",
			"/usr/lib64/libssl.so.3",
			// Arch, Alpine 3.17+
			"/usr/lib/libssl.so.3",
			"/lib/libssl.so.3",
			// OpenSSL 1.1 fallbacks
			"/usr/lib/x86_64-linux-gnu/libssl.so.1.1",
			"/lib/x86_64-linux-gnu/libssl.so.1.1",
			"/lib64/libssl.so.1.1",
			"/usr/lib64/libssl.so.1.1",
			"/usr/lib/libssl.so.1.1",
			"/lib/libssl.so.1.1",
		},
	}, Entry{
		Soname: "libssl.so.3",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libssl.so.3",
			"/lib/aarch64-linux-gnu/libssl.so.3",
			"/lib64/libssl.so.3",
			"/usr/lib64/libssl.so.3",
			"/usr/lib/libssl.so.3",
			"/lib/libssl.so.3",
			// OpenSSL 1.1 fallbacks
			"/usr/lib/aarch64-linux-gnu/libssl.so.1.1",
			"/lib/aarch64-linux-gnu/libssl.so.1.1",
			"/lib64/libssl.so.1.1",
			"/usr/lib64/libssl.so.1.1",
			"/usr/lib/libssl.so.1.1",
			"/lib/libssl.so.1.1",
		},
	})

	// ── libcrypto ───────────────────────────────────────────────────────────
	register("crypto", Entry{
		Soname: "libcrypto.so.3",
		Candidates: []string{
			"/usr/lib/x86_64-linux-gnu/libcrypto.so.3",
			"/lib/x86_64-linux-gnu/libcrypto.so.3",
			"/lib64/libcrypto.so.3",
			"/usr/lib64/libcrypto.so.3",
			"/usr/lib/libcrypto.so.3",
			"/lib/libcrypto.so.3",
			// OpenSSL 1.1 fallbacks
			"/usr/lib/x86_64-linux-gnu/libcrypto.so.1.1",
			"/lib/x86_64-linux-gnu/libcrypto.so.1.1",
			"/lib64/libcrypto.so.1.1",
			"/usr/lib64/libcrypto.so.1.1",
			"/usr/lib/libcrypto.so.1.1",
			"/lib/libcrypto.so.1.1",
		},
	}, Entry{
		Soname: "libcrypto.so.3",
		Candidates: []string{
			"/usr/lib/aarch64-linux-gnu/libcrypto.so.3",
			"/lib/aarch64-linux-gnu/libcrypto.so.3",
			"/lib64/libcrypto.so.3",
			"/usr/lib64/libcrypto.so.3",
			"/usr/lib/libcrypto.so.3",
			"/lib/libcrypto.so.3",
			// OpenSSL 1.1 fallbacks
			"/usr/lib/aarch64-linux-gnu/libcrypto.so.1.1",
			"/lib/aarch64-linux-gnu/libcrypto.so.1.1",
			"/lib64/libcrypto.so.1.1",
			"/usr/lib64/libcrypto.so.1.1",
			"/usr/lib/libcrypto.so.1.1",
			"/lib/libcrypto.so.1.1",
		},
	})
}