// result.go
package elf

import (
	binelf "github.com/vertex-language/compiler/bin/elf"
)

// OutputType controls what kind of ELF binary is produced.
type OutputType int

const (
	OutputExec   OutputType = iota // ET_EXEC: position-dependent executable
	OutputPIE                       // ET_DYN: position-independent executable (pie)
	OutputShared                    // ET_DYN: shared library
)

// LinkResult holds all post-link data and can apply it to a bin/elf Builder.
type LinkResult struct {
	Arch       binelf.Arch
	OutputType OutputType
	Entry      string
	Interp     string
	Soname     string
	Rpath      string
	Needed     []string // DT_NEEDED in load order
	Layout     *Layout
	Symtab     *SymbolTable
	Machine    uint16
	EFlags     uint32
}

// Builder builds and returns a fully-configured bin/elf Builder ready for Emit().
func (r *LinkResult) Builder() *binelf.Builder {
	b := binelf.NewBuilder(r.Arch)

	// Output type
	if r.OutputType == OutputShared || r.OutputType == OutputPIE {
		b.SetShared()
	}

	// Architecture flags (required for RISC-V)
	if r.EFlags != 0 {
		b.SetFlags(r.EFlags)
	}

	// Dynamic linking config
	if r.Interp != "" {
		b.SetInterp(r.Interp)
	}
	for _, n := range r.Needed {
		b.AddNeeded(n)
	}
	if r.Soname != "" {
		b.SetSoname(r.Soname)
	}
	if r.Rpath != "" {
		b.SetRpath(r.Rpath)
	}

	// Sections — emit in layout order
	for _, ms := range r.Layout.Sections {
		sec := binelf.Section{
			Name:  ms.Name,
			Type:  ms.Type,
			Flags: ms.Flags,
			Align: ms.Align,
		}
		if ms.Type != shtNobits {
			sec.Data = ms.Data
		} else {
			sec.Size = ms.Size
		}
		b.AddSection(sec)
	}

	// Symbols
	for _, sym := range r.Symtab.All() {
		if sym.RawSym == nil {
			continue
		}
		raw := sym.RawSym
		bsym := binelf.Symbol{
			Name:    sym.Name,
			Section: raw.SectionName,
			Offset:  sym.VAddr - func() uint64 {
				if ms, ok := r.Layout.SectionByName(raw.SectionName); ok {
					return ms.VAddr
				}
				return 0
			}(),
			Size:   raw.Size,
			Global: raw.Bind == stbGlobal,
			Weak:   raw.Bind == stbWeak,
			Type:   raw.Type,
			Vis:    raw.Vis,
		}
		b.AddSymbol(bsym)
	}

	// Entry point
	if r.Entry != "" {
		b.SetEntry(r.Entry)
	}

	return b
}

// collectNeeded returns the DT_NEEDED list in BFS-visited load order,
// deduplicating by soname.
func collectNeeded(libs []*SharedLib) []string {
	seen := make(map[string]bool)
	var out []string
	for _, lib := range libs {
		soname := lib.Soname()
		if !seen[soname] {
			seen[soname] = true
			out = append(out, soname)
		}
	}
	return out
}