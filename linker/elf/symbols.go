// symbols.go
package elf

import "fmt"

// ── Symbol kinds (precedence order, low → high) ───────────────────────────────

type symKind int

const (
	kindUndefined symKind = iota // referenced but not yet defined
	kindLazy                     // definition available in an archive member (not yet extracted)
	kindShared                   // defined in a shared library
	kindCommon                   // tentative C common block
	kindDefined                  // hard definition in a relocatable object file
)

// ── Global symbol entry ───────────────────────────────────────────────────────

// Symbol is one entry in the linker's global symbol table.
type Symbol struct {
	Name string
	Kind symKind

	// For kindDefined / kindCommon:
	Object      *ObjectFile
	RawSym      *RawSymbol

	// For kindLazy:
	Archive     *Archive
	Member      *ArchiveMember

	// For kindShared:
	Lib         *SharedLib
	DynSym      *DynSymbol

	// Filled in after layout: virtual address of this symbol.
	VAddr uint64

	// Weak is true if the original binding was STB_WEAK.
	Weak bool
}

// IsDefined reports whether the symbol has a concrete definition that
// produces output bytes (i.e. kindDefined or kindCommon).
func (s *Symbol) IsDefined() bool {
	return s.Kind == kindDefined || s.Kind == kindCommon
}

// IsUndefined reports whether the symbol still needs resolution.
func (s *Symbol) IsUndefined() bool {
	return s.Kind == kindUndefined
}

// IsShared reports whether the symbol is satisfied by a shared library.
func (s *Symbol) IsShared() bool {
	return s.Kind == kindShared
}

// ── Symbol table ──────────────────────────────────────────────────────────────

// SymbolTable is the linker's global symbol table.
// It enforces the ELF resolution rules:
//
//   Defined   > Common > Shared > Lazy > Undefined
//   STB_GLOBAL overrides STB_WEAK among defined symbols
//   Two STB_GLOBAL definitions → duplicate definition error
//   STB_WEAK undefined → resolves to zero, no error
type SymbolTable struct {
	entries map[string]*Symbol
	// undefined symbols referenced by at least one .o (not just .so)
	// — these drive archive member extraction
	objUndefs map[string]bool
}

func newSymbolTable() *SymbolTable {
	return &SymbolTable{
		entries:   make(map[string]*Symbol),
		objUndefs: make(map[string]bool),
	}
}

// Lookup returns the symbol entry for name, or nil.
func (t *SymbolTable) Lookup(name string) *Symbol {
	return t.entries[name]
}

// All returns every symbol in the table.
func (t *SymbolTable) All() []*Symbol {
	out := make([]*Symbol, 0, len(t.entries))
	for _, s := range t.entries {
		out = append(out, s)
	}
	return out
}

// ── Ingest: process all inputs in command-line order ─────────────────────────

// Ingest processes all inputs, performing symbol resolution and archive
// member extraction. Processing order matches the classical Unix linker
// left-to-right command-line semantics.
func (t *SymbolTable) Ingest(objects []*ObjectFile, archives []*Archive, shared []*SharedLib) error {
	// Step 1: process all direct object files
	for _, obj := range objects {
		if err := t.ingestObject(obj); err != nil {
			return err
		}
	}

	// Step 2: process shared libraries — contribute symbols without extraction
	for _, lib := range shared {
		t.ingestShared(lib)
	}

	// Step 3: archive extraction loop — repeat until stable
	// A single pass is insufficient when archive members have cross-dependencies.
	for {
		extracted := false
		for _, ar := range archives {
			n, err := t.extractFromArchive(ar)
			if err != nil {
				return err
			}
			if n > 0 {
				extracted = true
			}
		}
		if !extracted {
			break
		}
	}

	// Step 4: error on unresolved strong undefined symbols
	for name, sym := range t.entries {
		if sym.Kind == kindUndefined && !sym.Weak {
			if t.objUndefs[name] {
				return fmt.Errorf("undefined reference to %q", name)
			}
		}
	}

	return nil
}

// ingestObject processes one relocatable object file.
func (t *SymbolTable) ingestObject(obj *ObjectFile) error {
	for _, raw := range obj.Symbols {
		if raw.Bind == stbLocal || raw.Name == "" {
			continue // local symbols don't enter the global table
		}

		name := raw.Name

		// Track undefined references from object files (drives archive extraction).
		if raw.ShndxRaw == shnUndef {
			t.objUndefs[name] = true
			t.ensureUndefined(name, raw.Bind == stbWeak)
			continue
		}

		// Common block: STB_GLOBAL + SHN_COMMON
		if raw.ShndxRaw == shnCommon {
			if err := t.resolveCommon(name, raw, obj); err != nil {
				return err
			}
			continue
		}

		// Hard definition
		if err := t.resolveDefinition(name, raw, obj); err != nil {
			return err
		}
	}
	return nil
}

