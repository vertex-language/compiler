// cpu/arm64/reloc.go
package arm64

import "github.com/vertex-language/compiler/object"

// ARM64 relocation aliases — these map semantic arm64 names to the canonical
// RelocKind values defined in the object package.  Using the named constants
// (rather than hardcoded integers) means a future insertion into object.go's
// iota sequence can never silently corrupt the mapping.
//
// Adapter translations (from object package):
//   RelocARM64Call26   → ELF R_AARCH64_CALL26 / PE Branch26 / Mach-O BRANCH26
//   RelocARM64ADRPHi21 → ELF R_AARCH64_ADR_PREL_PG_HI21 / PE PagebaseRel21 / Mach-O PAGE21
//   RelocARM64AddLo12  → ELF R_AARCH64_ADD_ABS_LO12_NC / PE Pageoffset12A / Mach-O PAGEOFF12
//   RelocARM64Ld64Lo12 → ELF R_AARCH64_LDST64_ABS_LO12_NC / PE Pageoffset12L / Mach-O PAGEOFF12
const (
	RelocARM64Call26   = object.RelocCall32
	RelocARM64ADRPHi21 = object.RelocADRP
	RelocARM64AddLo12  = object.RelocADRPOff12Add
	RelocARM64Ld64Lo12 = object.RelocADRPOff12Load
)