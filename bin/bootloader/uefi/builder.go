// Package uefi constructs UEFI PE32+ executable images.
//
// UEFI images are a constrained subset of PE32+. Unlike general PE binaries
// they never use DLL imports — all firmware services are accessed through the
// EFI_SYSTEM_TABLE pointer passed to the entry point. The builder reflects
// this: there is no import or export API. You describe your sections, annotate
// any absolute pointers with [Reloc] entries so the builder can auto-generate
// the mandatory .reloc section, name your entry point, and call [Builder.Emit].
//
//	b := uefi.NewBuilder(uefi.ArchAMD64, uefi.SubsystemEFIApplication)
//
//	b.AddSection(uefi.Section{
//	    Name:  ".text",
//	    Chars: uefi.IMAGE_SCN_CNT_CODE | uefi.IMAGE_SCN_MEM_EXECUTE | uefi.IMAGE_SCN_MEM_READ,
//	    Data:  machineCode,
//	})
//
//	b.SetEntry("EfiMain")
//
//	out, err := b.Emit()
//	os.WriteFile("bootx64.efi", out, 0o755)
//
// The builder always enforces:
//   - PE32+ format (64-bit; magic 0x020B)
//   - Minimal 64-byte MZ header with no MS-DOS program stub
//   - .reloc section always present; base-relocation data directory always set
//   - DYNAMIC_BASE | NX_COMPAT | NO_SEH always set in DllCharacteristics
//   - SectionAlignment fixed at 4096 (UEFI CA memory-mitigation requirement)
//   - No section may combine IMAGE_SCN_MEM_WRITE and IMAGE_SCN_MEM_EXECUTE
package uefi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// -------------------------------------------------------------------------
// Public types
// -------------------------------------------------------------------------

// Arch is the target CPU architecture, encoded in the COFF Machine field.
type Arch uint16

const (
	ArchAMD64 Arch = 0x8664 // EFI_IMAGE_MACHINE_x64     (x86-64)
	ArchARM64 Arch = 0xAA64 // EFI_IMAGE_MACHINE_AARCH64 (AArch64)
)

// Subsystem is the EFI image type, encoded in the PE optional-header
// Subsystem field. The UEFI Boot Manager checks this field to determine
// whether an image is valid for the current boot phase.
type Subsystem uint16

const (
	// SubsystemEFIApplication is for bootloaders and boot managers.
	// Entry receives (EFI_HANDLE ImageHandle, EFI_SYSTEM_TABLE *SystemTable)
	// and returns an EFI_STATUS. The image exits via BS->Exit().
	SubsystemEFIApplication Subsystem = 10

	// SubsystemEFIBootService is for drivers active only during boot services.
	// The image is unloaded when ExitBootServices() is called.
	SubsystemEFIBootService Subsystem = 11

	// SubsystemEFIRuntime is for drivers that survive ExitBootServices().
	// The firmware calls SetVirtualAddressMap() to rebase the image after
	// boot, so the .reloc section must be complete and correct.
	SubsystemEFIRuntime Subsystem = 12
)

// Section is a named region of code or data placed in the UEFI image.
type Section struct {
	// Name is the PE section name. At most 8 bytes; silently truncated if longer.
	Name string

	// Chars is the bitwise OR of IMAGE_SCN_* flags describing content type
	// and virtual-memory permissions.
	//
	// UEFI NX policy forbids combining IMAGE_SCN_MEM_WRITE and
	// IMAGE_SCN_MEM_EXECUTE on the same section. Emit returns an error if
	// this constraint is violated.
	Chars uint32

	// Data is the raw section content. Padded to FileAlignment (512 B) in
	// the output file; the unpadded length is recorded as VirtualSize.
	Data []byte

	// Relocs lists every location within Data that holds a 64-bit absolute
	// address requiring a base-relocation fixup at load time. The builder
	// uses these to auto-generate the mandatory .reloc section.
	//
	// If your code is fully position-independent and contains no absolute
	// pointers, leave this nil. The .reloc section is still emitted (UEFI
	// requires it to be present) but will contain no fixup entries.
	Relocs []Reloc
}

