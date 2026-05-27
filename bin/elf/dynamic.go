// dynamic.go
package elf

import (
	"bytes"
	"sort"
)

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
//   .hash            — SysV symbol hash table (optional, legacy)
//   .gnu.hash        — GNU symbol hash table (optional, preferred)
//   .gnu.version     — per-symbol version indices (optional)
//   .gnu.version_r   — versioned library dependencies (optional)
//   .rela.dyn        — RELA relocations for data symbols (R_*_GLOB_DAT)
//   .rela.plt        — RELA relocations for PLT stubs (R_*_JUMP_SLOT)
//   .plt             — procedure linkage table (lazy binding stubs)
//   .got             — global offset table (data symbols)
//   .got.plt         — GOT entries for PLT stubs
//   .dynamic         — Elf64_Dyn array consumed by the runtime linker

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
	arch Arch
	syms []DynSym
}

// NewDynBuilder returns a DynBuilder for arch.
func NewDynBuilder(arch Arch) *DynBuilder {
	return &DynBuilder{arch: arch}
}

// Add registers sym for PLT/GOT allocation.
func (db *DynBuilder) Add(sym DynSym) { db.syms = append(db.syms, sym) }

// PLTEntrySize returns the PLT stub byte size for db.arch.
func (db *DynBuilder) PLTEntrySize() int { return 16 }

