// Package linux provides syscall number lookup for Linux targets.
// The linker uses this when emitting syscall trampolines for imports
// whose first slot has the form "linux:kernel/syscalls".
package linux

// SyscallNumber returns the syscall number for name on arch.
// arch is a GOARCH string: "amd64" or "arm64".
//
// Returns -1, false if name is unknown or not available on this arch.
// A return of -1 with true means the syscall exists in the table but is
// explicitly unavailable on this arch (e.g. open(2) on arm64).
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