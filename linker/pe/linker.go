// Package pe implements a static linker for PE32+ (Windows) executables and DLLs.
// It reads COFF relocatable objects (.obj) and import libraries (.lib),
// resolves symbols, merges sections, patches COFF relocations, and emits
// the final image via github.com/vertex-language/compiler/bin/pe.
package pe

import (
	"encoding/binary"
	"fmt"
	"strings"

	binpe "github.com/vertex-language/compiler/bin/pe"
)

// Re-export the machine type constants from bin/pe for convenience.
const (
	MachineAMD64 = binpe.MachineAMD64
	MachineARM64 = binpe.MachineARM64
)

// Linker links COFF object files and import libraries into a PE32+ image.
//
// Usage:
//
//	lnk := pe.NewLinker(pe.MachineAMD64)
//	lnk.SetEntry("main")
//	lnk.AddObject(pe.MustOpenObject("main.obj"))
//	lnk.AddArchive(pe.MustOpenArchive("kernel32.lib"))
//
//	result, err := lnk.Link()
//	out, err := result.Emit()
//	os.WriteFile("program.exe", out, 0o755)
type Linker struct {
	machine   binpe.MachineType
	subsystem binpe.Subsystem
	imageBase uint64
	entry     string
	dllMode   bool
	dllName   string
	dynamicBase bool

	objects  []*ObjectFile
	archives []*Archive

	// Additional explicit exports (beyond those found in .drectve).
	extraExports []ExportRecord

	loadConfig   *binpe.LoadConfig
	debugEntries []binpe.DebugEntry

	dllCharacteristics uint16
	stackReserve       uint64
	stackCommit        uint64
	heapReserve        uint64
	heapCommit         uint64

	majorOSVersion, minorOSVersion       uint16
	majorSubsystemVersion, minorSubsystemVersion uint16
}

// NewLinker returns a Linker configured for machine. Default subsystem is
// SubsystemWindowsCUI. ASLR (DYNAMIC_BASE, HIGH_ENTROPY_VA), NX (NX_COMPAT),
// and CFG (GUARD_CF) are enabled by default.
func NewLinker(machine binpe.MachineType) *Linker {
	return &Linker{
		machine:     machine,
		subsystem:   binpe.SubsystemWindowsCUI,
		dynamicBase: true,
		dllCharacteristics: binpe.IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA |
			binpe.IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE |
			binpe.IMAGE_DLLCHARACTERISTICS_NX_COMPAT |
			binpe.IMAGE_DLLCHARACTERISTICS_GUARD_CF,
		majorOSVersion:        6,
		minorOSVersion:        0,
		majorSubsystemVersion: 6,
		minorSubsystemVersion: 0,
	}
}

// ── Configuration ─────────────────────────────────────────────────────────────

func (l *Linker) SetEntry(name string)              { l.entry = name }
func (l *Linker) SetSubsystem(ss binpe.Subsystem)   { l.subsystem = ss }
func (l *Linker) SetImageBase(base uint64)           { l.imageBase = base }
func (l *Linker) SetDLL(name string)                 { l.dllMode = true; l.dllName = name }
func (l *Linker) SetDynamicBase(v bool)              { l.dynamicBase = v }
func (l *Linker) SetDllCharacteristics(f uint16)     { l.dllCharacteristics = f }
func (l *Linker) AddDllCharacteristics(f uint16)     { l.dllCharacteristics |= f }
func (l *Linker) SetLoadConfig(lc binpe.LoadConfig)  { l.loadConfig = &lc }
func (l *Linker) AddDebugEntry(d binpe.DebugEntry)   { l.debugEntries = append(l.debugEntries, d) }
func (l *Linker) SetStackSize(reserve, commit uint64) {
	l.stackReserve, l.stackCommit = reserve, commit
}
func (l *Linker) SetHeapSize(reserve, commit uint64) {
	l.heapReserve, l.heapCommit = reserve, commit
}
func (l *Linker) SetOSVersion(major, minor uint16) {
	l.majorOSVersion, l.minorOSVersion = major, minor
}
func (l *Linker) SetSubsystemVersion(major, minor uint16) {
	l.majorSubsystemVersion, l.minorSubsystemVersion = major, minor
}

// AddExport registers an explicit export (for DLL mode).
// Ordinal=0 assigns ordinals sequentially starting from 1.
func (l *Linker) AddExport(er ExportRecord) {
	l.extraExports = append(l.extraExports, er)
}

