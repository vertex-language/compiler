package macho

import (
	"fmt"
	"os"
	"path/filepath"

	binmacho "github.com/vertex-language/compiler/bin/macho"
)

// ──────────────────────────────────────────────────────────────────────────────
// Linker
// ──────────────────────────────────────────────────────────────────────────────

// Linker is the top-level facade for the Mach-O static linker.
type Linker struct {
	arch        uint32
	outputType  OutputType
	entry       string
	installName string
	dyldMode    binmacho.DyldMode
	platform    binmacho.BuildVersion
	rpaths      []string
	libPaths    []string

	objects  []*ObjectFile
	archives []*Archive
	dylibs   []*DylibFile
}

// NewLinker returns a Linker for the given Mach-O cputype.
func NewLinker(arch uint32) *Linker {
	return &Linker{
		arch:       arch,
		outputType: OutputExec,
		dyldMode:   binmacho.DyldModeChained,
		platform: binmacho.BuildVersion{
			Platform: binmacho.PlatformMacOS,
			MinOS:    binmacho.PackVersion(14, 0, 0),
			SDK:      binmacho.PackVersion(14, 5, 0),
		},
	}
}

// SetOutputType sets the Mach-O output file type.
func (l *Linker) SetOutputType(t OutputType) { l.outputType = t }

// SetEntry sets the entry-point symbol name for MH_EXECUTE output.
func (l *Linker) SetEntry(name string) { l.entry = name }

// SetInstallName sets the dylib install name (LC_ID_DYLIB) for dylib output.
func (l *Linker) SetInstallName(name string) { l.installName = name }

// SetDyldMode selects chained fixups (default) or legacy dyld info.
func (l *Linker) SetDyldMode(m binmacho.DyldMode) { l.dyldMode = m }

// SetPlatform sets the LC_BUILD_VERSION platform and version triple.
func (l *Linker) SetPlatform(p binmacho.Platform, minOS, sdk uint32) {
	l.platform = binmacho.BuildVersion{Platform: p, MinOS: minOS, SDK: sdk}
}

// AddRpath appends an LC_RPATH entry.
func (l *Linker) AddRpath(path string) { l.rpaths = append(l.rpaths, path) }

// AddLibraryPath adds a directory to search when resolving transitive dylib
// dependencies declared via DT_NEEDED / LC_LOAD_DYLIB.
func (l *Linker) AddLibraryPath(path string) { l.libPaths = append(l.libPaths, path) }

// AddObject adds a parsed MH_OBJECT file to the link.
func (l *Linker) AddObject(o *ObjectFile) { l.objects = append(l.objects, o) }

// AddArchive adds a static archive to the link.
func (l *Linker) AddArchive(a *Archive) { l.archives = append(l.archives, a) }

// AddDylib adds a dynamic library to the link.  The order of AddDylib calls
// determines the 1-based dylib ordinals written into the output.
func (l *Linker) AddDylib(d *DylibFile) { l.dylibs = append(l.dylibs, d) }

