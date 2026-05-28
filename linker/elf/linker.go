// linker.go
package elf

import (
	"encoding/binary"
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

	extraNeeded []string
	eflags      uint32
}

// NewLinker returns a Linker targeting the given ELF machine value.
func NewLinker(machine uint16) *Linker {
	return &Linker{arch: machine, outputType: OutputExec}
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

// AddSONeeded records a DT_NEEDED entry by soname without a parsed SharedLib.
func (l *Linker) AddSONeeded(soname string) {
	l.extraNeeded = append(l.extraNeeded, soname)
}

// Link runs all phases and returns a LinkResult.
func (l *Linker) Link() (*LinkResult, error) {
	// Phase 1: transitive shared library dependency walk
	if err := l.walkSharedDeps(); err != nil {
		return nil, fmt.Errorf("link: dependency walk: %w", err)
	}

	// Phase 2: symbol resolution
	symtab := newSymbolTable()
	if err := symtab.Ingest(l.objects, l.archives, l.shared); err != nil {
		return nil, fmt.Errorf("link: symbol resolution: %w", err)
	}
	allObjects := l.collectExtractedObjects()

	// Phase 3: section merging
	layout, err := MergeSections(allObjects)
	if err != nil {
		return nil, fmt.Errorf("link: section merge: %w", err)
	}

	// Phase 3b: PLT synthesis.
	// .plt, .got.plt, and .rela.plt are injected into the layout so they
	// receive virtual addresses from AssignLayout. Stub bytes and GOT initial
	// values are zeroed here and written in Phase 5b once addresses are final.
	pltSyms := collectPLTSymbols(symtab, allObjects)
	if len(pltSyms) > 0 {
		injectPLTSections(layout, pltSyms)
	}

	// Phase 4: virtual address layout
	if err := AssignLayout(l.outputType, layout); err != nil {
		return nil, fmt.Errorf("link: layout: %w", err)
	}

	// Phase 5: resolve symbol addresses
	if err := ResolveSymbolAddresses(symtab, layout); err != nil {
		return nil, fmt.Errorf("link: symbol address resolution: %w", err)
	}

	// Phase 5b: write PLT stubs and assign shared-symbol VAddrs.
	// Must run after AssignLayout (so PLT/GOT addresses are known) and before
	// PatchRelocations (which reads sym.VAddr for R_X86_64_PLT32 etc.).
	if len(pltSyms) > 0 {
		if err := patchPLT(l.arch, layout, pltSyms); err != nil {
			return nil, fmt.Errorf("link: PLT patch: %w", err)
		}
	}

	// Phase 6: relocation patching.
	// Shared symbols now have VAddr = their PLT stub, so R_X86_64_PLT32
	// patches land at the correct stub address instead of 0x0.
	if err := PatchRelocations(l.arch, layout, symtab, allObjects); err != nil {
		return nil, fmt.Errorf("link: relocation patching: %w", err)
	}

	// Phase 7: build result
	needed := collectNeeded(l.shared)
	seen := make(map[string]bool, len(needed))
	for _, n := range needed {
		seen[n] = true
	}
	for _, n := range l.extraNeeded {
		if !seen[n] {
			seen[n] = true
			needed = append(needed, n)
		}
	}

	return &LinkResult{
		Arch:       binelf.Arch(l.arch),
		OutputType: l.outputType,
		Entry:      l.entry,
		Interp:     l.interp,
		Soname:     l.soname,
		Rpath:      l.rpath,
		Needed:     needed,
		Layout:     layout,
		Symtab:     symtab,
		Machine:    l.arch,
		EFlags:     l.eflags,
		PLTSyms:    pltSymNames(pltSyms),
	}, nil
}

// ── PLT synthesis ─────────────────────────────────────────────────────────────

// pltEntry pairs a shared symbol with its 0-based stub index (PLT0 not counted).
type pltEntry struct {
	name string
	sym  *Symbol
	idx  int
}

// collectPLTSymbols returns every kindShared symbol that is actually referenced
// by an object-file relocation, in stable first-seen order.
func collectPLTSymbols(symtab *SymbolTable, objects []*ObjectFile) []pltEntry {
	referenced := make(map[string]bool)
	for _, obj := range objects {
		for _, rel := range obj.Relocs {
			if int(rel.SymIdx) < len(obj.Symbols) {
				if name := obj.Symbols[rel.SymIdx].Name; name != "" {
					referenced[name] = true
				}
			}
		}
	}

	var out []pltEntry
	seen := make(map[string]bool)
	for _, obj := range objects {
		for _, raw := range obj.Symbols {
			if raw.Name == "" || seen[raw.Name] || !referenced[raw.Name] {
				continue
			}
			sym := symtab.Lookup(raw.Name)
			if sym == nil || !sym.IsShared() {
				continue
			}
			seen[raw.Name] = true
			out = append(out, pltEntry{name: raw.Name, sym: sym, idx: len(out)})
		}
	}
	return out
}

// injectPLTSections appends placeholder .plt, .got.plt, and .rela.plt sections
// to the layout so AssignLayout gives them virtual addresses.
func injectPLTSections(layout *Layout, syms []pltEntry) {
	n := len(syms)

	// .plt — exec segment: PLT0 (16 bytes) + one 16-byte stub per symbol
	plt := &MergedSection{
		Name:  ".plt",
		Type:  shtProgbits,
		Flags: 0x2 | 0x4, // SHF_ALLOC | SHF_EXECINSTR
		Data:  make([]byte, 16+n*16),
		Size:  uint64(16 + n*16),
		Align: 16,
	}
	// .got.plt — RW segment: 3 reserved slots + one 8-byte entry per symbol
	gotPLT := &MergedSection{
		Name:  ".got.plt",
		Type:  shtProgbits,
		Flags: 0x2 | 0x1, // SHF_ALLOC | SHF_WRITE
		Data:  make([]byte, (3+n)*8),
		Size:  uint64((3 + n) * 8),
		Align: 8,
	}
	// .rela.plt — RO segment: one 24-byte JUMP_SLOT entry per symbol
	relaPLT := &MergedSection{
		Name:  ".rela.plt",
		Type:  shtRela,
		Flags: 0x2 | 0x40, // SHF_ALLOC | SHF_INFO_LINK
		Data:  make([]byte, n*relaEntSize),
		Size:  uint64(n * relaEntSize),
		Align: 8,
	}

	layout.Sections = append(layout.Sections, plt, gotPLT, relaPLT)
	layout.secByName[".plt"] = plt
	layout.secByName[".got.plt"] = gotPLT
	layout.secByName[".rela.plt"] = relaPLT
}

// patchPLT dispatches to the architecture-specific PLT patcher.
func patchPLT(arch uint16, layout *Layout, syms []pltEntry) error {
	pltSec, ok1 := layout.SectionByName(".plt")
	gotSec, ok2 := layout.SectionByName(".got.plt")
	relaSec, ok3 := layout.SectionByName(".rela.plt")
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("patchPLT: PLT sections missing from layout after AssignLayout")
	}
	switch arch {
	case 0x3E: // EM_X86_64
		patchPLTAMD64(pltSec, gotSec, relaSec, syms)
		return nil
	default:
		return fmt.Errorf("patchPLT: arch 0x%x not yet supported", arch)
	}
}