// Reloc identifies a single base-relocation fixup site within a section.
// UEFI firmware applies the delta (actualLoadBase − ImageBase) to the
// value at the given offset whenever it loads the image at an address
// other than ImageBase.
type Reloc struct {
	// Offset is the byte offset within the containing section's Data.
	Offset uint32

	// Type is the relocation type. Use IMAGE_REL_BASED_DIR64 for every
	// 64-bit absolute pointer in AMD64 and ARM64 UEFI images.
	Type uint8
}

// Section characteristic flags (IMAGE_SCN_*).
const (
	IMAGE_SCN_CNT_CODE               uint32 = 0x0000_0020 // Section contains executable code.
	IMAGE_SCN_CNT_INITIALIZED_DATA   uint32 = 0x0000_0040 // Section contains initialized data.
	IMAGE_SCN_CNT_UNINITIALIZED_DATA uint32 = 0x0000_0080 // Section is zero-initialized; no raw data in file.
	IMAGE_SCN_MEM_DISCARDABLE        uint32 = 0x0200_0000 // Can be discarded after load (e.g. .reloc).
	IMAGE_SCN_MEM_NOT_PAGED          uint32 = 0x0800_0000 // Cannot be paged out (for runtime drivers).
	IMAGE_SCN_MEM_EXECUTE            uint32 = 0x2000_0000 // Mapped executable. Must not combine with MEM_WRITE.
	IMAGE_SCN_MEM_READ               uint32 = 0x4000_0000 // Mapped readable.
	IMAGE_SCN_MEM_WRITE              uint32 = 0x8000_0000 // Mapped writable. Must not combine with MEM_EXECUTE.
)

// -------------------------------------------------------------------------
// Builder
// -------------------------------------------------------------------------

// Builder assembles a UEFI PE32+ image from sections and emits a .efi binary.
type Builder struct {
	arch      Arch
	subsystem Subsystem
	sections  []Section
	symbols   map[string]symRef // name → location, registered via DefineSymbol
	entry     string
	imageBase uint64
}

// symRef is an internal symbol: a named offset within a named section.
type symRef struct {
	section string
	offset  uint32
}

// NewBuilder returns a Builder targeting the given CPU architecture and
// EFI subsystem type.
func NewBuilder(arch Arch, subsystem Subsystem) *Builder {
	return &Builder{
		arch:      arch,
		subsystem: subsystem,
		symbols:   make(map[string]symRef),
		imageBase: defaultImageBase,
	}
}

// AddSection appends a section to the image.
// Sections are laid out and emitted in the order they are added.
func (b *Builder) AddSection(s Section) {
	b.sections = append(b.sections, s)
}

// DefineSymbol associates name with a byte offset within a named section.
// Call this before SetEntry when the entry point is not at offset 0 of .text.
//
//	b.DefineSymbol("EfiMain", ".text", 0)
//	b.SetEntry("EfiMain")
func (b *Builder) DefineSymbol(name, section string, offset uint32) {
	b.symbols[name] = symRef{section: section, offset: offset}
}

// SetEntry names the image entry-point symbol (e.g. "EfiMain").
//
// Entry-point resolution order:
//  1. A symbol registered via [Builder.DefineSymbol] with this name.
//  2. A section whose Name equals this string (entry at offset 0).
//  3. The first section named ".text" (entry at offset 0).
//
// Emit returns an error if none of these succeed.
func (b *Builder) SetEntry(name string) {
	b.entry = name
}

// SetImageBase overrides the preferred load address (default 0x0040_0000).
// UEFI firmware always ignores this value and rebases the image using the
// .reloc section, but a non-zero ImageBase is required by the PE format.
func (b *Builder) SetImageBase(base uint64) {
	b.imageBase = base
}

