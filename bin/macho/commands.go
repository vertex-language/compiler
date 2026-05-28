package macho

import "encoding/binary"

// ──────────────────────────────────────────────────────────────────────────────
// Load command type constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	lcSegment64          uint32 = 0x19         // LC_SEGMENT_64
	lcSymtab             uint32 = 0x02         // LC_SYMTAB
	lcDysymtab           uint32 = 0x0b         // LC_DYSYMTAB
	lcLoadDylinker       uint32 = 0x0e         // LC_LOAD_DYLINKER
	lcIDDylinker         uint32 = 0x0f         // LC_ID_DYLINKER
	lcLoadDylib          uint32 = 0x0c         // LC_LOAD_DYLIB
	lcIDDylib            uint32 = 0x0d         // LC_ID_DYLIB
	lcLoadWeakDylib      uint32 = 0x80000018   // LC_LOAD_WEAK_DYLIB
	lcReexportDylib      uint32 = 0x8000001f   // LC_REEXPORT_DYLIB
	lcLazyLoadDylib      uint32 = 0x20         // LC_LAZY_LOAD_DYLIB
	lcLoadUpwardDylib    uint32 = 0x80000023   // LC_LOAD_UPWARD_DYLIB
	lcMain               uint32 = 0x80000028   // LC_MAIN
	lcUUID               uint32 = 0x1b         // LC_UUID
	lcRpath              uint32 = 0x8000001c   // LC_RPATH
	lcBuildVersion       uint32 = 0x32         // LC_BUILD_VERSION
	lcSourceVersion      uint32 = 0x2a         // LC_SOURCE_VERSION
	lcDyldInfo           uint32 = 0x22         // LC_DYLD_INFO
	lcDyldInfoOnly       uint32 = 0x80000022   // LC_DYLD_INFO_ONLY
	lcDyldChainedFixups  uint32 = 0x80000034   // LC_DYLD_CHAINED_FIXUPS
	lcDyldExportsTrie    uint32 = 0x80000033   // LC_DYLD_EXPORTS_TRIE
	lcFunctionStarts     uint32 = 0x26         // LC_FUNCTION_STARTS
	lcDataInCode         uint32 = 0x29         // LC_DATA_IN_CODE
	lcCodeSignature      uint32 = 0x1d         // LC_CODE_SIGNATURE
	lcLinkerOption       uint32 = 0x2d         // LC_LINKER_OPTION
	lcVersionMinMacOS    uint32 = 0x24         // LC_VERSION_MIN_MACOSX (legacy)
	lcVersionMinIPhoneOS uint32 = 0x25         // LC_VERSION_MIN_IPHONEOS (legacy)
)

// ──────────────────────────────────────────────────────────────────────────────
// Mach-O header constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	mhMagic64 uint32 = 0xfeedfacf
)

// nlist_64 N_TYPE mask values.
const (
	nExt  uint8 = 0x01 // N_EXT  — external (global)
	nPExt uint8 = 0x10 // N_PEXT — private external
	nSect uint8 = 0x0e // N_SECT — symbol in a section
	nUndf uint8 = 0x00 // N_UNDF — undefined
	nAbs  uint8 = 0x02 // N_ABS  — absolute
)

// nlist_64 n_desc flag bits.
const (
	nDescWeakRef  uint16 = 0x0040 // N_WEAK_REF
	nDescWeakDef  uint16 = 0x0080 // N_WEAK_DEF
	nDescAltEntry uint16 = 0x0200 // N_ALT_ENTRY
)

// ──────────────────────────────────────────────────────────────────────────────
// Fixed structure sizes
// ──────────────────────────────────────────────────────────────────────────────

const (
	sizeofMachHeader64        = 32
	sizeofSegmentCommand64    = 64
	sizeofSection64           = 80
	sizeofSymtabCommand       = 24
	sizeofDysymtabCommand     = 80
	sizeofEntryPointCommand   = 24 // LC_MAIN (entry_point_command)
	sizeofDylibCommand        = 24 // base of dylib_command (before path string)
	sizeofLoadDylinkerCommand = 12 // base of dylinker_command
	sizeofNlist64             = 16
	sizeofRelocEntry          = 8
	sizeofUUIDCommand         = 24 // uuid_command
	sizeofRpathCommandBase    = 12 // rpath_command base (before path string)
	sizeofBuildVersionCommand = 24 // build_version_command (without tool entries)
	sizeofBuildToolVersion    = 8  // build_tool_version
	sizeofSourceVersionCommand = 16 // source_version_command
	sizeofDyldInfoCommand     = 48 // dyld_info_command
	sizeofLinkeditDataCommand = 16 // linkedit_data_command (code sig, function starts, etc.)
	sizeofDataInCodeEntry     = 8  // data_in_code_entry
	sizeofLinkerOptionBase    = 8  // linker_option_command base (before strings)
)