// patchPLTAMD64 fills AMD64 PLT stubs, sets GOT.PLT lazy-binding initial values,
// writes R_X86_64_JUMP_SLOT entries into .rela.plt, and assigns each shared
// symbol's VAddr to its stub address so PatchRelocations patches call sites
// to the correct PLT stub rather than 0x0.
//
// PLT0 (16 bytes):
//
//	0:  ff 35 rel32   pushq  *(.got.plt+8)(%rip)    RIP_after = pltBase+6
//	6:  ff 25 rel32   jmpq   *(.got.plt+16)(%rip)   RIP_after = pltBase+12
//	12: 0f 1f 40 00   nopl   0(%rax)
//
// Stub i (16 bytes at pltBase+16+i*16):
//
//	0:  ff 25 rel32   jmpq   *(.got.plt[3+i])(%rip) RIP_after = stubBase+6
//	6:  68 idx32      pushq  $i
//	11: e9 rel32      jmpq   plt0                   RIP_after = stubBase+16
func patchPLTAMD64(pltSec, gotSec, relaSec *MergedSection, syms []pltEntry) {
	plt  := pltSec.Data
	got  := gotSec.Data
	rela := relaSec.Data

	pltBase := pltSec.VAddr
	gotBase := gotSec.VAddr

	// PLT0
	plt[0], plt[1] = 0xff, 0x35
	putI32LE(plt[2:], ripRel(gotBase+8, pltBase+6))
	plt[6], plt[7] = 0xff, 0x25
	putI32LE(plt[8:], ripRel(gotBase+16, pltBase+12))
	plt[12], plt[13], plt[14], plt[15] = 0x0f, 0x1f, 0x40, 0x00

	for _, e := range syms {
		i := e.idx
		stubBase    := pltBase + 16 + uint64(i)*16
		stubOff     := 16 + i*16
		gotSlotAddr := gotBase + uint64(3+i)*8
		gotSlotOff  := (3 + i) * 8

		// Stub: jmpq, pushq, jmpq-plt0
		plt[stubOff+0], plt[stubOff+1] = 0xff, 0x25
		putI32LE(plt[stubOff+2:], ripRel(gotSlotAddr, stubBase+6))
		plt[stubOff+6] = 0x68
		putI32LE(plt[stubOff+7:], int32(i))
		plt[stubOff+11] = 0xe9
		putI32LE(plt[stubOff+12:], ripRel(pltBase, stubBase+16))

		// GOT.PLT initial value: points to the pushq (lazy binding trampoline)
		binary.LittleEndian.PutUint64(got[gotSlotOff:], stubBase+6)

		// .rela.plt: R_X86_64_JUMP_SLOT (type 7) at the GOT slot, symIdx=i+1
		ro := i * relaEntSize
		binary.LittleEndian.PutUint64(rela[ro:], gotSlotAddr)
		binary.LittleEndian.PutUint64(rela[ro+8:], (uint64(i+1)<<32)|7)
		// r_addend = 0 (already zero from make)

		// Assign the stub address so PatchRelocations emits a correct PC-rel offset.
		e.sym.VAddr = stubBase
	}
}

