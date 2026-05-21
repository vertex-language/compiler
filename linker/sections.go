// Package linker — sections.go
// Internal section-kind enum and the alignUp helper used throughout the
// linker.  The actual layout computation (file offsets + VAs) lives in
// output/elf.go so that the output package owns its own binary format.
package linker

// sectionKind identifies one of the linker's internal merged section buffers.
// Used by relocSite to know which buffer to patch and which base VA to use.
type sectionKind int

const (
	secKindText   sectionKind = iota
	secKindROData
	secKindData
)

// alignUp rounds x up to the nearest multiple of a (must be a power of two).
func alignUp(x, a uint64) uint64 {
	if a <= 1 {
		return x
	}
	return (x + a - 1) &^ (a - 1)
}