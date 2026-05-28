// Package darwin provides system library resolution for DarwinSystemLib imports.
//
// # dyld Shared Cache — Critical Context
//
// Since macOS 11 (Big Sur), Apple merged virtually all system dylibs into the
// dyld shared cache at /System/Library/dyld/. The files no longer exist as
// individual paths on disk under /usr/lib/. This means:
//
//   - Stat-checking candidate paths at compile time does NOT work on macOS 11+.
//   - The install name (LC_LOAD_DYLIB path) is still /usr/lib/libfoo.dylib —
//     that path is what goes into the binary, and dyld resolves it from its
//     shared cache at runtime.
//   - The Xcode SDK ships .tbd (text-based stub) files at those same paths for
//     link-time symbol resolution.
//
// Therefore, Entry.InstallName is the authoritative field. It is written
// directly into the Mach-O LC_LOAD_DYLIB load command. The driver must NOT
// stat-check this path — it will not be present on disk on modern macOS.
//
// The driver should instead verify that the corresponding .tbd stub exists in
// the active Xcode SDK when cross-compiling, or assume presence at runtime
// when targeting the host machine.
//
// # Frameworks
//
// Apple system frameworks (CoreFoundation, Security, IOKit, etc.) use a
// different LC_LOAD_DYLIB path scheme:
//
//	/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation
//
// These are registered with the "framework/" name prefix so the wasm IR can
// import them as:
//
//	(import "darwin/framework/CoreFoundation" "CFStringCreateWithCString@..." ...)
//
// Use ResolveFramework("CoreFoundation") to look them up.
package darwin