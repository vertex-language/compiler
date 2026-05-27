package macho

import (
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────────────
// Symbol kinds (resolution precedence)
// ──────────────────────────────────────────────────────────────────────────────

type symKind int

const (
	kindUndef   symKind = iota
	kindLazy            // archive member not yet extracted
	kindDylib           // exported by a .dylib
	kindCommon          // N_UNDF + Value > 0 (tentative)
	kindDefined         // hard definition from an .o file
)

// ──────────────────────────────────────────────────────────────────────────────
// ResolvedSym — one entry in the unified symbol table
// ──────────────────────────────────────────────────────────────────────────────

// ResolvedSym is the linker's internal representation of a symbol after
// resolution.
type ResolvedSym struct {
	Name    string
	Kind    symKind
	IsWeak  bool // weak definition or weak reference
	IsGlobal bool

	// kindDefined / kindCommon
	Obj         *ObjectFile
	RawSym      *RawSymbol
	SectionName string
	SegmentName string
	Value       uint64 // n_value (offset within section for N_SECT; size for common)

	// kindDylib
	Dylib       *DylibFile
	DylibSym    *DylibSymbol
	LibOrdinal  int // 1-based dylib ordinal in the output LC_LOAD_DYLIB list

	// kindDefined: resolved virtual address (filled by ResolveSymbolAddresses)
	VAddr uint64

	// IsAbs: absolute symbol (N_ABS), VAddr = Value unchanged
	IsAbs bool
}

// ──────────────────────────────────────────────────────────────────────────────
// SymbolTable
// ──────────────────────────────────────────────────────────────────────────────

// SymbolTable is the linker's unified symbol resolution table.
type SymbolTable struct {
	syms map[string]*ResolvedSym
}

// NewSymbolTable returns an empty SymbolTable.
func NewSymbolTable() *SymbolTable {
	return &SymbolTable{syms: make(map[string]*ResolvedSym)}
}

// Lookup returns the resolved symbol for name, or nil.
func (t *SymbolTable) Lookup(name string) *ResolvedSym {
	return t.syms[name]
}

// All returns all resolved symbols (order is not defined).
func (t *SymbolTable) All() []*ResolvedSym {
	out := make([]*ResolvedSym, 0, len(t.syms))
	for _, s := range t.syms {
		out = append(out, s)
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────────────
// Ingest
// ──────────────────────────────────────────────────────────────────────────────

// Ingest processes all inputs and populates the symbol table.
//
// objects, archives, and dylibs are processed in their given order.
// Archives are extracted in a fixed-point loop until no new members are added.
// dylibOrdinal maps each DylibFile to its 1-based ordinal in the output
// LC_LOAD_DYLIB list (must be pre-computed by the caller).
func (t *SymbolTable) Ingest(
	objects []*ObjectFile,
	archives []*Archive,
	dylibs []*DylibFile,
	dylibOrdinals map[*DylibFile]int,
) ([]*ObjectFile, error) {

	// 1. Register all direct object files.
	for _, obj := range objects {
		if err := t.ingestObject(obj); err != nil {
			return nil, err
		}
	}

	// 2. Register dylib exports as kindDylib (weakest of defined kinds).
	for _, d := range dylibs {
		ord := dylibOrdinals[d]
		for _, sym := range d.symbols {
			t.offerDylib(sym.Name, d, sym, ord)
		}
	}

	// 3. Archive extraction fixed-point loop.
	active := objects
	changed := true
	for changed {
		changed = false
		for _, arc := range archives {
			// Check every undefined symbol against this archive.
			for name, rs := range t.syms {
				if rs.Kind != kindUndef && rs.Kind != kindLazy {
					continue
				}
				if rs.IsWeak {
					continue // weak undef never triggers extraction
				}
				m := arc.MemberForSymbol(name)
				if m == nil {
					continue
				}
				obj, err := m.Object()
				if err != nil {
					return nil, fmt.Errorf("archive %s member %s: %w", arc.Path, m.Name, err)
				}
				if err := t.ingestObject(obj); err != nil {
					return nil, err
				}
				active = append(active, obj)
				changed = true
			}
		}
	}

	// 4. Report unresolved strong undefineds.
	for name, rs := range t.syms {
		if rs.Kind == kindUndef && !rs.IsWeak {
			return nil, fmt.Errorf("undefined reference to %q", name)
		}
	}

	return active, nil
}

func (t *SymbolTable) ingestObject(obj *ObjectFile) error {
	for _, s := range obj.Symbols {
		if s.IsDebug || (!s.IsGlobal && !s.IsPrivExt) {
			continue // skip local and debug symbols
		}
		if err := t.offer(s, obj); err != nil {
			return err
		}
	}
	return nil
}

// offer attempts to resolve a symbol from an object file symbol entry.
func (t *SymbolTable) offer(raw *RawSymbol, obj *ObjectFile) error {
	name := raw.Name
	incoming := &ResolvedSym{
		Name:     name,
		IsWeak:   raw.IsWeak,
		IsGlobal: raw.IsGlobal,
		Obj:      obj,
		RawSym:   raw,
	}

	switch {
	case raw.IsAbs:
		incoming.Kind = kindDefined
		incoming.IsAbs = true
		incoming.Value = raw.Value
	case raw.IsCommon:
		incoming.Kind = kindCommon
		incoming.Value = raw.Value // Value = size for common
		incoming.SectionName = "__common"
		incoming.SegmentName = "__DATA"
	case raw.IsUndef:
		incoming.Kind = kindUndef
	default:
		// Defined N_SECT
		incoming.Kind = kindDefined
		incoming.SectionName = raw.SectionName
		incoming.SegmentName = raw.SegmentName
		incoming.Value = raw.Value
	}

	existing, ok := t.syms[name]
	if !ok {
		t.syms[name] = incoming
		return nil
	}

	return t.merge(existing, incoming)
}

func (t *SymbolTable) offerDylib(name string, d *DylibFile, sym *DylibSymbol, ord int) {
	incoming := &ResolvedSym{
		Name:       name,
		Kind:       kindDylib,
		IsWeak:     sym.IsWeak,
		IsGlobal:   true,
		Dylib:      d,
		DylibSym:   sym,
		LibOrdinal: ord,
	}
	existing, ok := t.syms[name]
	if !ok {
		t.syms[name] = incoming
		return
	}
	// Dylib only wins over Undef / Lazy.
	if existing.Kind < kindDylib {
		t.syms[name] = incoming
	}
}

// merge resolves a conflict between existing and incoming.
func (t *SymbolTable) merge(existing, incoming *ResolvedSym) error {
	// Higher kind always wins.
	if incoming.Kind > existing.Kind {
		t.syms[incoming.Name] = incoming
		return nil
	}
	if incoming.Kind < existing.Kind {
		return nil // existing wins
	}

	// Same kind.
	switch existing.Kind {
	case kindDefined:
		// Both are hard definitions.
		if !existing.IsWeak && !incoming.IsWeak {
			return fmt.Errorf("duplicate definition of %q", existing.Name)
		}
		if !existing.IsWeak && incoming.IsWeak {
			return nil // existing strong wins
		}
		if existing.IsWeak && !incoming.IsWeak {
			t.syms[incoming.Name] = incoming // incoming strong wins
			return nil
		}
		// Both weak: first wins.
		return nil

	case kindCommon:
		// Larger common block wins.
		if incoming.Value > existing.Value {
			t.syms[incoming.Name] = incoming
		}
		return nil

	case kindDylib:
		// First dylib wins (two-level namespace: same name in different dylibs = ambiguous; keep first).
		return nil

	default:
		t.syms[incoming.Name] = incoming
		return nil
	}
}