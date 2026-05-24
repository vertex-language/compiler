package elf

import "bytes"

// DynBuilder constructs the PLT, GOT, and RELA machinery needed for dynamic
// linking. The emitter calls buildDynamicSections internally; callers that
// need finer control over PLT/GOT layout can use DynBuilder directly and
// inject the resulting sections via Builder.AddSection.
//
// Architecture of a dynamically linked ELF binary:
//
//   .interp          — path to the runtime linker (PT_INTERP)
//   .dynsym          — dynamic symbol table (SHT_DYNSYM)
//   .dynstr          — string table for .dynsym
//   .gnu.hash        — GNU-style symbol hash (optional, faster lookup)
//   .rela.dyn        — RELA relocations for data symbols (GOT entries, R_*_GLOB_DAT)
//   .rela.plt        — RELA relocations for PLT stubs (R_*_JUMP_SLOT)
//   .plt             — procedure linkage table (lazy binding stubs)
//   .got             — global offset table (data symbols)
//   .got.plt         — GOT entries for PLT stubs
//   .dynamic         — array of Elf64_Dyn entries consumed by the runtime linker

// DynSym is a symbol that requires a PLT stub and/or a GOT entry.
type DynSym struct {
	// Name is the symbol's external name (e.g. "printf", "malloc").
	Name string

	// NeedsPLT requests a PLT stub and a .got.plt slot with an
	// R_*_JUMP_SLOT relocation. Set for imported functions.
	NeedsPLT bool

	// NeedsGOT requests a .got entry with an R_*_GLOB_DAT relocation.
	// Set for imported data objects.
	NeedsGOT bool
}

// DynBuilder accumulates dynamic symbol requests and emits the .plt, .got,
// .got.plt, .rela.plt, and .rela.dyn sections as a coherent unit.
type DynBuilder struct {
	arch    Arch
	syms    []DynSym
	gotBase uint64 // virtual address of .got.plt, set during layout
}

// NewDynBuilder returns a DynBuilder for arch.
func NewDynBuilder(arch Arch) *DynBuilder {
	return &DynBuilder{arch: arch}
}

// Add registers sym for PLT/GOT allocation.
func (db *DynBuilder) Add(sym DynSym) { db.syms = append(db.syms, sym) }

// PLTEntrySize returns the PLT stub byte size for db.arch.
// AMD64: 16 bytes per entry (including the PLT0 trampoline).
// ARM64: 16 bytes per entry.
func (db *DynBuilder) PLTEntrySize() int {
	switch db.arch {
	case ArchARM64:
		return 16
	default: // AMD64, RISCV64
		return 16
	}
}

// Sections returns the set of ELF sections produced by this DynBuilder.
// The caller must add these to the Builder in the order returned:
//
//	.got.plt → .plt → .got → .rela.plt → .rela.dyn
//
// Addresses within the sections are expressed as offsets from zero; the
// emitter resolves them to virtual addresses during layout.
func (db *DynBuilder) Sections() []Section {
	var gotPLT, plt, got, relaPLT, relaDyn []byte

	// .got.plt slot layout:
	//   [0]  reserved — &.dynamic
	//   [1]  reserved — link_map*
	//   [2]  reserved — _dl_runtime_resolve*
	//   [3…] one 8-byte slot per PLT symbol
	gotPLT = make([]byte, 3*8) // three reserved slots, filled at runtime

	// .plt: PLT0 trampoline (arch-specific), then one stub per PLT symbol.
	plt = db.buildPLT0()

	for i, sym := range db.syms {
		if !sym.NeedsPLT {
			continue
		}
		gotSlotIdx := 3 + i // slot index in .got.plt
		gotSlotOff := uint64(gotSlotIdx * 8)

		// PLT stub for this symbol.
		stub := db.buildPLTStub(gotSlotOff, len(plt))
		plt = append(plt, stub...)

		// .got.plt slot — pre-filled with the address of the push instruction
		// in the PLT stub so the lazy resolver can determine which slot fired.
		slot := make([]byte, 8)
		// Actual value patched at load time; we emit 0 as a placeholder.
		gotPLT = append(gotPLT, slot...)

		// R_*_JUMP_SLOT relocation for this GOT slot.
		symIdx := uint32(1 + i) // 1-based index into .dynsym (0 = null entry)
		relaPLT = appendRela(relaPLT, gotSlotOff, symIdx, db.jumpSlotType(), 0)
	}

	for i, sym := range db.syms {
		if !sym.NeedsGOT {
			continue
		}
		gotSlotOff := uint64(len(got))
		got = append(got, make([]byte, 8)...)

		symIdx := uint32(1 + i)
		relaDyn = appendRela(relaDyn, gotSlotOff, symIdx, db.globDatType(), 0)
	}

	var secs []Section
	if len(gotPLT) > 0 {
		secs = append(secs, Section{
			Name:  ".got.plt",
			Type:  SHT_PROGBITS,
			Flags: SHF_ALLOC | SHF_WRITE,
			Data:  gotPLT,
			Align: 8,
		})
	}
	if len(plt) > 0 {
		secs = append(secs, Section{
			Name:  ".plt",
			Type:  SHT_PROGBITS,
			Flags: SHF_ALLOC | SHF_EXECINSTR,
			Data:  plt,
			Align: 16,
		})
	}
	if len(got) > 0 {
		secs = append(secs, Section{
			Name:  ".got",
			Type:  SHT_PROGBITS,
			Flags: SHF_ALLOC | SHF_WRITE,
			Data:  got,
			Align: 8,
		})
	}
	if len(relaPLT) > 0 {
		secs = append(secs, Section{
			Name:    ".rela.plt",
			Type:    SHT_RELA,
			Flags:   SHF_ALLOC | SHF_INFO_LINK,
			Data:    relaPLT,
			Align:   8,
			EntSize: relaEntrySize,
		})
	}
	if len(relaDyn) > 0 {
		secs = append(secs, Section{
			Name:    ".rela.dyn",
			Type:    SHT_RELA,
			Flags:   SHF_ALLOC,
			Data:    relaDyn,
			Align:   8,
			EntSize: relaEntrySize,
		})
	}
	return secs
}