// ──────────────────────────────────────────────────────────────────────────────
// Little-endian write helpers
// ──────────────────────────────────────────────────────────────────────────────

func putU16(buf []byte, off int, v uint16) {
	binary.LittleEndian.PutUint16(buf[off:], v)
}

func putU32(buf []byte, off int, v uint32) {
	binary.LittleEndian.PutUint32(buf[off:], v)
}

func putU64(buf []byte, off int, v uint64) {
	binary.LittleEndian.PutUint64(buf[off:], v)
}

// putStr16 writes s zero-padded into a 16-byte field.
func putStr16(dst []byte, s string) {
	for i := 0; i < 16; i++ {
		if i < len(s) {
			dst[i] = s[i]
		} else {
			dst[i] = 0
		}
	}
}

// alignUp rounds v up to the next multiple of align (power of two).
func alignUp(v, align uint64) uint64 {
	if align == 0 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}

// log2ceil returns ⌈log2(v)⌉ for alignment field encoding.
func log2ceil(v uint32) uint32 {
	if v <= 1 {
		return 0
	}
	n := uint32(0)
	for (uint32(1) << n) < v {
		n++
	}
	return n
}

// ──────────────────────────────────────────────────────────────────────────────
// Emit functions — each writes one load command (or header) into buf at off
// and returns the next offset.
// ──────────────────────────────────────────────────────────────────────────────

func emitMachHeader64(buf []byte, arch Arch, filetype uint32, flags MHFlags, ncmds, sizeofcmds uint32) {
	putU32(buf, 0, mhMagic64)
	putU32(buf, 4, uint32(arch))
	putU32(buf, 8, arch.cpuSubtype())
	putU32(buf, 12, filetype)
	putU32(buf, 16, ncmds)
	putU32(buf, 20, sizeofcmds)
	putU32(buf, 24, uint32(flags))
	putU32(buf, 28, 0) // reserved
}

func emitSegmentCommand64(buf []byte, off int,
	name string,
	vmaddr, vmsize, fileoff, filesize uint64,
	maxprot, initprot Prot,
	nsects uint32,
	flags SegFlags,
) int {
	cmdsize := uint32(sizeofSegmentCommand64 + int(nsects)*sizeofSection64)
	putU32(buf, off+0, lcSegment64)
	putU32(buf, off+4, cmdsize)
	putStr16(buf[off+8:], name)
	putU64(buf, off+24, vmaddr)
	putU64(buf, off+32, vmsize)
	putU64(buf, off+40, fileoff)
	putU64(buf, off+48, filesize)
	putU32(buf, off+56, uint32(maxprot))
	putU32(buf, off+60, uint32(initprot))
	putU32(buf, off+64, nsects)
	putU32(buf, off+68, uint32(flags))
	return off + sizeofSegmentCommand64
}

func emitSection64(buf []byte, off int,
	sectName, segName string,
	addr, size uint64,
	fileoff, alignLog, reloff, nreloc, flags, reserved1, reserved2 uint32,
) int {
	putStr16(buf[off+0:], sectName)
	putStr16(buf[off+16:], segName)
	putU64(buf, off+32, addr)
	putU64(buf, off+40, size)
	putU32(buf, off+48, fileoff)
	putU32(buf, off+52, alignLog)
	putU32(buf, off+56, reloff)
	putU32(buf, off+60, nreloc)
	putU32(buf, off+64, flags)
	putU32(buf, off+68, reserved1)
	putU32(buf, off+72, reserved2)
	putU32(buf, off+76, 0) // reserved3
	return off + sizeofSection64
}