// ripRel returns the signed 32-bit RIP-relative offset from ripAfter to target.
// ripAfter is the address of the first byte after the rel32 field
// (the RIP value when the CPU evaluates the displacement).
func ripRel(target, ripAfter uint64) int32 {
	return int32(int64(target) - int64(ripAfter))
}

// putI32LE writes v as a little-endian int32 into b.
func putI32LE(b []byte, v int32) {
	binary.LittleEndian.PutUint32(b, uint32(v))
}

// pltSymNames extracts symbol names from a pltEntry slice.
func pltSymNames(syms []pltEntry) []string {
	if len(syms) == 0 {
		return nil
	}
	out := make([]string, len(syms))
	for i, s := range syms {
		out[i] = s.name
	}
	return out
}

// ── shared library dependency walk ────────────────────────────────────────────

func (l *Linker) walkSharedDeps() error {
	seen := make(map[string]bool)
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
				return fmt.Errorf("loading %s (needed by %s): %w", soname, cur.Soname(), err)
			}
			l.shared = append(l.shared, dep)
			queue = append(queue, dep)
		}
	}
	return nil
}

func (l *Linker) findShared(soname string, rpaths []string) (*SharedLib, error) {
	for _, rp := range rpaths {
		candidate := filepath.Join(rp, soname)
		if _, err := os.Stat(candidate); err == nil {
			return OpenShared(candidate)
		}
	}
	for _, lp := range l.libPaths {
		candidate := filepath.Join(lp, soname)
		if _, err := os.Stat(candidate); err == nil {
			return OpenShared(candidate)
		}
	}
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