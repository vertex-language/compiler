package macho

import (
	"encoding/binary"
)

// Load command type constants (cmd field of every load command).
const (
	lcSegment64   uint32 = 0x19 // LC_SEGMENT_64
	lcSymtab      uint32 = 0x02 // LC_SYMTAB
	lcDysymtab    uint32 = 0x0b // LC_DYSYMTAB
	lcLoadDylinker uint32 = 0x0e // LC_LOAD_DYLINKER
	lcLoadDylib   uint32 = 0x0c // LC_LOAD_DYLIB
	lcMain        uint32 = 0x80000028 // LC_MAIN (versioned; requires MH_EXECUTE)
)

// Mach-O header magic and file type constants.
const (
	mhMagic64   uint32 = 0xfeedfacf
	mhExecute   uint32 = 0x2
	mhDylib     uint32 = 0x6
	mhNoundefs  uint32 = 0x1
	mhDyldlink  uint32 = 0x4
	mhTwolevel  uint32 = 0x80
	mhPIE       uint32 = 0x200000
)

// nlist_64 N_TYPE masks and values.
const (
	nExt  uint8 = 0x01 // N_EXT  — external (global) symbol
	nSect uint8 = 0x0e // N_SECT — defined in a section
	nUndf uint8 = 0x00 // N_UNDF — undefined
)

// putU32 appends a little-endian uint32 to buf.
func putU32(buf []byte, off int, v uint32) {
	binary.LittleEndian.PutUint32(buf[off:], v)
}

// putU64 appends a little-endian uint64 to buf.
func putU64(buf []byte, off int, v uint64) {
	binary.LittleEndian.PutUint64(buf[off:], v)
}

// putStr16 writes s into a fixed [16]byte field, zero-padded.
func putStr16(dst []byte, s string) {
	for i := range 16 {
		if i < len(s) {
			dst[i] = s[i]
		} else {
			dst[i] = 0
		}
	}
}

// alignUp rounds v up to the next multiple of align (which must be a power of two).
func alignUp(v, align uint64) uint64 {
	if align == 0 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}

// sizeofMachHeader64 is the fixed size of mach_header_64.
const sizeofMachHeader64 = 32

// sizeofSegmentCommand64 is the fixed size of segment_command_64 (without sections).
const sizeofSegmentCommand64 = 64

// sizeofSection64 is the fixed size of section_64.
const sizeofSection64 = 80

// sizeofSymtabCommand is the fixed size of symtab_command.
const sizeofSymtabCommand = 24

// sizeofDysymtabCommand is the fixed size of dysymtab_command.
const sizeofDysymtabCommand = 80

// sizeofEntryPointCommand is the fixed size of entry_point_command (LC_MAIN).
const sizeofEntryPointCommand = 24

// sizeofDylibCommand is the base size of dylib_command (before the name string).
const sizeofDylibCommand = 24

// sizeofLoadDylinkerCommand is the base size of dylinker_command.
const sizeofLoadDylinkerCommand = 12

// sizeofNlist64 is the size of nlist_64.
const sizeofNlist64 = 16

// sizeofRelocEntry is the size of a relocation_info entry.
const sizeofRelocEntry = 8

// emitMachHeader64 writes a mach_header_64 into buf at offset 0.
// ncmds and sizeofcmds must be the final values.
func emitMachHeader64(buf []byte, arch Arch, filetype, flags, ncmds, sizeofcmds uint32) {
	putU32(buf, 0, mhMagic64)
	putU32(buf, 4, uint32(arch))
	putU32(buf, 8, arch.cpuSubtype())
	putU32(buf, 12, filetype)
	putU32(buf, 16, ncmds)
	putU32(buf, 20, sizeofcmds)
	putU32(buf, 24, flags)
	putU32(buf, 28, 0) // reserved
}

// emitSegmentCommand64 serialises a segment_command_64 (without sections) into buf at off.
// Returns the offset just after the command header (where section_64s should follow).
func emitSegmentCommand64(buf []byte, off int, name string, vmaddr, vmsize, fileoff, filesize uint64, maxprot, initprot Prot, nsects, flags uint32) int {
	cmdsize := uint32(sizeofSegmentCommand64 + int(nsects)*sizeofSection64)
	putU32(buf, off, lcSegment64)
	putU32(buf, off+4, cmdsize)
	putStr16(buf[off+8:], name)
	putU64(buf, off+24, vmaddr)
	putU64(buf, off+32, vmsize)
	putU64(buf, off+40, fileoff)
	putU64(buf, off+48, filesize)
	putU32(buf, off+56, uint32(maxprot))
	putU32(buf, off+60, uint32(initprot))
	putU32(buf, off+64, nsects)
	putU32(buf, off+68, flags)
	return off + sizeofSegmentCommand64
}