func emitSymtabCommand(buf []byte, off int, symoff, nsyms, stroff, strsize uint32) int {
	putU32(buf, off+0, lcSymtab)
	putU32(buf, off+4, sizeofSymtabCommand)
	putU32(buf, off+8, symoff)
	putU32(buf, off+12, nsyms)
	putU32(buf, off+16, stroff)
	putU32(buf, off+20, strsize)
	return off + sizeofSymtabCommand
}

func emitDysymtabCommand(buf []byte, off int,
	ilocal, nlocal, iextdef, nextdef, iundef, nundef uint32,
	indirectSymOff, nIndirectSyms uint32,
) int {
	putU32(buf, off+0, lcDysymtab)
	putU32(buf, off+4, sizeofDysymtabCommand)
	putU32(buf, off+8, ilocal)
	putU32(buf, off+12, nlocal)
	putU32(buf, off+16, iextdef)
	putU32(buf, off+20, nextdef)
	putU32(buf, off+24, iundef)
	putU32(buf, off+28, nundef)
	// tocoff, ntoc, modtaboff, nmodtab, extrefsymoff, nextrefsyms = 0
	for i := 32; i < 56; i++ {
		buf[off+i] = 0
	}
	putU32(buf, off+56, indirectSymOff)
	putU32(buf, off+60, nIndirectSyms)
	// extreloff, nextrel, locreloff, nlocrel = 0
	for i := 64; i < sizeofDysymtabCommand; i++ {
		buf[off+i] = 0
	}
	return off + sizeofDysymtabCommand
}

func emitMainCommand(buf []byte, off int, entryoff uint64) int {
	putU32(buf, off+0, lcMain)
	putU32(buf, off+4, sizeofEntryPointCommand)
	putU64(buf, off+8, entryoff)
	putU64(buf, off+16, 0) // stacksize (0 = default)
	return off + sizeofEntryPointCommand
}

func emitLoadDylinkerCommand(buf []byte, off int, path string) int {
	pb := []byte(path)
	total := int(alignUp(uint64(sizeofLoadDylinkerCommand)+uint64(len(pb))+1, 8))
	putU32(buf, off+0, lcLoadDylinker)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(sizeofLoadDylinkerCommand)) // name offset
	copy(buf[off+sizeofLoadDylinkerCommand:], pb)
	buf[off+sizeofLoadDylinkerCommand+len(pb)] = 0
	return off + total
}

// dylibCmdFor maps DylibKind to the correct LC_* constant.
func dylibCmdFor(kind DylibKind) uint32 {
	switch kind {
	case DylibWeak:
		return lcLoadWeakDylib
	case DylibReexport:
		return lcReexportDylib
	case DylibLazy:
		return lcLazyLoadDylib
	case DylibUpward:
		return lcLoadUpwardDylib
	default:
		return lcLoadDylib
	}
}

func emitDylibCommand(buf []byte, off int, cmd uint32, ref DylibRef) int {
	pb := []byte(ref.Path)
	total := int(alignUp(uint64(sizeofDylibCommand)+uint64(len(pb))+1, 8))
	putU32(buf, off+0, cmd)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(sizeofDylibCommand)) // name offset within command
	putU32(buf, off+12, 0)                          // timestamp (0 = build-time placeholder)
	putU32(buf, off+16, ref.CurrentVersion)
	putU32(buf, off+20, ref.CompatVersion)
	copy(buf[off+sizeofDylibCommand:], pb)
	buf[off+sizeofDylibCommand+len(pb)] = 0
	return off + total
}

func emitIDDylibCommand(buf []byte, off int, ref DylibRef) int {
	return emitDylibCommand(buf, off, lcIDDylib, ref)
}

func emitUUIDCommand(buf []byte, off int, uuid [16]byte) int {
	putU32(buf, off+0, lcUUID)
	putU32(buf, off+4, sizeofUUIDCommand)
	copy(buf[off+8:], uuid[:])
	return off + sizeofUUIDCommand
}

func emitRpathCommand(buf []byte, off int, path string) int {
	pb := []byte(path)
	total := int(alignUp(uint64(sizeofRpathCommandBase)+uint64(len(pb))+1, 8))
	putU32(buf, off+0, lcRpath)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(sizeofRpathCommandBase)) // path offset within command
	copy(buf[off+sizeofRpathCommandBase:], pb)
	buf[off+sizeofRpathCommandBase+len(pb)] = 0
	return off + total
}

