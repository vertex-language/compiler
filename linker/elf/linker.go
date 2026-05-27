// linker.go
// Package elf implements a static linker for ELF64 binaries.
//
// It reads ET_REL object files and .a archives, resolves symbols, merges
// sections, patches RELA relocations, and drives github.com/vertex-language/
// compiler/bin/elf to produce the final binary.
//
// Pipeline:
//
//   [.o / .a / .so inputs]
//         │
//         ▼
//   ParseObject / ParseArchive / ParseShared   (reader layer — no debug/elf)
//         │
//         ▼
//   SymbolTable.Ingest()                        (resolution + archive extraction)
//         │
//         ▼
//   MergeSections()                             (combine same-named sections)
//         │
//         ▼
//   AssignLayout()                              (virtual address + file offset)
//         │
//         ▼
//   ResolveSymbolAddresses()                    (vaddr per symbol)
//         │
//         ▼
//   PatchRelocations()                          (apply RELA formulas)
//         │
//         ▼
//   LinkResult.Builder() → bin/elf.Emit()       (serialise)
package elf

import (
	"fmt"
	"os"
	"path/filepath"

	binelf "github.com/vertex-language/compiler/bin/elf"
)

// Linker coordinates all linking phases.
type Linker struct {
	arch       uint16
	outputType OutputType
	entry      string
	interp     string
	soname     string
	rpath      string
	libPaths   []string

	objects  []*ObjectFile
	archives []*Archive
	shared   []*SharedLib

	// EFlags to forward to the bin/elf builder (required for RISC-V).
	eflags uint32
}

// NewLinker returns a Linker targeting the given ELF machine value
// (EM_X86_64=0x3E, EM_AARCH64=0xB7, EM_RISCV=0xF3).
// The default output type is OutputExec.
func NewLinker(machine uint16) *Linker {
	return &Linker{
		arch:       machine,
		outputType: OutputExec,
	}
}

func (l *Linker) SetOutputType(t OutputType) { l.outputType = t }
func (l *Linker) SetEntry(name string)        { l.entry = name }
func (l *Linker) SetInterp(path string)       { l.interp = path }
func (l *Linker) SetSoname(name string)       { l.soname = name }
func (l *Linker) SetRpath(path string)        { l.rpath = path }
func (l *Linker) SetEFlags(f uint32)          { l.eflags = f }
func (l *Linker) AddLibraryPath(path string)  { l.libPaths = append(l.libPaths, path) }
func (l *Linker) AddObject(o *ObjectFile)     { l.objects = append(l.objects, o) }
func (l *Linker) AddArchive(a *Archive)       { l.archives = append(l.archives, a) }
func (l *Linker) AddShared(s *SharedLib)      { l.shared = append(l.shared, s) }

// Link runs all phases and returns a LinkResult.
func (l *Linker) Link() (*LinkResult, error) {
	// ── Phase 1: transitive shared library dependency walk ────────────────────
	if err := l.walkSharedDeps(); err != nil {
		return nil, fmt.Errorf("link: dependency walk: %w", err)
	}

	// ── Phase 2: symbol resolution ────────────────────────────────────────────
	symtab := newSymbolTable()
	if err := symtab.Ingest(l.objects, l.archives, l.shared); err != nil {
		return nil, fmt.Errorf("link: symbol resolution: %w", err)
	}

	// Collect all object files that ended up contributing (direct + extracted).
	allObjects := l.collectExtractedObjects()

	// ── Phase 3: section merging ──────────────────────────────────────────────
	layout, err := MergeSections(allObjects)
	if err != nil {
		return nil, fmt.Errorf("link: section merge: %w", err)
	}

	// ── Phase 4: virtual address layout ──────────────────────────────────────
	if err := AssignLayout(l.outputType, layout); err != nil {
		return nil, fmt.Errorf("link: layout: %w", err)
	}

	// ── Phase 5: resolve symbol addresses ─────────────────────────────────────
	if err := ResolveSymbolAddresses(symtab, layout); err != nil {
		return nil, fmt.Errorf("link: symbol address resolution: %w", err)
	}

	// ── Phase 6: relocation patching ──────────────────────────────────────────
	if err := PatchRelocations(l.arch, layout, symtab, allObjects); err != nil {
		return nil, fmt.Errorf("link: relocation patching: %w", err)
	}

	// ── Phase 7: build result ─────────────────────────────────────────────────

	result := &LinkResult{
		Arch:       binelf.Arch(l.arch),
		OutputType: l.outputType,
		Entry:      l.entry,
		Interp:     l.interp,
		Soname:     l.soname,
		Rpath:      l.rpath,
		Needed:     collectNeeded(l.shared),
		Layout:     layout,
		Symtab:     symtab,
		Machine:    l.arch,
		EFlags:     l.eflags,
	}
	return result, nil
}

// ── Shared library dependency walk ────────────────────────────────────────────

// walkSharedDeps performs a BFS over DT_NEEDED entries, loading transitive
// dependencies from l.libPaths and the embedding library's own DT_RPATH/DT_RUNPATH.
func (l *Linker) walkSharedDeps() error {
	seen := make(map[string]bool)

	// Seed with directly-added libs.
	queue := make([]*SharedLib, len(l.shared))
	copy(queue, l.shared)
	for _, s := range l.shared {
		seen[s.Soname()] = true
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, soname := range cur.Needed() {
			if seen[soname] {
				continue
			}
			seen[soname] = true

			dep, err := l.findShared(soname, cur.Rpaths())
			if err != nil {
				return fmt.Errorf("loading %s (needed by %s): %w",
					soname, cur.Soname(), err)
			}
			l.shared = append(l.shared, dep)
			queue = append(queue, dep)
		}
	}
	return nil
}

// findShared searches for soname in the embedding library's rpaths, then
// l.libPaths, following Linux ld.so search conventions.
func (l *Linker) findShared(soname string, rpaths []string) (*SharedLib, error) {
	// 1. Library's own DT_RPATH / DT_RUNPATH
	for _, rp := range rpaths {
		candidate := filepath.Join(rp, soname)
		if _, err := os.Stat(candidate); err == nil {
			return OpenShared(candidate)
		}
	}
	// 2. Linker's -rpath / library search paths
	for _, lp := range l.libPaths {
		candidate := filepath.Join(lp, soname)
		if _, err := os.Stat(candidate); err == nil {
			return OpenShared(candidate)
		}
	}
	// 3. System default paths
	defaults := []string{
		"/lib/x86_64-linux-gnu",
		"/usr/lib/x86_64-linux-gnu",
		"/lib64",
		"/usr/lib64",
		"/lib",
		"/usr/lib",
	}
	for _, dp := range defaults {
		candidate := filepath.Join(dp, soname)
		if _, err := os.Stat(candidate); err == nil {
			return OpenShared(candidate)
		}
	}
	return nil, fmt.Errorf("shared library %q not found (searched %v + system paths)",
		soname, append(rpaths, l.libPaths...))
}

// collectExtractedObjects returns the union of directly-added objects and
// all archive members that were extracted during symbol resolution.
func (l *Linker) collectExtractedObjects() []*ObjectFile {
	out := make([]*ObjectFile, len(l.objects))
	copy(out, l.objects)

	for _, ar := range l.archives {
		for _, m := range ar.Members {
			if m.obj != nil {
				out = append(out, m.obj)
			}
		}
	}
	return out
}