// Emit serializes the builder state into a complete UEFI PE32+ binary.
// The returned bytes can be written directly to a .efi file.
func (b *Builder) Emit() ([]byte, error) {
	if b.entry == "" {
		return nil, errors.New("uefi: no entry point set; call SetEntry before Emit")
	}
	if len(b.sections) == 0 {
		return nil, errors.New("uefi: no sections added")
	}
	if err := b.checkSections(); err != nil {
		return nil, err
	}
	return b.serialize()
}

// -------------------------------------------------------------------------
// Internal constants
// -------------------------------------------------------------------------

const (
	pe32PlusMagic uint16 = 0x020B       // PE32+ optional-header magic
	peSig         uint32 = 0x0000_4550  // "PE\0\0"

	dosStubSize    = 64  // minimal MZ header; no MS-DOS program bytes
	coffHdrSize    = 20
	optHdrSize     = 240 // 24 standard + 88 Windows-specific + 16×8 data dirs
	sectionHdrSize = 40
	numDataDirs    = 16
	dirBaseReloc   = 5   // IMAGE_DIRECTORY_ENTRY_BASERELOC

	// SectionAlignment is fixed at 4096 (UEFI CA memory-mitigation requirement).
	sectAlign uint32 = 0x1000
	// FileAlignment: minimum allowed by PE spec; small images benefit from 512 B.
	fileAlign uint32 = 0x0200

	// Default preferred load address. Firmware always rebases via .reloc.
	defaultImageBase uint64 = 0x0000_0000_0040_0000

	// Conventional UEFI stack sizes.
	defaultStackReserve uint64 = 0x0000_0000_0020_0000 // 2 MB reserve
	defaultStackCommit  uint64 = 0x0000_0000_0000_1000 // 4 KB commit

	// COFF IMAGE_FILE_* characteristics applied to every UEFI image.
	imageFileExecutable        uint16 = 0x0002
	imageFileLargeAddressAware uint16 = 0x0020

	// DllCharacteristics enforced on every UEFI image:
	//   DYNAMIC_BASE  — firmware must use .reloc to rebase the image.
	//   NX_COMPAT     — no writable+executable memory; required for UEFI CA signing.
	//   NO_SEH        — UEFI has no structured-exception infrastructure.
	dllCharDynamicBase uint16 = 0x0040
	dllCharNXCompat    uint16 = 0x0100
	dllCharNoSEH       uint16 = 0x0400
)

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