func emitBuildVersionCommand(buf []byte, off int, bv BuildVersion) int {
	ntools := uint32(len(bv.Tools))
	total := sizeofBuildVersionCommand + int(ntools)*sizeofBuildToolVersion
	putU32(buf, off+0, lcBuildVersion)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(bv.Platform))
	putU32(buf, off+12, bv.MinOS)
	putU32(buf, off+16, bv.SDK)
	putU32(buf, off+20, ntools)
	for i, t := range bv.Tools {
		base := off + sizeofBuildVersionCommand + i*sizeofBuildToolVersion
		putU32(buf, base+0, uint32(t.Tool))
		putU32(buf, base+4, t.Version)
	}
	return off + total
}

func emitSourceVersionCommand(buf []byte, off int, version uint64) int {
	putU32(buf, off+0, lcSourceVersion)
	putU32(buf, off+4, sizeofSourceVersionCommand)
	putU64(buf, off+8, version)
	return off + sizeofSourceVersionCommand
}

// emitDyldInfoCommand emits LC_DYLD_INFO_ONLY pointing into __LINKEDIT.
func emitDyldInfoCommand(buf []byte, off int,
	rebaseOff, rebaseSize,
	bindOff, bindSize,
	weakBindOff, weakBindSize,
	lazyBindOff, lazyBindSize,
	exportOff, exportSize uint32,
) int {
	putU32(buf, off+0, lcDyldInfoOnly)
	putU32(buf, off+4, sizeofDyldInfoCommand)
	putU32(buf, off+8, rebaseOff)
	putU32(buf, off+12, rebaseSize)
	putU32(buf, off+16, bindOff)
	putU32(buf, off+20, bindSize)
	putU32(buf, off+24, weakBindOff)
	putU32(buf, off+28, weakBindSize)
	putU32(buf, off+32, lazyBindOff)
	putU32(buf, off+36, lazyBindSize)
	putU32(buf, off+40, exportOff)
	putU32(buf, off+44, exportSize)
	return off + sizeofDyldInfoCommand
}

// emitLinkeditDataCommand emits a linkedit_data_command (code sig, function
// starts, data-in-code, exports trie, chained fixups).
func emitLinkeditDataCommand(buf []byte, off int, cmd uint32, dataoff, datasize uint32) int {
	putU32(buf, off+0, cmd)
	putU32(buf, off+4, sizeofLinkeditDataCommand)
	putU32(buf, off+8, dataoff)
	putU32(buf, off+12, datasize)
	return off + sizeofLinkeditDataCommand
}

// emitLinkerOptionCommand emits LC_LINKER_OPTION with a slice of NUL-terminated strings.
func emitLinkerOptionCommand(buf []byte, off int, opts []string) int {
	// Compute raw string bytes.
	var raw []byte
	for _, o := range opts {
		raw = append(raw, []byte(o)...)
		raw = append(raw, 0)
	}
	total := int(alignUp(uint64(sizeofLinkerOptionBase)+uint64(len(raw)), 8))
	putU32(buf, off+0, lcLinkerOption)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(len(opts)))
	copy(buf[off+sizeofLinkerOptionBase:], raw)
	return off + total
}

func emitNlist64(buf []byte, off int, strx uint32, ntype, nsect uint8, ndesc uint16, value uint64) int {
	putU32(buf, off+0, strx)
	buf[off+4] = ntype
	buf[off+5] = nsect
	binary.LittleEndian.PutUint16(buf[off+6:], ndesc)
	putU64(buf, off+8, value)
	return off + sizeofNlist64
}

func emitRelocEntry(buf []byte, off int, r Reloc, symOrSectIdx uint32) int {
	putU32(buf, off+0, r.Offset)
	packed := symOrSectIdx & 0x00ffffff
	if r.PCRel {
		packed |= 1 << 24
	}
	packed |= uint32(r.Length&0x3) << 25
	if r.Extern {
		packed |= 1 << 27
	}
	packed |= uint32(r.Type) << 28
	putU32(buf, off+4, packed)
	return off + sizeofRelocEntry
}

func emitDataInCodeEntry(buf []byte, off int, e DataInCodeEntry) int {
	putU32(buf, off+0, e.Offset)
	putU16(buf, off+4, e.Length)
	putU16(buf, off+6, uint16(e.Kind))
	return off + sizeofDataInCodeEntry
}