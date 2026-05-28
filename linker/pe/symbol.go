package pe

import (
	"fmt"
	"strings"
)

// symKind is the resolution kind of a symbol in the global table.
type symKind int

const (
	symUndefined  symKind = iota // unresolved external reference
	symWeak                      // weak external (has a default resolution)
	symImport                    // __imp_* or thunk from a short import stub
	symCommon                    // tentative (IMAGE_SYM_UNDEFINED with nonzero Value)
	symAbsolute                  // IMAGE_SYM_ABSOLUTE (-1)
	symDefined                   // hard definition in a concrete section
)

// globalSym is one entry in the global symbol table.
type globalSym struct {
	kind    symKind
	name    string
	value   uint32 // raw Value from symbol table
	va      uint64 // virtual address — filled by resolveSymbolAddresses
	// For symDefined / symCommon:
	obj    *ObjectFile
	secIdx int    // 0-based section index within obj.Sections
	// For symWeak:
	weakDefault string
	weakChars   uint32
	// For symImport (__imp_*):
	imp    *CollectedImport
	impSym *CollectedImportSym
	// For symAbsolute:
	absVal uint32
}

// SymbolTable is the global linker symbol table.
type SymbolTable struct {
	syms    map[string]*globalSym
	objs    []*ObjectFile
	imports []*CollectedImport // deduplicated DLL imports
	exports []ExportRecord
}

// ExportRecord is one symbol to be exported from the output image.
type ExportRecord struct {
	ExportName   string
	InternalName string
	Ordinal      uint16
	IsData       bool
}

func newSymbolTable() *SymbolTable {
	return &SymbolTable{syms: make(map[string]*globalSym)}
}

func (st *SymbolTable) objects() []*ObjectFile { return st.objs }

// get returns the symbol entry, creating an Undefined one if absent.
func (st *SymbolTable) get(name string) *globalSym {
	if s, ok := st.syms[name]; ok {
		return s
	}
	s := &globalSym{kind: symUndefined, name: name}
	st.syms[name] = s
	return s
}

// define attempts to define sym. Returns an error for hard conflicts.
func (st *SymbolTable) define(sym *globalSym, incoming *globalSym) error {
	switch {
	case incoming.kind < sym.kind:
		return nil // incoming is weaker, keep existing
	case incoming.kind > sym.kind:
		*sym = *incoming
		return nil
	}
	// Same kind — resolve conflict.
	switch sym.kind {
	case symDefined:
		// Two hard definitions.
		if sym.obj == incoming.obj && sym.secIdx == incoming.secIdx {
			return nil // same section, idempotent
		}
		// Check if both are COMDAT.
		sec := sym.obj.Sections[sym.secIdx]
		inSec := incoming.obj.Sections[incoming.secIdx]
		if sec.IsComdat && inSec.IsComdat {
			return resolveComdat(sym, incoming, sec, inSec)
		}
		return fmt.Errorf("duplicate definition of %q", sym.name)
	case symCommon:
		// Keep the larger common block.
		if incoming.value > sym.value {
			*sym = *incoming
		}
	case symWeak:
		// Keep first weak definition.
	}
	return nil
}

func resolveComdat(keep, incoming *globalSym, keepSec, inSec *RawSection) error {
	sel := keepSec.ComdatSel
	if sel == 0 {
		sel = inSec.ComdatSel
	}
	switch sel {
	case 1: // IMAGE_COMDAT_SELECT_NODUPLICATES
		return fmt.Errorf("duplicate COMDAT %q (SELECT_NODUPLICATES)", keep.name)
	case 2: // IMAGE_COMDAT_SELECT_ANY
		return nil // keep existing
	case 3: // IMAGE_COMDAT_SELECT_SAME_SIZE
		ksz := uint32(len(keepSec.Data))
		isz := uint32(len(inSec.Data))
		if ksz != isz {
			return fmt.Errorf("COMDAT size mismatch for %q: %d vs %d", keep.name, ksz, isz)
		}
		return nil
	case 4: // IMAGE_COMDAT_SELECT_EXACT_MATCH
		if string(keepSec.Data) != string(inSec.Data) {
			return fmt.Errorf("COMDAT content mismatch for %q", keep.name)
		}
		return nil
	case 5: // IMAGE_COMDAT_SELECT_ASSOCIATIVE
		return nil // leader governs
	case 6: // IMAGE_COMDAT_SELECT_LARGEST
		if len(inSec.Data) > len(keepSec.Data) {
			*keep = *incoming
		}
		return nil
	default:
		return nil // unknown — keep first
	}
}

// ingestObject ingests one ObjectFile into the symbol table.
func (st *SymbolTable) ingestObject(obj *ObjectFile) error {
	st.objs = append(st.objs, obj)
	for i, sym := range obj.Symbols {
		if sym.StorageClass == 255 { // aux record placeholder
			continue
		}
		if sym.Name == "" {
			continue
		}
		if sym.StorageClass != 2 && sym.StorageClass != 105 {
			// Not IMAGE_SYM_CLASS_EXTERNAL or WEAK_EXTERNAL — local/special symbol.
			continue
		}

		gs := st.get(sym.Name)

		switch {
		case sym.SectionNumber == 0 && sym.Value == 0:
			// Undefined reference — just ensure entry exists (already done by get).
		case sym.SectionNumber == 0 && sym.Value != 0:
			// Common block.
			if err := st.define(gs, &globalSym{
				kind: symCommon, name: sym.Name,
				value: sym.Value, obj: obj, secIdx: i,
			}); err != nil {
				return err
			}
		case sym.SectionNumber == -1:
			// Absolute symbol.
			if err := st.define(gs, &globalSym{
				kind: symAbsolute, name: sym.Name, absVal: sym.Value,
			}); err != nil {
				return err
			}
		case sym.SectionNumber == -2:
			// Debug symbol — ignore.
		case sym.SectionNumber > 0:
			secIdx := int(sym.SectionNumber) - 1
			if secIdx >= len(obj.Sections) {
				continue
			}
			if err := st.define(gs, &globalSym{
				kind: symDefined, name: sym.Name,
				value: sym.Value, obj: obj, secIdx: secIdx,
			}); err != nil {
				return err
			}
		case sym.StorageClass == 105: // WEAK_EXTERNAL
			defSym := ""
			if sym.WeakDefaultIdx < len(obj.Symbols) {
				defSym = obj.Symbols[sym.WeakDefaultIdx].Name
			}
			if gs.kind == symUndefined {
				gs.kind = symWeak
				gs.weakDefault = defSym
				gs.weakChars = sym.WeakChars
			}
		}
	}
	return nil
}