func (b *Builder) checkSections() error {
	for _, s := range b.sections {
		if s.Chars&IMAGE_SCN_MEM_WRITE != 0 && s.Chars&IMAGE_SCN_MEM_EXECUTE != 0 {
			return fmt.Errorf("uefi: section %q combines IMAGE_SCN_MEM_WRITE and "+
				"IMAGE_SCN_MEM_EXECUTE; UEFI NX policy forbids W+X sections", s.Name)
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Serialization
// -------------------------------------------------------------------------

// sectionSlot holds the computed RVA and file position of one section.
type sectionSlot struct {
	rva      uint32
	fileOff  uint32
	virtSize uint32 // actual content length (unpadded)
	rawSize  uint32 // length in the file (padded to fileAlign)
}

func (b *Builder) serialize() ([]byte, error) {
	// The image contains: user sections + auto-generated .reloc.
	totalSections := len(b.sections) + 1

	// ── SizeOfHeaders ────────────────────────────────────────────────────
	rawHdr := uint32(dosStubSize + 4 + coffHdrSize + optHdrSize +
		totalSections*sectionHdrSize)
	sizeOfHeaders := alignUp(rawHdr, fileAlign)

	// ── Lay out user sections ─────────────────────────────────────────────
	slots := make([]sectionSlot, len(b.sections))
	nextRVA := alignUp(sizeOfHeaders, sectAlign)
	nextFile := sizeOfHeaders
	for i, sec := range b.sections {
		vs := uint32(len(sec.Data))
		rs := alignUp(vs, fileAlign)
		slots[i] = sectionSlot{
			rva:      nextRVA,
			fileOff:  nextFile,
			virtSize: vs,
			rawSize:  rs,
		}
		nextRVA = alignUp(nextRVA+vs, sectAlign)
		nextFile += rs
	}

	// ── Build .reloc ──────────────────────────────────────────────────────
	rvas := make([]uint32, len(slots))
	for i, s := range slots {
		rvas[i] = s.rva
	}
	relocData := buildReloc(b.sections, rvas)
	relocVS := uint32(len(relocData))
	relocRS := alignUp(relocVS, fileAlign)
	relocSlot := sectionSlot{
		rva:      nextRVA,
		fileOff:  nextFile,
		virtSize: relocVS,
		rawSize:  relocRS,
	}

	// ── SizeOfImage ───────────────────────────────────────────────────────
	sizeOfImage := alignUp(relocSlot.rva+relocVS, sectAlign)

	// ── Entry-point RVA ───────────────────────────────────────────────────
	entryRVA, err := b.resolveEntry(slots)
	if err != nil {
		return nil, err
	}

	// ── Optional-header aggregate fields ──────────────────────────────────
	var sizeOfCode, sizeOfInitData, sizeOfUninitData, baseOfCode uint32
	for i, sec := range b.sections {
		sz := alignUp(uint32(len(sec.Data)), fileAlign)
		switch {
		case sec.Chars&IMAGE_SCN_CNT_CODE != 0:
			sizeOfCode += sz
			if baseOfCode == 0 {
				baseOfCode = slots[i].rva
			}
		case sec.Chars&IMAGE_SCN_CNT_UNINITIALIZED_DATA != 0:
			sizeOfUninitData += sz
		default:
			sizeOfInitData += sz
		}
	}
	sizeOfInitData += relocRS // .reloc is initialized data

	// ── Write ─────────────────────────────────────────────────────────────
	var buf bytes.Buffer

	// DOS stub — 64-byte minimal MZ header; e_lfanew points past it.
	// No MS-DOS program: bytes 2–59 are zero, making this inert if
	// someone tries to run it under DOS.
	var dos [dosStubSize]byte
	dos[0] = 'M'
	dos[1] = 'Z'
	binary.LittleEndian.PutUint32(dos[0x3C:], dosStubSize) // e_lfanew = 0x40
	buf.Write(dos[:])

	// PE signature
	write32(&buf, peSig)

	// COFF file header (20 bytes)
	write16(&buf, uint16(b.arch))
	write16(&buf, uint16(totalSections))
	write32(&buf, 0)                 // TimeDateStamp
	write32(&buf, 0)                 // PointerToSymbolTable
	write32(&buf, 0)                 // NumberOfSymbols
	write16(&buf, uint16(optHdrSize))
	write16(&buf, imageFileExecutable|imageFileLargeAddressAware)

	// Optional header — standard fields, PE32+ (24 bytes, no BaseOfData)
	write16(&buf, pe32PlusMagic)
	buf.WriteByte(0) // MajorLinkerVersion
	buf.WriteByte(0) // MinorLinkerVersion
	write32(&buf, sizeOfCode)
	write32(&buf, sizeOfInitData)
	write32(&buf, sizeOfUninitData)
	write32(&buf, entryRVA)  // AddressOfEntryPoint
	write32(&buf, baseOfCode) // BaseOfCode

	// Optional header — Windows-specific fields (88 bytes)
	write64(&buf, b.imageBase)
	write32(&buf, sectAlign)
	write32(&buf, fileAlign)
	write16(&buf, 0) // MajorOperatingSystemVersion
	write16(&buf, 0) // MinorOperatingSystemVersion
	write16(&buf, 0) // MajorImageVersion
	write16(&buf, 0) // MinorImageVersion
	write16(&buf, 0) // MajorSubsystemVersion
	write16(&buf, 0) // MinorSubsystemVersion
	write32(&buf, 0) // Win32VersionValue — reserved, must be zero
	write32(&buf, sizeOfImage)
	write32(&buf, sizeOfHeaders)
	write32(&buf, 0) // CheckSum — zero is valid for non-boot-critical images
	write16(&buf, uint16(b.subsystem))
	write16(&buf, dllCharDynamicBase|dllCharNXCompat|dllCharNoSEH)
	write64(&buf, defaultStackReserve)
	write64(&buf, defaultStackCommit)
	write64(&buf, 0) // SizeOfHeapReserve
	write64(&buf, 0) // SizeOfHeapCommit
	write32(&buf, 0) // LoaderFlags — reserved, must be zero
	write32(&buf, numDataDirs)

	// Data directories (16 × 8 bytes).
	// Only the base-relocation directory is populated; all others are zero.
	for i := 0; i < numDataDirs; i++ {
		if i == dirBaseReloc {
			write32(&buf, relocSlot.rva)
			write32(&buf, relocVS)
		} else {
			write64(&buf, 0)
		}
	}

	// Section headers — user sections
	for i, sec := range b.sections {
		writeSectionHeader(&buf, sec.Name,
			slots[i].rva, slots[i].virtSize,
			slots[i].rawSize, slots[i].fileOff,
			sec.Chars)
	}
	// Section header — .reloc
	writeSectionHeader(&buf, ".reloc",
		relocSlot.rva, relocVS,
		relocRS, relocSlot.fileOff,
		IMAGE_SCN_CNT_INITIALIZED_DATA|IMAGE_SCN_MEM_DISCARDABLE|IMAGE_SCN_MEM_READ)

	// Pad headers to SizeOfHeaders
	writeZeros(&buf, int(sizeOfHeaders)-buf.Len())

	// Section raw data — user sections
	for i, sec := range b.sections {
		buf.Write(sec.Data)
		writeZeros(&buf, int(slots[i].rawSize)-len(sec.Data))
	}

	// Section raw data — .reloc
	buf.Write(relocData)
	writeZeros(&buf, int(relocRS)-len(relocData))

	return buf.Bytes(), nil
}

// resolveEntry walks the resolution order described in SetEntry.
func (b *Builder) resolveEntry(slots []sectionSlot) (uint32, error) {
	// Build name → RVA for fast lookup.
	byName := make(map[string]uint32, len(b.sections))
	for i, sec := range b.sections {
		byName[sec.Name] = slots[i].rva
	}

	// 1. Explicit symbol registered via DefineSymbol.
	if sym, ok := b.symbols[b.entry]; ok {
		rva, ok := byName[sym.section]
		if !ok {
			return 0, fmt.Errorf("uefi: symbol %q references unknown section %q",
				b.entry, sym.section)
		}
		return rva + sym.offset, nil
	}

	// 2. A section whose name matches the entry string.
	if rva, ok := byName[b.entry]; ok {
		return rva, nil
	}

	// 3. Fall back to the start of .text.
	if rva, ok := byName[".text"]; ok {
		return rva, nil
	}

	return 0, fmt.Errorf("uefi: cannot resolve entry point %q: "+
		"no matching symbol, no section named %q, and no .text section",
		b.entry, b.entry)
}

// -------------------------------------------------------------------------
// Binary write helpers
// -------------------------------------------------------------------------

func write16(w *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	w.Write(b[:])
}

func write32(w *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.Write(b[:])
}

func write64(w *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	w.Write(b[:])
}

func writeZeros(w *bytes.Buffer, n int) {
	if n > 0 {
		w.Write(make([]byte, n))
	}
}

func writeSectionHeader(w *bytes.Buffer, name string,
	rva, virtSize, rawSize, fileOff, chars uint32) {

	var nameBuf [8]byte
	copy(nameBuf[:], name) // silently truncates names longer than 8 bytes
	w.Write(nameBuf[:])
	write32(w, virtSize)
	write32(w, rva)
	write32(w, rawSize)
	write32(w, fileOff)
	write32(w, 0) // PointerToRelocations (0 for linked images)
	write32(w, 0) // PointerToLinenumbers (deprecated)
	write16(w, 0) // NumberOfRelocations
	write16(w, 0) // NumberOfLinenumbers
	write32(w, chars)
}

func alignUp(v, align uint32) uint32 {
	return (v + align - 1) &^ (align - 1)
}