// ── Input ─────────────────────────────────────────────────────────────────────

// AddObject adds a COFF object file to be linked.
func (l *Linker) AddObject(o *ObjectFile) { l.objects = append(l.objects, o) }

// AddArchive adds a COFF archive (static library or import library).
func (l *Linker) AddArchive(a *Archive) { l.archives = append(l.archives, a) }

// ── Link ──────────────────────────────────────────────────────────────────────

// Link runs all linker phases and returns a LinkResult on success.
func (l *Linker) Link() (*LinkResult, error) {
	// ── Phase 1: Symbol resolution ────────────────────────────────────────────
	symtab := newSymbolTable()

	// Ingest all explicitly added object files first.
	if err := symtab.ingestObjects(l.objects); err != nil {
		return nil, err
	}
	// Honour entry-hint directives from .drectve.
	if l.entry == "" {
		for _, obj := range l.objects {
			if obj.EntryHint != "" {
				l.entry = obj.EntryHint
				break
			}
		}
	}

	// Archive extraction: LLD-style fixed-point loop.
	// We also pre-scan import stubs in every archive eagerly so that
	// __imp_* symbols are always available for undefined-symbol checks.
	for _, ar := range l.archives {
		for _, m := range ar.Members {
			if m.imp != nil {
				symtab.ingestImportStub(m.imp)
			}
		}
	}
	for {
		prev := symtab.undefinedCount()
		for _, ar := range l.archives {
			if err := symtab.ingestArchive(ar); err != nil {
				return nil, err
			}
		}
		if symtab.undefinedCount() >= prev {
			break
		}
	}

	// Collect all imports gathered from import stubs.
	imports := symtab.collectImports()

	// Collect exports (drectve + explicit).
	var exports []ExportRecord
	exports = append(exports, symtab.exports...)
	exports = append(exports, l.extraExports...)
	// Assign ordinals to exports that lack them.
	for i := range exports {
		if exports[i].Ordinal == 0 {
			exports[i].Ordinal = uint16(i + 1)
		}
	}

	// ── Phase 2: Check for unresolved strong symbols ──────────────────────────
	if err := symtab.checkUndefined(); err != nil {
		return nil, err
	}

	// ── Phase 3: Section merging ──────────────────────────────────────────────
	layout, err := mergeSections(symtab.objs)
	if err != nil {
		return nil, err
	}

	// ── Phase 4: Resolve image base ───────────────────────────────────────────
	imageBase := l.imageBase
	if imageBase == 0 {
		if l.dllMode {
			imageBase = 0x0000000180000000
		} else {
			imageBase = 0x0000000140000000
		}
	}

	// ── Phase 5: VA assignment ────────────────────────────────────────────────
	synth := assignLayout(layout, imports, exports, l.dynamicBase,
		l.dllName, l.loadConfig != nil, len(l.debugEntries) > 0)

	// ── Phase 6: Resolve symbol addresses ────────────────────────────────────
	if err := resolveSymbolAddresses(symtab, layout, imageBase); err != nil {
		return nil, err
	}

	// Resolve __imp_xxx symbols from IAT slot RVAs.
	resolveImportSymbols(symtab, imports, synth, imageBase)

	// ── Phase 7: Synthesise import thunks ─────────────────────────────────────
	thunks := synthesizeThunks(uint16(l.machine), symtab, imports, synth, imageBase)

	// Patch thunk symbol VAs into the symbol table.
	if thunks != nil {
		for sym, va := range thunks.SymAddr {
			if gs, ok := symtab.syms[sym]; ok && gs.kind == symImport {
				gs.va = uint64(va)
			}
		}
	}

	// ── Phase 8: Patch relocations ────────────────────────────────────────────
	if err := patchRelocations(uint16(l.machine), layout, symtab, imageBase); err != nil {
		return nil, err
	}

	return &LinkResult{
		Machine:               l.machine,
		Subsystem:             l.subsystem,
		ImageBase:             imageBase,
		Entry:                 l.entry,
		IsDLL:                 l.dllMode,
		DLLName:               l.dllName,
		Layout:                layout,
		Symtab:                symtab,
		Imports:               imports,
		Exports:               exports,
		Thunks:                thunks,
		Synth:                 synth,
		DllCharacteristics:    l.dllCharacteristics,
		StackReserve:          l.stackReserve,
		StackCommit:           l.stackCommit,
		HeapReserve:           l.heapReserve,
		HeapCommit:            l.heapCommit,
		MajorOSVersion:        l.majorOSVersion,
		MinorOSVersion:        l.minorOSVersion,
		MajorSubsystemVersion: l.majorSubsystemVersion,
		MinorSubsystemVersion: l.minorSubsystemVersion,
		LoadConfig:            l.loadConfig,
		DebugEntries:          l.debugEntries,
	}, nil
}

