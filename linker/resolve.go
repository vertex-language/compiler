// Package linker — resolve.go
// Global symbol table: tracks defined symbols (in any section), TLS symbols,
// and unresolved references.
package linker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vertex-language/compiler/object"
)

// symDef is a resolved symbol: the section it lives in and its byte offset
// within that merged section buffer.
type symDef struct {
	section object.SymSection
	off     int
}

// symTable is the linker's global symbol table.
type symTable struct {
	defs    map[string]symDef // defined symbols (all non-TLS sections)
	tlsDefs map[string]int    // TLS symbols: name → TLS-template byte offset
	refs    map[string]bool   // symbols referenced but not yet defined
}

func newSymTable() symTable {
	return symTable{
		defs:    make(map[string]symDef),
		tlsDefs: make(map[string]int),
		refs:    make(map[string]bool),
	}
}

// define records a defined symbol.  Returns an error on duplicate definition.
// Use this for directly-supplied objects where a duplicate is a hard error.
func (st *symTable) define(name string, section object.SymSection, off int) error {
	if _, dup := st.defs[name]; dup {
		return fmt.Errorf("duplicate symbol %q", name)
	}
	st.defs[name] = symDef{section: section, off: off}
	delete(st.refs, name)
	return nil
}

// tryDefine is like define but silently keeps the first definition on
// duplicate, returning false rather than an error.  Use this when ingesting
// archive members, where first-definition-wins is correct linker behaviour:
// every TU in libc may emit its own copy of GCC-internal symbols such as
// DW.ref.__gcc_personality_v0 and the first one pulled in wins.
func (st *symTable) tryDefine(name string, section object.SymSection, off int) (bool, error) {
	if _, dup := st.defs[name]; dup {
		return false, nil
	}
	st.defs[name] = symDef{section: section, off: off}
	delete(st.refs, name)
	return true, nil
}

// defineTLS records a TLS symbol at the given TLS-template byte offset.
// Returns an error on duplicate definition.
func (st *symTable) defineTLS(name string, tlsOff int) error {
	if _, dup := st.tlsDefs[name]; dup {
		return fmt.Errorf("duplicate TLS symbol %q", name)
	}
	st.tlsDefs[name] = tlsOff
	delete(st.refs, name)
	return nil
}

// tryDefineTLS is like defineTLS but silently keeps the first definition on
// duplicate.  Used when ingesting archive members.
func (st *symTable) tryDefineTLS(name string, tlsOff int) (bool, error) {
	if _, dup := st.tlsDefs[name]; dup {
		return false, nil
	}
	st.tlsDefs[name] = tlsOff
	delete(st.refs, name)
	return true, nil
}

// reference marks name as needed if it is not already defined.
func (st *symTable) reference(name string) {
	if _, ok := st.defs[name]; ok {
		return
	}
	if _, ok := st.tlsDefs[name]; ok {
		return
	}
	st.refs[name] = true
}

// offset returns the byte offset within the merged .text buffer for a
// text-defined symbol.  Used for computing the entry-point offset.
func (st *symTable) offset(name string) (int, bool) {
	d, ok := st.defs[name]
	if !ok || d.section != object.SymSecText {
		return 0, false
	}
	return d.off, ok
}

// tlsOffset returns the TLS-template byte offset for a TLS symbol.
func (st *symTable) tlsOffset(name string) (int, bool) {
	off, ok := st.tlsDefs[name]
	return off, ok
}

// defined returns a snapshot of text-only defined symbols (name → .text
// offset) for use by the ELF symbol table emitter.
func (st *symTable) defined() map[string]int {
	out := make(map[string]int, len(st.defs))
	for name, d := range st.defs {
		if d.section == object.SymSecText {
			out[name] = d.off
		}
	}
	return out
}

// checkResolved returns an error listing every symbol that was referenced
// but never defined.
func (lnk *linker) checkResolved() error {
	if len(lnk.sym.refs) == 0 {
		return nil
	}
	missing := make([]string, 0, len(lnk.sym.refs))
	for name := range lnk.sym.refs {
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return fmt.Errorf("linker: unresolved symbols:\n\t%s", strings.Join(missing, "\n\t"))
}