// AddDylibByInstallName adds a dynamic library dependency by its canonical
// install name only, without a parsed DylibFile. Used when cross-linking
// without SDK stubs available — the LC_LOAD_DYLIB is emitted and dyld
// resolves it at runtime from the shared cache.
func (l *Linker) AddDylibByInstallName(name string) {
	l.dylibs = append(l.dylibs, &DylibFile{
		Path:    name,
		Soname:  name,
		symbols: make(map[string]*DylibSymbol),
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Link — runs all phases
// ──────────────────────────────────────────────────────────────────────────────

// Link runs all seven pipeline phases and returns a LinkResult on success.
func (l *Linker) Link() (*LinkResult, error) {
	// ── Phase 0: resolve transitive dylib dependencies ──────────────────────
	if err := l.resolveTransitiveDylibs(); err != nil {
		return nil, err
	}

	// Build dylib ordinal map (1-based).
	dylibOrdinals := make(map[*DylibFile]int, len(l.dylibs))
	for i, d := range l.dylibs {
		dylibOrdinals[d] = i + 1
	}

	// ── Phase 1: symbol resolution ──────────────────────────────────────────
	symtab := NewSymbolTable()
	activeObjects, err := symtab.Ingest(l.objects, l.archives, l.dylibs, dylibOrdinals)
	if err != nil {
		return nil, err
	}

	// ── Phase 2: merge sections ─────────────────────────────────────────────
	layout, err := MergeSections(activeObjects)
	if err != nil {
		return nil, err
	}

	// ── Phase 3: assign layout ──────────────────────────────────────────────
	if err := AssignLayout(l.outputType, layout); err != nil {
		return nil, err
	}

	// ── Phase 4: resolve symbol addresses ───────────────────────────────────
	if err := ResolveSymbolAddresses(symtab, layout); err != nil {
		return nil, err
	}

	// ── Phase 5: build stubs and GOT ────────────────────────────────────────
	stubs, err := BuildStubs(l.arch, activeObjects, symtab, layout)
	if err != nil {
		return nil, err
	}

	// Re-assign layout to account for synthesised sections.
	if err := AssignLayout(l.outputType, layout); err != nil {
		return nil, err
	}

	// Finalise stub byte sequences now that addresses are known.
	FinalizeStubs(stubs, layout)

	// Update dylib symbol VAddrs (point to stub for function symbols).
	for name, e := range stubs.byName {
		rs := symtab.Lookup(name)
		if rs != nil && rs.Kind == kindDylib {
			rs.VAddr = e.StubVAddr
		}
	}

	// ── Phase 6: patch relocations ──────────────────────────────────────────
	if err := PatchRelocations(l.arch, layout, symtab, stubs, activeObjects); err != nil {
		return nil, err
	}

	// ── Phase 7: validate entry point ───────────────────────────────────────
	if l.outputType == OutputExec && l.entry != "" {
		if symtab.Lookup(l.entry) == nil {
			return nil, fmt.Errorf("entry point %q not found", l.entry)
		}
	}

	return &LinkResult{
		Arch:        l.arch,
		OutputType:  l.outputType,
		Entry:       l.entry,
		InstallName: l.installName,
		Platform:    l.platform,
		Layout:      layout,
		Symtab:      symtab,
		Stubs:       stubs,
		Dylibs:      l.dylibs,
		Rpaths:      l.rpaths,
		DyldMode:    l.dyldMode,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Transitive dylib resolution
// ──────────────────────────────────────────────────────────────────────────────

var systemLibPaths = []string{
	"/usr/lib",
	"/usr/lib/swift",
	"/usr/local/lib",
	"/System/Library/Frameworks",
}

func (l *Linker) resolveTransitiveDylibs() error {
	seen := make(map[string]bool)
	for _, d := range l.dylibs {
		seen[d.Soname] = true
		seen[d.Path] = true
	}

	queue := make([]*DylibFile, len(l.dylibs))
	copy(queue, l.dylibs)

	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]

		for _, needed := range d.Needed {
			if seen[needed] {
				continue
			}
			seen[needed] = true

			dep, err := l.findDylib(needed)
			if err != nil {
				// Non-fatal: some dependencies may be unavailable on the build host.
				// The output binary will still work if the dylib is present at runtime.
				continue
			}
			l.dylibs = append(l.dylibs, dep)
			queue = append(queue, dep)
		}
	}
	return nil
}

func (l *Linker) findDylib(name string) (*DylibFile, error) {
	// Absolute path.
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err == nil {
			return OpenDylib(name)
		}
	}

	// Search -L paths, then system paths.
	searchPaths := append(append([]string{}, l.libPaths...), systemLibPaths...)
	base := filepath.Base(name)
	for _, dir := range searchPaths {
		candidate := filepath.Join(dir, base)
		if _, err := os.Stat(candidate); err == nil {
			return OpenDylib(candidate)
		}
	}

	return nil, fmt.Errorf("dylib %q not found", name)
}

// ──────────────────────────────────────────────────────────────────────────────
// readFile helper
// ──────────────────────────────────────────────────────────────────────────────

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}