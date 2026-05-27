package pe

import (
	"fmt"

	binpe "github.com/vertex-language/compiler/bin/pe"
)

// LinkResult is the output of a successful Linker.Link() call.
type LinkResult struct {
	Machine   binpe.MachineType
	Subsystem binpe.Subsystem
	ImageBase uint64
	Entry     string
	IsDLL     bool
	DLLName   string

	Layout  *Layout
	Symtab  *SymbolTable
	Imports []*CollectedImport
	Exports []ExportRecord
	Thunks  *ThunkSection // synthesised import thunks, if any

	DllCharacteristics    uint16
	StackReserve, StackCommit uint64
	HeapReserve, HeapCommit   uint64
	MajorOSVersion, MinorOSVersion       uint16
	MajorSubsystemVersion, MinorSubsystemVersion uint16

	LoadConfig   *binpe.LoadConfig
	DebugEntries []binpe.DebugEntry

	Synth SyntheticLayout // VA info for synthetic sections
}

// ThunkSection holds synthesised import-thunk code.
type ThunkSection struct {
	Data  []byte
	VAddr uint32
	// SymAddr maps symbol name → VA of the thunk.
	SymAddr map[string]uint32
}

// Builder constructs and returns a configured bin/pe.Builder ready to emit
// the final PE image. All relocations must have been patched before calling.
func (r *LinkResult) Builder() *binpe.Builder {
	b := binpe.NewBuilder(r.Machine)
	b.SetSubsystem(r.Subsystem)
	if r.ImageBase != 0 {
		b.SetImageBase(r.ImageBase)
	}
	if r.Entry != "" {
		b.SetEntry(r.Entry)
	}
	if r.IsDLL {
		b.SetDLL(r.DLLName)
	}
	if r.DllCharacteristics != 0 {
		b.SetDllCharacteristics(r.DllCharacteristics)
	}
	if r.StackReserve != 0 || r.StackCommit != 0 {
		b.SetStackSize(r.StackReserve, r.StackCommit)
	}
	if r.HeapReserve != 0 || r.HeapCommit != 0 {
		b.SetHeapSize(r.HeapReserve, r.HeapCommit)
	}
	if r.MajorOSVersion != 0 || r.MinorOSVersion != 0 {
		b.SetOSVersion(r.MajorOSVersion, r.MinorOSVersion)
	}
	if r.MajorSubsystemVersion != 0 || r.MinorSubsystemVersion != 0 {
		b.SetSubsystemVersion(r.MajorSubsystemVersion, r.MinorSubsystemVersion)
	}
	if r.LoadConfig != nil {
		b.SetLoadConfig(*r.LoadConfig)
	}
	for _, de := range r.DebugEntries {
		b.AddDebugEntry(de)
	}

	// Add all merged user sections (including .pdata, .xdata, .tls, .debug
	// sourced from input objects).
	for _, ms := range r.Layout.Sections {
		vsz := ms.VirtualSize
		if vsz == 0 {
			vsz = uint32(len(ms.Data))
		}
		b.AddSection(binpe.Section{
			Name:        ms.Name,
			Chars:       ms.Chars,
			Data:        ms.Data,
			VirtualSize: vsz,
		})
		// Also register the symbol for the entry point if it is in this section.
		// (Entry resolution is done by name, so we just need the symbol registered.)
	}

	// Add synthesised thunk section, if any.
	if r.Thunks != nil && len(r.Thunks.Data) > 0 {
		b.AddSection(binpe.Section{
			Name:        ".text$thk",
			Chars:       binpe.ScnCode,
			Data:        r.Thunks.Data,
			VirtualSize: uint32(len(r.Thunks.Data)),
		})
		// Register thunk symbols so the entry resolver can find them.
		for sym, va := range r.Thunks.SymAddr {
			rva := uint32(va - uint32(r.ImageBase))
			_ = rva
			b.AddSymbol(binpe.Symbol{
				Name:    sym,
				Section: ".text$thk",
				Offset:  va - r.Thunks.VAddr,
				Global:  true,
			})
		}
	}

	// Register all symbols from the symbol table that have resolved VAs, so
	// that bin/pe can resolve the entry-point name.
	for name, gs := range r.Symtab.syms {
		if gs.va == 0 || gs.kind != symDefined {
			continue
		}
		// Find which output section this symbol lives in.
		for _, ms := range r.Layout.Sections {
			if gs.va >= uint64(ms.VAddr)+r.ImageBase &&
				gs.va < uint64(ms.VAddr)+uint64(ms.VirtualSize)+r.ImageBase {
				off := uint32(gs.va - r.ImageBase) - ms.VAddr
				b.AddSymbol(binpe.Symbol{
					Name:    name,
					Section: ms.Name,
					Offset:  off,
					Global:  true,
				})
				break
			}
		}
	}

	// Declare imports.
	for _, ci := range r.Imports {
		imp := binpe.Import{DLL: ci.DLL}
		for _, sym := range ci.Symbols {
			imp.Symbols = append(imp.Symbols, binpe.ImportSymbol{
				Name:    sym.Name,
				Ordinal: sym.Ordinal,
				Hint:    sym.Hint,
			})
		}
		b.AddImport(imp)
	}

	// Declare exports.
	for i, er := range r.Exports {
		ord := er.Ordinal
		if ord == 0 {
			ord = uint16(i + 1)
		}
		b.AddExport(binpe.Export{
			Name:    er.ExportName,
			Symbol:  er.InternalName,
			Ordinal: ord,
		})
	}

	// Override data-directory entries for sections that came from input objects.
	// (bin/pe won't know about .pdata, .tls, .debug VAs since we used AddSection,
	// not SetPdata/SetTLS/AddDebugEntry for those.)
	if ms := r.Layout.SectionByName(".pdata"); ms != nil {
		b.SetExtraDataDir(binpe.DataDirException, ms.VAddr, ms.VirtualSize)
	}
	if ms := r.Layout.SectionByName(".tls"); ms != nil {
		// The TLS data directory is the IMAGE_TLS_DIRECTORY64 struct at offset 0.
		b.SetExtraDataDir(binpe.DataDirTLS, ms.VAddr, 40)
	}

	return b
}