// ingestObjects ingests a slice of objects.
func (st *SymbolTable) ingestObjects(objs []*ObjectFile) error {
	for _, obj := range objs {
		if err := st.ingestObject(obj); err != nil {
			return err
		}
		// Collect exports from .drectve.
		st.collectDrectveExports(obj)
	}
	return nil
}

// ingestArchive performs one pass of archive extraction.
// It extracts any member whose defined symbol resolves an existing undefined.
func (st *SymbolTable) ingestArchive(ar *Archive) error {
	for _, m := range ar.Members {
		if m.imp != nil {
			st.ingestImportStub(m.imp)
			continue
		}
		// Check if any symbol in this member is needed.
		obj, err := m.Object()
		if err != nil || obj == nil {
			continue
		}
		needed := false
		for _, sym := range obj.Symbols {
			if sym.StorageClass != 2 || sym.SectionNumber <= 0 {
				continue
			}
			if gs, ok := st.syms[sym.Name]; ok && gs.kind == symUndefined {
				needed = true
				break
			}
		}
		if !needed {
			continue
		}
		// Check not already ingested.
		already := false
		for _, o := range st.objs {
			if o == obj {
				already = true
				break
			}
		}
		if !already {
			if err := st.ingestObject(obj); err != nil {
				return err
			}
			st.collectDrectveExports(obj)
		}
	}
	return nil
}

// ingestImportStub registers __imp_sym (and a thunk sym) for a short import stub.
func (st *SymbolTable) ingestImportStub(imp *ShortImport) {
	// Find or create the CollectedImport for this DLL.
	var ci *CollectedImport
	for _, c := range st.imports {
		if strings.EqualFold(c.DLL, imp.DLL) {
			ci = c
			break
		}
	}
	if ci == nil {
		ci = &CollectedImport{DLL: imp.DLL}
		st.imports = append(st.imports, ci)
	}

	// Add the symbol if not already present.
	for i := range ci.Symbols {
		if ci.Symbols[i].Name == imp.SymName && ci.Symbols[i].ImportType == imp.ImportType {
			// Already registered; ensure __imp_ entry exists in global table.
			gs := st.get("__imp_" + imp.SymName)
			if gs.kind == symUndefined {
				gs.kind = symImport
				gs.imp = ci
				gs.impSym = &ci.Symbols[i]
			}
			return
		}
	}

	ci.Symbols = append(ci.Symbols, CollectedImportSym{
		Name:       imp.SymName,
		Ordinal:    imp.Ordinal,
		ImportType: imp.ImportType,
	})
	csym := &ci.Symbols[len(ci.Symbols)-1]

	// Define __imp_sym.
	impName := "__imp_" + imp.SymName
	gs := st.get(impName)
	if gs.kind <= symImport {
		gs.kind = symImport
		gs.name = impName
		gs.imp = ci
		gs.impSym = csym
	}

	// For IMPORT_CODE (0) and IMPORT_DATA (1): also define the undecorated symbol.
	if imp.ImportType == 0 || imp.ImportType == 1 {
		ts := st.get(imp.SymName)
		if ts.kind == symUndefined {
			ts.kind = symImport // thunk placeholder
			ts.name = imp.SymName
			ts.imp = ci
			ts.impSym = csym
		}
	}
}

func (st *SymbolTable) collectDrectveExports(obj *ObjectFile) {
	for _, de := range obj.Exports {
		already := false
		for _, er := range st.exports {
			if er.InternalName == de.InternalName {
				already = true
				break
			}
		}
		if !already {
			st.exports = append(st.exports, ExportRecord{
				ExportName:   de.ExportName,
				InternalName: de.InternalName,
				IsData:       de.IsData,
			})
		}
	}
}

// collectImports returns the deduplicated import list.
func (st *SymbolTable) collectImports() []*CollectedImport { return st.imports }

// collectExports returns all exports (filtered to those with resolved symbols).
func (st *SymbolTable) collectExports(l *Layout) []ExportRecord { return st.exports }

// undefinedCount returns the number of strong undefined symbols.
func (st *SymbolTable) undefinedCount() int {
	n := 0
	for _, gs := range st.syms {
		if gs.kind == symUndefined {
			n++
		}
	}
	return n
}

// checkUndefined returns an error if any strong undefined symbol remains.
func (st *SymbolTable) checkUndefined() error {
	for name, gs := range st.syms {
		if gs.kind == symUndefined {
			return fmt.Errorf("undefined reference to %q", name)
		}
	}
	return nil
}

// Lookup returns the VA of name, or 0 if not found/resolved.
func (st *SymbolTable) Lookup(name string) uint64 {
	if gs, ok := st.syms[name]; ok {
		return gs.va
	}
	return 0
}