// DynSyms returns the ordered dynamic symbol list for populating .dynsym.
func (db *DynBuilder) DynSyms() []Symbol {
	syms := make([]Symbol, len(db.syms))
	for i, ds := range db.syms {
		syms[i] = Symbol{
			Name:   ds.Name,
			Global: true,
			Type:   STT_FUNC,
		}
	}
	return syms
}

// ── architecture-specific PLT emission ───────────────────────────────────────

// buildPLT0 returns the PLT0 trampoline stub for the target architecture.
// The trampoline is responsible for transferring control to the dynamic linker
// on the first call through any PLT entry.
func (db *DynBuilder) buildPLT0() []byte {
	switch db.arch {
	case ArchARM64:
		// AArch64 PLT0: 32 bytes, loads GOT[1] (link_map) and GOT[2]
		// (_dl_runtime_resolve) via IP0/IP1 and branches.
		return []byte{
			// stp x16, x30, [sp, #-16]!
			0xf0, 0x7b, 0xbf, 0xa9,
			// adrp x16, .got.plt@PAGE
			0x10, 0x00, 0x00, 0x90,
			// ldr x17, [x16, #:lo12:.got.plt+8]
			0x11, 0x02, 0x40, 0xf9,
			// add x16, x16, #:lo12:.got.plt
			0x10, 0x00, 0x00, 0x91,
			// ldr x17, [x16, #16]
			0x11, 0x06, 0x40, 0xf9,
			// br x17
			0x20, 0x02, 0x1f, 0xd6,
			// nop × 2 (padding to 32 bytes)
			0x1f, 0x20, 0x03, 0xd5,
			0x1f, 0x20, 0x03, 0xd5,
		}
	default: // AMD64
		// 16-byte PLT0: pushq GOT[1]; jmpq *GOT[2]; nop padding.
		return []byte{
			0xFF, 0x35, 0x00, 0x00, 0x00, 0x00, // pushq  *GOT+8(%rip)
			0xFF, 0x25, 0x00, 0x00, 0x00, 0x00, // jmpq   *GOT+16(%rip)
			0x0F, 0x1F, 0x40, 0x00,             // nopl   0(%rax)  [padding]
		}
	}
}

// buildPLTStub returns the PLT stub for a single symbol. gotSlotOff is the
// byte offset of the symbol's .got.plt slot; pltOff is the current write
// offset within .plt (used for PC-relative calculations).
func (db *DynBuilder) buildPLTStub(gotSlotOff uint64, pltOff int) []byte {
	switch db.arch {
	case ArchARM64:
		return []byte{
			// adrp x16, gotSlotOff@PAGE
			0x10, 0x00, 0x00, 0x90,
			// ldr x17, [x16, gotSlotOff@PAGEOFF]
			0x11, 0x02, 0x40, 0xf9,
			// add x16, x16, gotSlotOff@PAGEOFF
			0x10, 0x00, 0x00, 0x91,
			// br x17
			0x20, 0x02, 0x1f, 0xd6,
		}
	default: // AMD64
		// jmpq *gotSlotOff(%rip); pushq $symIdx; jmpq plt0
		return []byte{
			0xFF, 0x25, 0x00, 0x00, 0x00, 0x00, // jmpq *GOT[n](%rip)
			0x68, 0x00, 0x00, 0x00, 0x00,       // pushq $symIdx
			0xE9, 0x00, 0x00, 0x00, 0x00,       // jmpq  plt0
		}
	}
}