// Sections returns the set of ELF sections produced by this DynBuilder
// in the order they should be added to the Builder:
//
//	.got.plt → .plt → .got → .rela.plt → .rela.dyn
func (db *DynBuilder) Sections() []Section {
	var gotPLT, plt, got, relaPLT, relaDyn []byte

	// .got.plt reserved slots: [0] &.dynamic  [1] link_map*  [2] _dl_runtime_resolve*
	gotPLT = make([]byte, 3*8)
	plt = db.buildPLT0()

	for i, sym := range db.syms {
		if !sym.NeedsPLT {
			continue
		}
		gotSlotIdx := 3 + i
		gotSlotOff := uint64(gotSlotIdx * 8)

		plt = append(plt, db.buildPLTStub(gotSlotOff, len(plt))...)
		gotPLT = append(gotPLT, make([]byte, 8)...)

		symIdx := uint32(1 + i)
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

func (db *DynBuilder) buildPLT0() []byte {
	switch db.arch {
	case ArchARM64:
		// AArch64 PLT0 (32 bytes): loads GOT[1]/GOT[2] via IP0/IP1 and branches.
		return []byte{
			0xf0, 0x7b, 0xbf, 0xa9, // stp   x16, x30, [sp, #-16]!
			0x10, 0x00, 0x00, 0x90, // adrp  x16, .got.plt@PAGE
			0x11, 0x02, 0x40, 0xf9, // ldr   x17, [x16, #:lo12:.got.plt+8]
			0x10, 0x00, 0x00, 0x91, // add   x16, x16, #:lo12:.got.plt
			0x11, 0x06, 0x40, 0xf9, // ldr   x17, [x16, #16]
			0x20, 0x02, 0x1f, 0xd6, // br    x17
			0x1f, 0x20, 0x03, 0xd5, // nop
			0x1f, 0x20, 0x03, 0xd5, // nop
		}
	case ArchRISCV64:
		// RISC-V PLT0 (32 bytes per psABI §4.5).
		// Immediates referencing .got.plt are placeholders; patch via
		// R_RISCV_PCREL_HI20 / R_RISCV_PCREL_LO12_I after layout.
		return []byte{
			0x97, 0x03, 0x00, 0x00, // auipc  t2, 0            (.got.plt@pcrel_hi)
			0x33, 0x03, 0xc3, 0x41, // sub    t1, t1, t3
			0x03, 0xbe, 0x03, 0x00, // ld     t3, 0(t2)        (_dl_runtime_resolve)
			0x13, 0x03, 0x03, 0xfe, // addi   t1, t1, -32
			0x93, 0x8e, 0x03, 0x00, // addi   t4, t2, 0        (.got.plt@pcrel_lo)
			0x13, 0x53, 0x13, 0x00, // srli   t1, t1, 1
			0x83, 0x82, 0x83, 0x00, // ld     t0, 8(t2)        (link_map)
			0x67, 0x00, 0x0e, 0x00, // jr     t3
		}
	default: // AMD64
		// 16-byte PLT0: pushq GOT[1]; jmpq *GOT[2]; nop padding.
		return []byte{
			0xff, 0x35, 0x00, 0x00, 0x00, 0x00, // pushq *GOT+8(%rip)
			0xff, 0x25, 0x00, 0x00, 0x00, 0x00, // jmpq  *GOT+16(%rip)
			0x0f, 0x1f, 0x40, 0x00,             // nopl  0(%rax)
		}
	}
}

func (db *DynBuilder) buildPLTStub(gotSlotOff uint64, pltOff int) []byte {
	switch db.arch {
	case ArchARM64:
		return []byte{
			0x10, 0x00, 0x00, 0x90, // adrp  x16, gotSlotOff@PAGE
			0x11, 0x02, 0x40, 0xf9, // ldr   x17, [x16, gotSlotOff@PAGEOFF]
			0x10, 0x00, 0x00, 0x91, // add   x16, x16, gotSlotOff@PAGEOFF
			0x20, 0x02, 0x1f, 0xd6, // br    x17
		}
	case ArchRISCV64:
		// 16-byte stub per psABI. Immediates are zero-placeholders;
		// patch via R_RISCV_PCREL_HI20 / R_RISCV_PCREL_LO12_I.
		return []byte{
			0x17, 0x0e, 0x00, 0x00, // auipc  t3, 0   (sym@.got.plt@pcrel_hi)
			0x03, 0x3e, 0x0e, 0x00, // ld     t3, 0(t3)
			0x67, 0x03, 0x0e, 0x00, // jalr   t1, t3
			0x13, 0x00, 0x00, 0x00, // nop
		}
	default: // AMD64
		// jmpq *GOT[n](%rip); pushq $symIdx; jmpq plt0
		return []byte{
			0xff, 0x25, 0x00, 0x00, 0x00, 0x00, // jmpq *GOT[n](%rip)
			0x68, 0x00, 0x00, 0x00, 0x00,       // pushq $symIdx
			0xe9, 0x00, 0x00, 0x00, 0x00,       // jmpq  plt0
		}
	}
}

func (db *DynBuilder) jumpSlotType() uint32 {
	switch db.arch {
	case ArchARM64:
		return R_AARCH64_JUMP_SLOT
	case ArchRISCV64:
		return R_RISCV_JUMP_SLOT
	default:
		return R_X86_64_JUMP_SLOT
	}
}

func (db *DynBuilder) globDatType() uint32 {
	switch db.arch {
	case ArchARM64:
		return R_AARCH64_GLOB_DAT
	case ArchRISCV64:
		// RISC-V uses R_RISCV_64 for GOT data entries (no separate GLOB_DAT).
		return R_RISCV_64
	default:
		return R_X86_64_GLOB_DAT
	}
}

// ── Hash section builders ─────────────────────────────────────────────────────

// BuildGNUHash builds a .gnu.hash (SHT_GNU_HASH) section body for the given
// dynamic symbol names.
//
// The GNU hash format requires .dynsym to be ordered by (gnuHash(name) %
// nbuckets). Use SortGNUHashSyms to produce that ordering; pass the same
// sorted name list here.
//
// symOffset is the .dynsym index of the first hashed symbol — typically 1,
// since entry 0 is always the null symbol.
func BuildGNUHash(sortedNames []string, symOffset uint32) []byte {
	n := len(sortedNames)

	nbuckets := uint32(n)
	if nbuckets == 0 {
		nbuckets = 1
	}

	// maskwords: Bloom filter word count; must be a non-zero power of two.
	// Heuristic: aim for ~12 bits per symbol.
	maskwords := uint32(1)
	for maskwords < uint32((n*12+63)/64) {
		maskwords <<= 1
	}
	const shift2 = uint32(6) // standard GNU ld value

	// Build Bloom filter (64-bit words for ELF64).
	bloom := make([]uint64, maskwords)
	for _, name := range sortedNames {
		h1 := gnuHash(name)
		h2 := h1 >> shift2
		word := (h1 / 64) % maskwords
		bloom[word] |= 1 << (h1 % 64)
		bloom[word] |= 1 << (h2 % 64)
	}

	// Build buckets: bucket[i] = lowest dynsym index whose hash%nbuckets == i.
	buckets := make([]uint32, nbuckets)
	for i, name := range sortedNames {
		b := gnuHash(name) % nbuckets
		if buckets[b] == 0 {
			buckets[b] = symOffset + uint32(i)
		}
	}

	// Build chains: one entry per hashed symbol.
	// Bit 0: 0 = more symbols in this bucket follow, 1 = last in bucket.
	chains := make([]uint32, n)
	for i, name := range sortedNames {
		h := gnuHash(name) &^ uint32(1) // clear bit 0
		if i+1 < n && gnuHash(sortedNames[i+1])%nbuckets == gnuHash(name)%nbuckets {
			chains[i] = h // more in chain
		} else {
			chains[i] = h | 1 // last in chain
		}
	}

	// Serialize: header(4×u32) + bloom(maskwords×u64) + buckets(×u32) + chains(×u32).
	size := 4*4 + 8*int(maskwords) + 4*int(nbuckets) + 4*n
	buf := make([]byte, size)
	off := 0
	putU32le(buf[off:], nbuckets);  off += 4
	putU32le(buf[off:], symOffset); off += 4
	putU32le(buf[off:], maskwords); off += 4
	putU32le(buf[off:], shift2);    off += 4
	for _, w := range bloom {
		putU64le(buf[off:], w); off += 8
	}
	for _, b := range buckets {
		putU32le(buf[off:], b); off += 4
	}
	for _, c := range chains {
		putU32le(buf[off:], c); off += 4
	}
	return buf
}

// SortGNUHashSyms returns symNames sorted into GNU hash order (by
// gnuHash(name) % nbuckets). The second return value maps each output
// position to its original input index, which callers need to reorder
// their .dynsym entries to match.
func SortGNUHashSyms(symNames []string) (sorted []string, perm []int) {
	type entry struct {
		name    string
		origIdx int
	}
	entries := make([]entry, len(symNames))
	for i, n := range symNames {
		entries[i] = entry{n, i}
	}
	nbuckets := uint32(len(symNames))
	if nbuckets == 0 {
		nbuckets = 1
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return gnuHash(entries[i].name)%nbuckets < gnuHash(entries[j].name)%nbuckets
	})
	sorted = make([]string, len(entries))
	perm = make([]int, len(entries))
	for i, e := range entries {
		sorted[i] = e.name
		perm[i] = e.origIdx
	}
	return
}

// BuildSysVHash builds a .hash (SHT_HASH) section body for the given dynamic
// symbol names. symNames must include the null entry at index 0 (pass an empty
// string "" as the first element).
func BuildSysVHash(symNames []string) []byte {
	nchain := uint32(len(symNames))
	nbuckets := nchain
	if nbuckets == 0 {
		nbuckets = 1
	}

	buckets := make([]uint32, nbuckets)
	chains := make([]uint32, nchain)

	for i, name := range symNames {
		if name == "" {
			continue // null entry
		}
		symIdx := uint32(i)
		b := elfHash(name) % nbuckets
		chains[symIdx] = buckets[b]
		buckets[b] = symIdx
	}

	buf := make([]byte, 4*2+4*int(nbuckets)+4*int(nchain))
	off := 0
	putU32le(buf[off:], nbuckets); off += 4
	putU32le(buf[off:], nchain);   off += 4
	for _, b := range buckets {
		putU32le(buf[off:], b); off += 4
	}
	for _, c := range chains {
		putU32le(buf[off:], c); off += 4
	}
	return buf
}

// ── Symbol versioning builders ────────────────────────────────────────────────

// VersionNeed declares a versioned dependency on a shared library.
type VersionNeed struct {
	// Library is the DT_NEEDED library name, e.g. "libc.so.6".
	Library string

	// Versions lists the version strings required from Library,
	// e.g. ["GLIBC_2.5", "GLIBC_2.17"]. Version indices are assigned
	// in the order they appear across all VersionNeed entries, starting at 2
	// (0 = VER_NDX_LOCAL, 1 = VER_NDX_GLOBAL).
	Versions []string
}

// BuildVersionSym builds a .gnu.version (SHT_GNU_VERSYM) section body.
// indices must have one uint16 per .dynsym entry (including the null at [0]).
// Use VER_NDX_LOCAL (0), VER_NDX_GLOBAL (1), or a user-assigned index ≥ 2.
func BuildVersionSym(indices []uint16) []byte {
	buf := make([]byte, 2*len(indices))
	for i, idx := range indices {
		putU16le(buf[i*2:], idx)
	}
	return buf
}

// BuildVersionNeed builds a .gnu.version_r (SHT_GNU_VERNEED) section body.
// stringOffset is called for each library and version string; it must return
// that string's byte offset within the target .dynstr section. Callers are
// responsible for pre-interning all strings into .dynstr before calling.
func BuildVersionNeed(needs []VersionNeed, stringOffset func(string) uint32) []byte {
	if len(needs) == 0 {
		return nil
	}

	// Elf64_Verneed (16 bytes) followed immediately by Elf64_Vernaux (16 bytes each).
	const (
		verneedSize = 16
		vernauxSize = 16
	)

	var buf []byte
	versionIdx := uint16(2) // first user-defined index

	for ni, need := range needs {
		auxCount := uint16(len(need.Versions))
		vnNext := uint32(0)
		if ni+1 < len(needs) {
			vnNext = uint32(verneedSize + int(auxCount)*vernauxSize)
		}

		var vn [verneedSize]byte
		putU16le(vn[0:], 1)                              // vn_version = 1
		putU16le(vn[2:], auxCount)                        // vn_cnt
		putU32le(vn[4:], stringOffset(need.Library))      // vn_file
		putU32le(vn[8:], verneedSize)                     // vn_aux (immediately follows)
		putU32le(vn[12:], vnNext)                         // vn_next
		buf = append(buf, vn[:]...)

		for vi, ver := range need.Versions {
			vaNext := uint32(vernauxSize)
			if vi+1 == len(need.Versions) {
				vaNext = 0
			}
			var va [vernauxSize]byte
			putU32le(va[0:], elfHash(ver))           // vna_hash
			putU16le(va[4:], 0)                      // vna_flags
			putU16le(va[6:], versionIdx)             // vna_other (version index)
			putU32le(va[8:], stringOffset(ver))      // vna_name
			putU32le(va[12:], vaNext)                // vna_next
			buf = append(buf, va[:]...)
			versionIdx++
		}
	}
	return buf
}

// ── hash functions ────────────────────────────────────────────────────────────

// gnuHash implements the GNU symbol hash function (dl_new_hash).
func gnuHash(s string) uint32 {
	h := uint32(5381)
	for i := 0; i < len(s); i++ {
		h = h*33 + uint32(s[i])
	}
	return h
}

// elfHash implements the System V ELF symbol hash function.
func elfHash(s string) uint32 {
	var h uint32
	for i := 0; i < len(s); i++ {
		h = (h << 4) + uint32(s[i])
		if g := h & 0xF0000000; g != 0 {
			h ^= g >> 24
		}
		h &^= 0xF0000000
	}
	return h
}

// ── buildDynamicSections is called by the emitter ────────────────────────────

func (e *emitter) buildDynamicSections(dynSec *builtSection) {
	var dynstr strTab
	dynstr.add("")

	type dynEntry struct{ tag, val uint64 }
	var entries []dynEntry

	if e.b.soname != "" {
		entries = append(entries, dynEntry{DT_SONAME, uint64(dynstr.add(e.b.soname))})
	}
	for _, lib := range e.b.needed {
		entries = append(entries, dynEntry{DT_NEEDED, uint64(dynstr.add(lib))})
	}
	if e.b.rpath != "" {
		entries = append(entries, dynEntry{DT_RUNPATH, uint64(dynstr.add(e.b.rpath))})
	}

	if sec := e.secByName[".dynstr"]; sec != nil {
		entries = append(entries, dynEntry{DT_STRTAB, sec.addr})
	}
	if sec := e.secByName[".dynsym"]; sec != nil {
		entries = append(entries, dynEntry{DT_SYMTAB, sec.addr})
		entries = append(entries, dynEntry{DT_SYMENT, symEntrySize})
	}
	if sec := e.secByName[".rela.dyn"]; sec != nil {
		entries = append(entries, dynEntry{DT_RELA, sec.addr})
		entries = append(entries, dynEntry{DT_RELASZ, uint64(len(sec.data))})
		entries = append(entries, dynEntry{DT_RELAENT, relaEntrySize})
	}
	if sec := e.secByName[".rela.plt"]; sec != nil {
		entries = append(entries, dynEntry{DT_JMPREL, sec.addr})
		entries = append(entries, dynEntry{DT_PLTRELSZ, uint64(len(sec.data))})
		entries = append(entries, dynEntry{DT_PLTREL, DT_RELA})
	}
	if sec := e.secByName[".got.plt"]; sec != nil {
		entries = append(entries, dynEntry{DT_PLTGOT, sec.addr})
	}
	if sec := e.secByName[".gnu.hash"]; sec != nil {
		entries = append(entries, dynEntry{DT_GNU_HASH, sec.addr})
	}
	if sec := e.secByName[".hash"]; sec != nil {
		entries = append(entries, dynEntry{DT_HASH, sec.addr})
	}
	if sec := e.secByName[".gnu.version"]; sec != nil {
		entries = append(entries, dynEntry{DT_VERSYM, sec.addr})
	}
	if sec := e.secByName[".gnu.version_r"]; sec != nil {
		entries = append(entries, dynEntry{DT_VERNEED, sec.addr})
		// Count Verneed entries by scanning sh_info if set, else leave to caller.
		if sec.info > 0 {
			entries = append(entries, dynEntry{DT_VERNEEDNUM, uint64(sec.info)})
		}
	}

	// Finalize .dynstr size now that all strings are interned.
	dynstrData := dynstr.bytes()
	for i, en := range entries {
		if en.tag == DT_STRTAB {
			tail := make([]dynEntry, len(entries[i+1:]))
			copy(tail, entries[i+1:])
			entries = append(entries[:i+1], append([]dynEntry{{DT_STRSZ, uint64(len(dynstrData))}}, tail...)...)
			break
		}
	}

	entries = append(entries, dynEntry{DT_NULL, 0})

	var buf bytes.Buffer
	for _, en := range entries {
		var b [dynEntrySize]byte
		putU64le(b[0:], en.tag)
		putU64le(b[8:], en.val)
		buf.Write(b[:])
	}

	if sec := e.secByName[".dynstr"]; sec != nil {
		sec.data = dynstrData
		sec.memSize = uint64(len(dynstrData))
		if dynsym := e.secByName[".dynsym"]; dynsym != nil {
			dynsym.link = uint32(sec.shIdx)
			dynsym.data = make([]byte, symEntrySize) // null entry
			dynsym.memSize = symEntrySize
		}
	}
	if dynSec != nil {
		dynSec.data = buf.Bytes()
		dynSec.memSize = uint64(len(dynSec.data))
	}
}

// ── RELA helpers ──────────────────────────────────────────────────────────────

func appendRela(dst []byte, offset uint64, symIdx uint32, rType uint32, addend int64) []byte {
	info := (uint64(symIdx) << 32) | uint64(rType)
	var b [relaEntrySize]byte
	putU64le(b[0:], offset)
	putU64le(b[8:], info)
	putI64le(b[16:], addend)
	return append(dst, b[:]...)
}