// Package linux provides syscall number lookup for Linux targets.
// Used by the compiler when emitting inline syscall instructions for imports
// under the "linux/kernel/syscalls" module path.
package linux

// SyscallNumber returns the Linux syscall number for name on arch.
// arch is a GOARCH string: "amd64" or "arm64".
//
// Returns 0, false if name is unknown or unavailable on this arch.
func SyscallNumber(name, arch string) (int, bool) {
	var table map[string]int
	switch arch {
	case "amd64":
		table = amd64Numbers
	case "arm64":
		table = arm64Numbers
	default:
		return 0, false
	}
	n, ok := table[name]
	if !ok {
		return 0, false
	}
	return n, true
}