// Emit is a convenience that calls Builder().Emit().
func (r *LinkResult) Emit() ([]byte, error) {
	return r.Builder().Emit()
}

// ResolveSymbolAddresses fills globalSym.va for every defined symbol by
// combining the merged section VAddr, piece offset, and symbol value.
func ResolveSymbolAddresses(symtab *SymbolTable, layout *Layout, imageBase uint64) error {
	return resolveSymbolAddresses(symtab, layout, imageBase)
}

func resolveSymbolAddresses(symtab *SymbolTable, layout *Layout, imageBase uint64) error {
	// Build a fast lookup: (obj, secIdx) → (mergedSection, pieceOffset).
	type key struct {
		obj    *ObjectFile
		secIdx int
	}
	type val struct {
		ms  *MergedSection
		off uint32
	}
	lut := make(map[key]val)
	for _, ms := range layout.Sections {
		for _, p := range ms.Pieces {
			lut[key{p.Obj, p.Sec.Index}] = val{ms, p.Offset}
		}
	}

	for _, gs := range symtab.syms {
		switch gs.kind {
		case symDefined:
			k := key{gs.obj, gs.secIdx}
			if v, ok := lut[k]; ok {
				gs.va = imageBase + uint64(v.ms.VAddr) + uint64(v.off) + uint64(gs.value)
			} else {
				return fmt.Errorf("symbol %q: section not found in layout", gs.name)
			}
		case symAbsolute:
			gs.va = uint64(gs.absVal)
		case symCommon:
			// Common symbols are merged into .bss; find its VA.
			if ms := layout.SectionByName(".bss"); ms != nil {
				gs.va = imageBase + uint64(ms.VAddr) + uint64(gs.value)
			}
		}
	}
	return nil
}

// resolveImportSymbols sets the VA for __imp_xxx symbols from the IAT slot RVAs
// computed during AssignLayout.
func resolveImportSymbols(symtab *SymbolTable, imports []*CollectedImport,
	sl SyntheticLayout, imageBase uint64) {
	for _, ci := range imports {
		for i := range ci.Symbols {
			sym := &ci.Symbols[i]
			key := ci.DLL + "\x00" + sym.Name
			if sym.Name == "" {
				key = fmt.Sprintf("%s\x00#%d", ci.DLL, sym.Ordinal)
			}
			if rva, ok := sl.IATSlotRVA[key]; ok {
				sym.IATSlotRVA = rva
				va := imageBase + uint64(rva)
				// Set __imp_sym VA.
				if gs, ok2 := symtab.syms["__imp_"+sym.Name]; ok2 {
					gs.va = va
				}
			}
		}
	}
}