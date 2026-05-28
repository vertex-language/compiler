// result.go
package elf

import (
	binelf "github.com/vertex-language/compiler/bin/elf"
)

// OutputType controls what kind of ELF binary is produced.
type OutputType int

const (
	OutputExec   OutputType = iota
	OutputPIE
	OutputShared
)

// LinkResult holds all post-link data and drives the bin/elf Builder.
type LinkResult struct {
	Arch       binelf.Arch
	OutputType OutputType
	Entry      string
	Interp     string
	Soname     string
	Rpath      string
	Needed     []string
	Layout     *Layout
	Symtab     *SymbolTable
	Machine    uint16
	EFlags     uint32

	// PLTSyms is the ordered list of shared-library symbol names that were
	// given PLT stubs. The order matches the stub indices in .plt and the
	// JUMP_SLOT relocation entries in .rela.plt: PLTSyms[i] corresponds to
	// stub i (0-based), .dynsym entry i+1, and GOT.PLT slot 3+i.
	PLTSyms []string
}

// Builder builds and returns a fully-configured bin/elf Builder ready for Emit().
func (r *LinkResult) Builder() *binelf.Builder {
	b := binelf.NewBuilder(r.Arch)

	if r.OutputType == OutputShared || r.OutputType == OutputPIE {
		b.SetShared()
	}
	if r.EFlags != 0 {
		b.SetFlags(r.EFlags)
	}
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

	// Register PLT symbols so bin/elf populates .dynsym with the correct
	// entries. Order must match PLTSyms: entry i+1 in .dynsym corresponds to
	// the JUMP_SLOT relocation at .rela.plt[i] which has symIdx = i+1.
	for _, name := range r.PLTSyms {
		b.AddDynSym(name)
	}

	// Pass all linker-laid-out sections to the builder with their pre-assigned
	// virtual addresses and file offsets. bin/elf's layoutSections will use
	// these directly instead of recomputing, ensuring that PLT stubs and
	// relocated text — both patched against linker-assigned addresses — are
	// serialized at exactly the right positions.
	for _, ms := range r.Layout.Sections {
		sec := binelf.Section{
			Name:                  ms.Name,
			Type:                  ms.Type,
			Flags:                 ms.Flags,
			Align:                 ms.Align,
			PreassignedAddr:       ms.VAddr,
			PreassignedFileOffset: ms.FileOffset,
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
			Offset: sym.VAddr - func() uint64 {
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

	if r.Entry != "" {
		b.SetEntry(r.Entry)
	}

	return b
}

// collectNeeded returns DT_NEEDED sonames in BFS-visited load order,
// deduplicated by soname.
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