// emitSection64 serialises one section_64 into buf at off.
// reloff is the file offset of the section's relocation array; nreloc is its count.
func emitSection64(buf []byte, off int, sectName, segName string, addr, size uint64, fileoff, align, reloff, nreloc, flags uint32) int {
	putStr16(buf[off:], sectName)
	putStr16(buf[off+16:], segName)
	putU64(buf, off+32, addr)
	putU64(buf, off+40, size)
	putU32(buf, off+48, fileoff)
	// Align is stored as log2 of the byte count.
	alignLog := uint32(0)
	for (uint32(1) << alignLog) < align && alignLog < 31 {
		alignLog++
	}
	putU32(buf, off+52, alignLog)
	putU32(buf, off+56, reloff)
	putU32(buf, off+60, nreloc)
	putU32(buf, off+64, flags)
	putU32(buf, off+68, 0) // reserved1
	putU32(buf, off+72, 0) // reserved2
	putU32(buf, off+76, 0) // reserved3
	return off + sizeofSection64
}

// emitSymtabCommand serialises LC_SYMTAB into buf at off.
func emitSymtabCommand(buf []byte, off int, symoff, nsyms, stroff, strsize uint32) int {
	putU32(buf, off, lcSymtab)
	putU32(buf, off+4, uint32(sizeofSymtabCommand))
	putU32(buf, off+8, symoff)
	putU32(buf, off+12, nsyms)
	putU32(buf, off+16, stroff)
	putU32(buf, off+20, strsize)
	return off + sizeofSymtabCommand
}

// emitDysymtabCommand serialises LC_DYSYMTAB into buf at off.
// For a simple executable almost all counts are zero; only ilocalsym/nlocalsym
// and iextdefsym/nextdefsym are meaningful.
func emitDysymtabCommand(buf []byte, off int, ilocal, nlocal, iextdef, nextdef, iundef, nundef uint32) int {
	putU32(buf, off, lcDysymtab)
	putU32(buf, off+4, uint32(sizeofDysymtabCommand))
	putU32(buf, off+8, ilocal)
	putU32(buf, off+12, nlocal)
	putU32(buf, off+16, iextdef)
	putU32(buf, off+20, nextdef)
	putU32(buf, off+24, iundef)
	putU32(buf, off+28, nundef)
	// remaining 52 bytes (indirectsymoff … modtabsize) are all zero
	for i := 32; i < sizeofDysymtabCommand; i++ {
		buf[off+i] = 0
	}
	return off + sizeofDysymtabCommand
}

// emitMainCommand serialises LC_MAIN (entry_point_command) into buf at off.
// entryoff is the file offset of the entry point (from the start of __TEXT).
func emitMainCommand(buf []byte, off int, entryoff uint64) int {
	putU32(buf, off, lcMain)
	putU32(buf, off+4, uint32(sizeofEntryPointCommand))
	putU64(buf, off+8, entryoff)
	putU64(buf, off+16, 0) // stacksize (0 = default)
	return off + sizeofEntryPointCommand
}

// emitLoadDylinkerCommand serialises LC_LOAD_DYLINKER into buf at off.
// The conventional path is "/usr/lib/dyld".
func emitLoadDylinkerCommand(buf []byte, off int, path string) int {
	// name offset within the command is always 12 (the struct base size).
	pathBytes := []byte(path)
	// total size: header (12) + path + NUL, rounded up to 8-byte boundary.
	total := alignUp(uint64(sizeofLoadDylinkerCommand)+uint64(len(pathBytes))+1, 8)
	putU32(buf, off, lcLoadDylinker)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(sizeofLoadDylinkerCommand)) // name offset
	copy(buf[off+sizeofLoadDylinkerCommand:], pathBytes)
	buf[off+sizeofLoadDylinkerCommand+len(pathBytes)] = 0
	return off + int(total)
}

// emitLoadDylibCommand serialises one LC_LOAD_DYLIB into buf at off.
func emitLoadDylibCommand(buf []byte, off int, ref DylibRef) int {
	pathBytes := []byte(ref.Path)
	// name offset within dylib_command is always 24 (the struct base size).
	total := int(alignUp(uint64(sizeofDylibCommand)+uint64(len(pathBytes))+1, 8))
	putU32(buf, off, lcLoadDylib)
	putU32(buf, off+4, uint32(total))
	putU32(buf, off+8, uint32(sizeofDylibCommand))    // name offset within command
	putU32(buf, off+12, 0)                             // timestamp (0 = unset)
	putU32(buf, off+16, ref.CurrentVersion)
	putU32(buf, off+20, ref.CompatVersion)
	copy(buf[off+sizeofDylibCommand:], pathBytes)
	buf[off+sizeofDylibCommand+len(pathBytes)] = 0
	return off + total
}

// emitNlist64 serialises one nlist_64 entry into buf at off.
func emitNlist64(buf []byte, off int, strx uint32, ntype, nsect uint8, ndesc uint16, value uint64) int {
	putU32(buf, off, strx)
	buf[off+4] = ntype
	buf[off+5] = nsect
	binary.LittleEndian.PutUint16(buf[off+6:], ndesc)
	putU64(buf, off+8, value)
	return off + sizeofNlist64
}

// emitRelocEntry serialises one relocation_info into buf at off.
// The r_symbolnum/r_pcrel/r_length/r_extern/r_type fields are packed
// into a single little-endian uint32 per the Mach-O ABI.
func emitRelocEntry(buf []byte, off int, r Reloc, symIdx uint32) int {
	putU32(buf, off, r.Offset)
	packed := symIdx & 0x00ffffff
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