func (db *DynBuilder) jumpSlotType() uint32 {
	if db.arch == ArchARM64 {
		return R_AARCH64_JUMP_SLOT
	}
	return R_X86_64_JUMP_SLOT
}

func (db *DynBuilder) globDatType() uint32 {
	if db.arch == ArchARM64 {
		return R_AARCH64_GLOB_DAT
	}
	return R_X86_64_GLOB_DAT
}

// ── buildDynamicSections is called by the emitter ────────────────────────────

// buildDynamicSections constructs .dynstr and .dynamic section data. It is
// called after layout so that section virtual addresses are available.
func (e *emitter) buildDynamicSections(dynSec *builtSection) {
	var dynstr strTab
	dynstr.add("") // index 0 = empty

	type dynEntry struct{ tag, val uint64 }
	var entries []dynEntry

	if e.b.soname != "" {
		idx := dynstr.add(e.b.soname)
		entries = append(entries, dynEntry{DT_SONAME, uint64(idx)})
	}
	for _, lib := range e.b.needed {
		idx := dynstr.add(lib)
		entries = append(entries, dynEntry{DT_NEEDED, uint64(idx)})
	}
	if e.b.rpath != "" {
		idx := dynstr.add(e.b.rpath)
		entries = append(entries, dynEntry{DT_RUNPATH, uint64(idx)})
	}

	// Wire up .dynstr.
	if sec := e.secByName[".dynstr"]; sec != nil {
		entries = append(entries, dynEntry{DT_STRTAB, sec.addr})
		// Size will be known after dynstr is finalized; patch below.
	}
	// Wire up .dynsym.
	if sec := e.secByName[".dynsym"]; sec != nil {
		entries = append(entries, dynEntry{DT_SYMTAB, sec.addr})
		entries = append(entries, dynEntry{DT_SYMENT, symEntrySize})
	}
	// Wire up .rela.dyn.
	if sec := e.secByName[".rela.dyn"]; sec != nil {
		entries = append(entries, dynEntry{DT_RELA, sec.addr})
		entries = append(entries, dynEntry{DT_RELASZ, uint64(len(sec.data))})
		entries = append(entries, dynEntry{DT_RELAENT, relaEntrySize})
	}
	// Wire up .rela.plt.
	if sec := e.secByName[".rela.plt"]; sec != nil {
		entries = append(entries, dynEntry{DT_JMPREL, sec.addr})
		entries = append(entries, dynEntry{DT_PLTRELSZ, uint64(len(sec.data))})
		entries = append(entries, dynEntry{DT_PLTREL, DT_RELA})
	}
	// Wire up .got.plt.
	if sec := e.secByName[".got.plt"]; sec != nil {
		entries = append(entries, dynEntry{DT_PLTGOT, sec.addr})
	}

	// Finalize dynstr size.
	dynstrData := dynstr.bytes()
	for i, en := range entries {
		if en.tag == DT_STRTAB {
			entries = append(entries[:i+1],
				append([]dynEntry{{DT_STRSZ, uint64(len(dynstrData))}},
					entries[i+1:]...)...)
			break
		}
	}

	// DT_NULL terminator.
	entries = append(entries, dynEntry{DT_NULL, 0})

	// Serialize .dynamic.
	var buf bytes.Buffer
	for _, en := range entries {
		var b [dynEntrySize]byte
		putU64le(b[0:], en.tag)
		putU64le(b[8:], en.val)
		buf.Write(b[:])
	}

	// Patch .dynstr and .dynamic section data.
	if sec := e.secByName[".dynstr"]; sec != nil {
		sec.data = dynstrData
		sec.memSize = uint64(len(dynstrData))
		// Wire .dynsym.link → .dynstr shIdx (known after addSec calls).
		if dynsym := e.secByName[".dynsym"]; dynsym != nil {
			dynsym.link = uint32(sec.shIdx)
			// Emit a null .dynsym entry.
			dynsym.data = make([]byte, symEntrySize)
			dynsym.memSize = symEntrySize
		}
	}
	if dynSec != nil {
		dynSec.data = buf.Bytes()
		dynSec.memSize = uint64(len(dynSec.data))
	}
}

// ── RELA helpers ──────────────────────────────────────────────────────────────

// appendRela serializes a single Elf64_Rela entry and appends it to dst.
func appendRela(dst []byte, offset uint64, symIdx uint32, rType uint32, addend int64) []byte {
	info := (uint64(symIdx) << 32) | uint64(rType)
	var b [relaEntrySize]byte
	putU64le(b[0:], offset)
	putU64le(b[8:], info)
	putI64le(b[16:], addend)
	return append(dst, b[:]...)
}