// ingestShared registers all exported symbols from a shared library.
// Shared definitions only win over Undefined/Lazy entries, never over Defined.
func (t *SymbolTable) ingestShared(lib *SharedLib) {
	for name, dynSym := range lib.symbols {
		if dynSym.Bind != stbGlobal && dynSym.Bind != stbWeak {
			continue
		}
		existing := t.entries[name]
		if existing == nil || existing.Kind == kindUndefined || existing.Kind == kindLazy {
			t.entries[name] = &Symbol{
				Name:   name,
				Kind:   kindShared,
				Lib:    lib,
				DynSym: dynSym,
				Weak:   dynSym.Bind == stbWeak,
			}
		}
	}
}

// extractFromArchive iterates the archive's symbol index and extracts any
// member that resolves a currently-undefined STB_GLOBAL symbol.
// Returns the number of members extracted.
func (t *SymbolTable) extractFromArchive(ar *Archive) (int, error) {
	extracted := 0
	// We iterate over objUndefs rather than the full symtab to avoid
	// extracting members that only resolve shared-lib-only undefs.
	for name := range t.objUndefs {
		sym := t.entries[name]
		if sym == nil || sym.Kind != kindUndefined {
			continue
		}
		if sym.Weak {
			// Weak undefined symbols do NOT trigger archive extraction.
			continue
		}
		m := ar.MemberForSymbol(name)
		if m == nil {
			continue
		}
		obj, err := m.Object()
		if err != nil {
			return extracted, fmt.Errorf("extracting %q from %s: %w", name, ar.Path, err)
		}
		if err := t.ingestObject(obj); err != nil {
			return extracted, err
		}
		extracted++
	}
	return extracted, nil
}

// ── Resolution helpers ────────────────────────────────────────────────────────

func (t *SymbolTable) ensureUndefined(name string, weak bool) {
	existing := t.entries[name]
	if existing == nil {
		t.entries[name] = &Symbol{Name: name, Kind: kindUndefined, Weak: weak}
	}
}

func (t *SymbolTable) resolveDefinition(name string, raw *RawSymbol, obj *ObjectFile) error {
	incoming := &Symbol{
		Name:   name,
		Kind:   kindDefined,
		Object: obj,
		RawSym: raw,
		Weak:   raw.Bind == stbWeak,
	}

	existing := t.entries[name]
	if existing == nil {
		t.entries[name] = incoming
		return nil
	}

	switch existing.Kind {
	case kindUndefined, kindLazy, kindShared:
		// Any definition beats undefined/lazy/shared.
		t.entries[name] = incoming

	case kindCommon:
		// A hard definition beats a common block.
		t.entries[name] = incoming

	case kindDefined:
		existWeak := existing.Weak
		incomingWeak := raw.Bind == stbWeak

		switch {
		case existWeak && !incomingWeak:
			// Incoming strong beats existing weak.
			t.entries[name] = incoming
		case !existWeak && incomingWeak:
			// Existing strong wins; incoming weak is silently dropped.
		case existWeak && incomingWeak:
			// Weak vs weak: first one wins (matching ld/lld behavior).
		default:
			// Strong vs strong: duplicate definition error.
			return fmt.Errorf("duplicate definition of %q (in %s and %s)",
				name, existing.Object.Path, obj.Path)
		}
	}
	return nil
}

func (t *SymbolTable) resolveCommon(name string, raw *RawSymbol, obj *ObjectFile) error {
	incoming := &Symbol{
		Name:   name,
		Kind:   kindCommon,
		Object: obj,
		RawSym: raw,
	}

	existing := t.entries[name]
	if existing == nil {
		t.entries[name] = incoming
		return nil
	}

	switch existing.Kind {
	case kindUndefined, kindLazy, kindShared:
		t.entries[name] = incoming
	case kindCommon:
		// Larger common block wins (standard C behavior).
		if raw.Size > existing.RawSym.Size {
			t.entries[name] = incoming
		}
	case kindDefined:
		// Hard definition beats common; drop incoming.
	}
	return nil
}