// ─── Import thunk synthesis ───────────────────────────────────────────────────

// synthesizeThunks creates a .text$thk section containing import thunks for
// every CODE import symbol that is directly referenced (undecorated name used).
func synthesizeThunks(machine uint16, symtab *SymbolTable,
	imports []*CollectedImport, synth SyntheticLayout, imageBase uint64) *ThunkSection {

	type thunkEntry struct {
		sym  string
		impSym *CollectedImportSym
		imp    *CollectedImport
	}
	var entries []thunkEntry

	for _, ci := range imports {
		for i := range ci.Symbols {
			sym := &ci.Symbols[i]
			if sym.ImportType != 0 { // not IMPORT_CODE
				continue
			}
			// Only emit thunk if undecorated name is referenced.
			if gs, ok := symtab.syms[sym.Name]; ok && gs.kind == symImport {
				entries = append(entries, thunkEntry{sym.Name, sym, ci})
			}
		}
	}

	if len(entries) == 0 {
		return nil
	}

	thunkSize := thunkCodeSize(machine)
	total := uint32(len(entries)) * thunkSize
	data := make([]byte, total)
	symAddr := make(map[string]uint32)

	// We don't know the thunk section VA yet (it will be assigned by bin/pe after
	// all user sections). Store relative offsets; the actual VA is filled later
	// by the Builder when it adds the section.  For now, generate the thunk
	// code assuming VA=0 and note that relocations within the thunk are handled
	// by bin/pe's own IAT references after import resolution.
	//
	// For AMD64: FF 25 [REL32 → __imp_sym]
	// For ARM64: ADRP X16 / LDR X16,[X16] / BR X16  (with PAGEBASE_REL21 + PAGEOFFSET_12L relocs)
	// Since we've already patched everything, we embed the actual IAT VA directly.

	le := binary.LittleEndian
	for i, e := range entries {
		off := uint32(i) * thunkSize
		iatSlotVA := imageBase + uint64(e.impSym.IATSlotRVA)

		switch machine {
		case 0x8664: // AMD64: jmpq *<iat>(%rip)
			// FF 25 [rel32]
			// We don't know the thunk VA yet; use a placeholder.
			// The rel32 will be: iatSlotVA - (thunkVA + off + 6)
			// Store iatSlotVA in the rel32 field; fixup will happen in result.go
			// when thunkVA is known.
			data[off] = 0xFF
			data[off+1] = 0x25
			// Placeholder: store the IAT VA as a raw 64-bit value in scratch.
			// We'll fix this up in result.go when the thunk section VA is known.
			le.PutUint32(data[off+2:], 0)
			le.PutUint32(data[off+6:], uint32(iatSlotVA>>32))  // high dword as marker
			_ = iatSlotVA
		case 0xAA64, 0xA641: // ARM64
			// adrp x16, #page  (4 bytes)
			// ldr  x16, [x16, #off] (4 bytes)
			// br   x16 (4 bytes)
			le.PutUint32(data[off+0:], 0x90000010) // ADRP x16, 0
			le.PutUint32(data[off+4:], 0xF9400210) // LDR x16, [x16, #0]
			le.PutUint32(data[off+8:], 0xD61F0200) // BR x16
		}
		symAddr[e.sym] = off // section-relative; caller adds thunk.VAddr
	}

	return &ThunkSection{
		Data:    data,
		SymAddr: symAddr,
	}
}

func thunkCodeSize(machine uint16) uint32 {
	switch machine {
	case 0x8664:
		return 8 // FF 25 [rel32] + 2-byte padding (align to 8)
	case 0xAA64, 0xA641:
		return 12 // adrp + ldr + br
	default:
		return 8
	}
}

// Keep imports for use by referenced functions.
var _ = strings.EqualFold
var _ = fmt.Errorf
var _ = binary.LittleEndian