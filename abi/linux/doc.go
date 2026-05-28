// Package linux provides system library resolution for LinuxSystemLib imports.
//
// When the wasm IR declares an import like:
//
//	(import "linux/libc" "printf@ptr" (func (param i32) (result i32)))
//
// the driver calls Resolve("libc") to obtain an ordered list of candidate
// paths for the current architecture. The first path that exists on disk is
// used; if none exist the driver emits a hard compile error.
//
// All paths are for 64-bit targets only (amd64, arm64). 32-bit is not
// supported by the Vertex compiler.
//
// Path priority order within each entry reflects the FHS layout conventions
// of the major distribution families:
//
//	1. Debian/Ubuntu  — /usr/lib/<triplet>/libfoo.so.N
//	2. Fedora/RHEL    — /lib64/libfoo.so.N  or  /usr/lib64/libfoo.so.N
//	3. Arch Linux     — /usr/lib/libfoo.so.N
//	4. Alpine (musl)  — /lib/libfoo.so.N  or  /usr/lib/libfoo.so.N
//	5. Generic        — /lib/libfoo.so.N
package linux