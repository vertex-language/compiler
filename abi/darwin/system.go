package darwin

// system.go registers the broader set of Darwin system dylibs that ship
// with macOS and are present in the Xcode SDK as .tbd stubs.
//
// All of these have been in the dyld shared cache since macOS 11.
// The install names below are the LC_LOAD_DYLIB values to embed in the binary.
// Do not stat-check these paths on disk on macOS 11+.

func init() {
	// ── libz ─────────────────────────────────────────────────────────────────
	// zlib deflate/inflate. Ships with macOS since 10.0.
	// Always present; safe to link unconditionally.
	registerLib("libz", Entry{
		InstallName: "/usr/lib/libz.1.dylib",
		SDKStub:     "usr/lib/libz.1.tbd",
	})

	// ── libiconv ─────────────────────────────────────────────────────────────
	// Character set conversion: iconv_open, iconv, iconv_close.
	// Ships with macOS since 10.0. In dyld cache since macOS 11.
	registerLib("libiconv", Entry{
		InstallName: "/usr/lib/libiconv.2.dylib",
		SDKStub:     "usr/lib/libiconv.2.tbd",
	})

	// ── libxml2 ──────────────────────────────────────────────────────────────
	// libxml2 XML parser. Supported Apple SDK library; always ship-safe.
	registerLib("libxml2", Entry{
		InstallName: "/usr/lib/libxml2.2.dylib",
		SDKStub:     "usr/lib/libxml2.2.tbd",
	})

	// ── libxslt ──────────────────────────────────────────────────────────────
	// XSLT processor. Companion to libxml2.
	registerLib("libxslt", Entry{
		InstallName: "/usr/lib/libxslt.1.dylib",
		SDKStub:     "usr/lib/libxslt.1.tbd",
	})

	// ── libcurl ──────────────────────────────────────────────────────────────
	// URL transfer library. Present in macOS SDK but moved fully into dyld
	// shared cache on macOS 11. Use Apple's curl, not a bundled copy.
	registerLib("libcurl", Entry{
		InstallName: "/usr/lib/libcurl.4.dylib",
		SDKStub:     "usr/lib/libcurl.4.tbd",
	})

	// ── libbz2 ───────────────────────────────────────────────────────────────
	// bzip2 compression. Present since 10.5.
	registerLib("libbz2", Entry{
		InstallName: "/usr/lib/libbz2.1.0.dylib",
		SDKStub:     "usr/lib/libbz2.1.0.tbd",
	})

	// ── libpcap ──────────────────────────────────────────────────────────────
	// Packet capture. Ships with macOS; SDK .tbd present.
	registerLib("libpcap", Entry{
		InstallName: "/usr/lib/libpcap.A.dylib",
		SDKStub:     "usr/lib/libpcap.A.tbd",
	})

	// ── libsqlite3 ───────────────────────────────────────────────────────────
	// SQLite embedded database. Ships with macOS and is a supported API.
	registerLib("libsqlite3", Entry{
		InstallName: "/usr/lib/libsqlite3.dylib",
		SDKStub:     "usr/lib/libsqlite3.tbd",
	})

	// ── libncurses ───────────────────────────────────────────────────────────
	// Terminal control. macOS ships ncurses as libncurses.5.4.dylib.
	// /usr/lib/libcurses.dylib and /usr/lib/libncurses.dylib are symlinks.
	registerLib("libncurses", Entry{
		InstallName: "/usr/lib/libncurses.5.4.dylib",
		SDKStub:     "usr/lib/libncurses.5.4.tbd",
	})
	// Alias: some callers use -lcurses
	registerLib("libcurses", Entry{
		InstallName: "/usr/lib/libncurses.5.4.dylib",
		SDKStub:     "usr/lib/libncurses.5.4.tbd",
	})

	// ── libicucore ───────────────────────────────────────────────────────────
	// Apple's ICU (International Components for Unicode) core library.
	// Only the "core" is public API. Do not link against libicui18n directly.
	registerLib("libicucore", Entry{
		InstallName: "/usr/lib/libicucore.A.dylib",
		SDKStub:     "usr/lib/libicucore.A.tbd",
	})

	// ── libcompression ───────────────────────────────────────────────────────
	// Apple Compression framework C API (lz4, zlib, lzma, lzfse, lz4_raw).
	// Available since macOS 10.11.
	registerLib("libcompression", Entry{
		InstallName: "/usr/lib/libcompression.dylib",
		SDKStub:     "usr/lib/libcompression.tbd",
		WeakLink:    false,
	})

	// ── libarchive ───────────────────────────────────────────────────────────
	// Multi-format archive and compression: tar, zip, cpio, etc.
	// In the macOS SDK since 10.9 (Mavericks).
	registerLib("libarchive", Entry{
		InstallName: "/usr/lib/libarchive.2.dylib",
		SDKStub:     "usr/lib/libarchive.2.tbd",
	})

	// ── libedit ──────────────────────────────────────────────────────────────
	// BSD line-editing library (readline-compatible).
	registerLib("libedit", Entry{
		InstallName: "/usr/lib/libedit.3.dylib",
		SDKStub:     "usr/lib/libedit.3.tbd",
	})

	// ── libcharset ───────────────────────────────────────────────────────────
	// Character set locale support, re-exported by libiconv.
	registerLib("libcharset", Entry{
		InstallName: "/usr/lib/libcharset.1.dylib",
		SDKStub:     "usr/lib/libcharset.1.tbd",
	})

	// ── libpam ───────────────────────────────────────────────────────────────
	// Pluggable Authentication Modules. Ships with macOS.
	registerLib("libpam", Entry{
		InstallName: "/usr/lib/libpam.2.dylib",
		SDKStub:     "usr/lib/libpam.2.tbd",
	})

	// ── liblzma ──────────────────────────────────────────────────────────────
	// XZ/LZMA compression. Available since macOS 10.14 (Mojave).
	registerLib("liblzma", Entry{
		InstallName: "/usr/lib/liblzma.5.dylib",
		SDKStub:     "usr/lib/liblzma.5.tbd",
		WeakLink:    true, // absent on 10.13 and earlier
	})
}