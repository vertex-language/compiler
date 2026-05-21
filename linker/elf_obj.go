// Package linker — elf_obj.go
// Parses an ELF64 ET_REL relocatable object (.o) file and returns a
// *object.WasmObj so the linker can process it via the normal ingest path.
//
// Supported:
//
//	Sections:    .text .rodata .data .bss .tdata .tbss
//	Symbols:     STB_GLOBAL / STB_WEAK in any of the above sections
//	Relocations: R_X86_64_64 (1), PC32 (2), PLT32 (4), 32S (11),
//	             GOTPCREL (9), GOTTPOFF (22), TPOFF32 (23),
//	             GOTPCRELX (41), REX_GOTPCRELX (42)
//	Reloc sections: .rela.text .rela.rodata .rela.data
package linker

import (
	"encoding/binary"
	"fmt"

	"github.com/vertex-language/compiler/object"
)

// ── ELF64 constants ───────────────────────────────────────────────────────────

const (
	elfMagic    = "\x7FELF"
	elfClass64  = byte(2)
	elfData2LSB = byte(1)
	etRel       = uint16(1)

	shtProgbitsE = uint32(1)
	shtNobitsE   = uint32(8)
	shtSymtabE   = uint32(2)
	shtStrtabE   = uint32(3)
	shtRelaE     = uint32(4)

	shfWriteE     = uint64(0x1)
	shfAllocE     = uint64(0x2)
	shfExecInstrE = uint64(0x4)
	shfTLSE       = uint64(0x400)

	stbGlobalE = byte(1)
	stbWeakE   = byte(2)
	sttTLSE    = byte(6) // STT_TLS

	shnUndefE = uint16(0)
	shnAbsE   = uint16(0xfff1)

	// x86-64 RELA relocation types (AMD64 psABI table 4.10)
	rX64_64            = uint32(1)
	rX64_PC32          = uint32(2)
	rX64_PLT32         = uint32(4)
	rX64_GOTPCREL      = uint32(9)
	rX64_32S           = uint32(11)
	rX64_GOTTPOFF      = uint32(22)
	rX64_TPOFF32       = uint32(23)
	rX64_GOTPCRELX     = uint32(41)
	rX64_REX_GOTPCRELX = uint32(42)

	elf64ShdrSz = 64
	elf64SymSz  = 24
	elf64RelaSz = 24
)

// elfShdr is the decoded subset of Elf64_Shdr we care about.
//
// Elf64_Shdr layout (64 bytes):
//
//	0  sh_name      u32     16 sh_addr      u64
//	4  sh_type      u32     24 sh_offset    u64
//	8  sh_flags     u64     32 sh_size      u64
//	                        40 sh_link      u32
//	                        44 sh_info      u32
//	                        48 sh_addralign u64
//	                        56 sh_entsize   u64
type elfShdr struct {
	name    uint32
	typ     uint32
	flags   uint64
	off     uint64
	size    uint64
	link    uint32
	info    uint32
	entsize uint64
}

// parseELF64Obj parses one ELF64 ET_REL object and returns a *object.WasmObj.
func parseELF64Obj(data []byte) (*object.WasmObj, error) {
	le := binary.LittleEndian

	// ── Validate ELF header ───────────────────────────────────────────────
	if len(data) < 64 {
		return nil, fmt.Errorf("elf obj: too short (%d bytes)", len(data))
	}
	if string(data[0:4]) != elfMagic {
		return nil, fmt.Errorf("elf obj: bad magic")
	}
	if data[4] != elfClass64 {
		return nil, fmt.Errorf("elf obj: not ELF64 (class=%d)", data[4])
	}
	if data[5] != elfData2LSB {
		return nil, fmt.Errorf("elf obj: not little-endian (data=%d)", data[5])
	}
	if le.Uint16(data[16:]) != etRel {
		return nil, fmt.Errorf("elf obj: not ET_REL (e_type=%d)", le.Uint16(data[16:]))
	}

	shoff := le.Uint64(data[40:])
	shentsz := uint64(le.Uint16(data[58:]))
	shnum := int(le.Uint16(data[60:]))
	shstrndx := int(le.Uint16(data[62:]))

	if shentsz < elf64ShdrSz {
		return nil, fmt.Errorf("elf obj: e_shentsize %d < 64", shentsz)
	}
	if shoff+uint64(shnum)*shentsz > uint64(len(data)) {
		return nil, fmt.Errorf("elf obj: section header table out of bounds")
	}

	// ── Decode section header table ───────────────────────────────────────
	shdrs := make([]elfShdr, shnum)
	for i := 0; i < shnum; i++ {
		b := data[shoff+uint64(i)*shentsz:]
		shdrs[i] = elfShdr{
			name:    le.Uint32(b[0:]),
			typ:     le.Uint32(b[4:]),
			flags:   le.Uint64(b[8:]),
			off:     le.Uint64(b[24:]),
			size:    le.Uint64(b[32:]),
			link:    le.Uint32(b[40:]),
			info:    le.Uint32(b[44:]),
			entsize: le.Uint64(b[56:]),
		}
	}

	shstrtab, err := elfSecBytes(data, shdrs, shstrndx)
	if err != nil {
		return nil, fmt.Errorf("elf obj: shstrtab: %w", err)
	}

	// ── Identify sections by type + flags + name ──────────────────────────
	textIdx := -1
	rodataIdx := -1
	dataIdx := -1
	bssIdx := -1
	tdataIdx := -1
	tbssIdx := -1
	symtabIdx := -1
	// rela sections: map from target section index → rela section index
	relaFor := make(map[int]int)

	for i, sh := range shdrs {
		sname := elfStrAt(shstrtab, sh.name)
		switch {
		case sh.typ == shtProgbitsE && sh.flags&shfExecInstrE != 0:
			textIdx = i
		case sh.typ == shtProgbitsE && sh.flags&shfTLSE != 0:
			tdataIdx = i
		case sh.typ == shtNobitsE && sh.flags&shfTLSE != 0:
			tbssIdx = i
		case sh.typ == shtProgbitsE && sh.flags&shfWriteE != 0 && sh.flags&shfAllocE != 0:
			dataIdx = i
		case sh.typ == shtNobitsE && sh.flags&shfAllocE != 0 && sh.flags&shfTLSE == 0:
			bssIdx = i
		case sh.typ == shtProgbitsE && sh.flags == shfAllocE && sname == ".rodata":
			rodataIdx = i
		case sh.typ == shtProgbitsE && sh.flags == shfAllocE:
			// Catch-all for read-only sections (.rodata.*, .eh_frame, etc.)
			if rodataIdx < 0 {
				rodataIdx = i
			}
		case sh.typ == shtSymtabE:
			symtabIdx = i
		case sh.typ == shtRelaE:
			// sh_info = index of the section this rela applies to
			relaFor[int(sh.info)] = i
		}
	}

	// ── Extract section data ──────────────────────────────────────────────
	textBytes, err := elfSecBytesOpt(data, shdrs, textIdx)
	if err != nil {
		return nil, fmt.Errorf("elf obj: .text: %w", err)
	}
	rodataBytes, err := elfSecBytesOpt(data, shdrs, rodataIdx)
	if err != nil {
		return nil, fmt.Errorf("elf obj: .rodata: %w", err)
	}
	dataBytes, err := elfSecBytesOpt(data, shdrs, dataIdx)
	if err != nil {
		return nil, fmt.Errorf("elf obj: .data: %w", err)
	}
	tdataBytes, err := elfSecBytesOpt(data, shdrs, tdataIdx)
	if err != nil {
		return nil, fmt.Errorf("elf obj: .tdata: %w", err)
	}

	var bssSize uint64
	var tbssSize uint64
	if bssIdx >= 0 {
		bssSize = shdrs[bssIdx].size
	}
	if tbssIdx >= 0 {
		tbssSize = shdrs[tbssIdx].size
	}

	// tdataSize is used to compute the global TLS template offset for .tbss syms.
	tdataSize := uint64(len(tdataBytes))

	// ── Parse .symtab ─────────────────────────────────────────────────────
	// elfSymName[i] is the name of ELF symbol i (used when resolving relocs).
	var elfSymName []string
	var symbols []object.Symbol

	if symtabIdx >= 0 {
		sh := shdrs[symtabIdx]
		entSz := sh.entsize
		if entSz == 0 {
			entSz = elf64SymSz
		}
		symRaw, err := elfSecBytes(data, shdrs, symtabIdx)
		if err != nil {
			return nil, fmt.Errorf("elf obj: .symtab: %w", err)
		}
		strtabRaw, err := elfSecBytes(data, shdrs, int(sh.link))
		if err != nil {
			return nil, fmt.Errorf("elf obj: .strtab: %w", err)
		}

		nSym := int(sh.size / entSz)
		elfSymName = make([]string, nSym)

		for i := 0; i < nSym; i++ {
			base := i * int(entSz)
			if base+elf64SymSz > len(symRaw) {
				break
			}
			s := symRaw[base:]

			// Elf64_Sym layout (24 bytes):
			//  0  st_name  u32    8  st_value u64
			//  4  st_info  u8    16  st_size  u64
			//  5  st_other u8
			//  6  st_shndx u16
			nameOff := le.Uint32(s[0:])
			stInfo := s[4]
			shndx := le.Uint16(s[6:])
			value := le.Uint64(s[8:])

			bind := stInfo >> 4
			styp := stInfo & 0xf
			name := elfStrAt(strtabRaw, nameOff)
			elfSymName[i] = name

			if i == 0 || name == "" {
				continue // null entry or unnamed
			}
			if bind != stbGlobalE && bind != stbWeakE {
				continue // not visible outside this object
			}
			if shndx == shnUndefE {
				symbols = append(symbols, object.Symbol{Name: name, Kind: object.SymUndefined})
				continue
			}
			if shndx == shnAbsE {
				continue // absolute symbol; skip
			}

			isTLS := styp == sttTLSE || int(shndx) == tdataIdx || int(shndx) == tbssIdx

			switch {
			case int(shndx) == textIdx:
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymDefined,
					Section: object.SymSecText, Offset: int(value),
				})
			case int(shndx) == rodataIdx:
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymDefined,
					Section: object.SymSecROData, Offset: int(value),
				})
			case int(shndx) == dataIdx:
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymDefined,
					Section: object.SymSecData, Offset: int(value),
				})
			case int(shndx) == bssIdx:
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymDefined,
					Section: object.SymSecBSS, Offset: int(value),
				})
			case isTLS && int(shndx) == tdataIdx:
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymTLS,
					Section: object.SymSecTData, Offset: int(value),
				})
			case isTLS && int(shndx) == tbssIdx:
				// Offset within .tbss; ingest() adds tdataSize to get the
				// global TLS-template offset (see linker.go pendingTBSS).
				symbols = append(symbols, object.Symbol{
					Name: name, Kind: object.SymTLS,
					Section: object.SymSecTBSS, Offset: int(value),
				})
			}
			_ = tdataSize // used above for the tbss comment
		}
	}

	// ── Parse RELA sections ───────────────────────────────────────────────
	// We handle .rela.text, .rela.rodata, and .rela.data.
	var relocs []object.Reloc

	relaTargets := map[int]object.RelocSection{
		textIdx:   object.RelocSecText,
		rodataIdx: object.RelocSecROData,
		dataIdx:   object.RelocSecData,
	}

	for targetIdx, relocSec := range relaTargets {
		if targetIdx < 0 {
			continue
		}
		relaIdx, ok := relaFor[targetIdx]
		if !ok {
			continue
		}
		sh := shdrs[relaIdx]
		entSz := sh.entsize
		if entSz == 0 {
			entSz = elf64RelaSz
		}
		relaRaw, err := elfSecBytes(data, shdrs, relaIdx)
		if err != nil {
			return nil, fmt.Errorf("elf obj: rela section %d: %w", relaIdx, err)
		}

		nRela := int(sh.size / entSz)
		for i := 0; i < nRela; i++ {
			base := i * int(entSz)
			if base+elf64RelaSz > len(relaRaw) {
				break
			}
			r := relaRaw[base:]

			// Elf64_Rela (24 bytes):
			//  0 r_offset u64   8 r_info u64   16 r_addend i64
			rOffset := le.Uint64(r[0:])
			rInfo := le.Uint64(r[8:])
			rAddend := int64(le.Uint64(r[16:]))

			symIdx := uint32(rInfo >> 32)
			relType := uint32(rInfo & 0xffffffff)

			if int(symIdx) >= len(elfSymName) {
				return nil, fmt.Errorf("elf obj: rela[%d]: sym index %d out of range", i, symIdx)
			}
			symName := elfSymName[symIdx]
			if symName == "" {
				// Section symbol or local — skip; typically used for .rodata refs
				// that the linker handles through the section itself.
				continue
			}

			var kind object.RelocKind
			var addend int64
			switch relType {
			case rX64_PC32, rX64_PLT32:
				// Addend is almost always −4, but glibc uses −5 in a handful of
				// internal sites (e.g. __printf_modifier_table).  Accept any value.
				kind = object.RelocRel32
				addend = rAddend

			case rX64_64:
				// Addend encodes a byte offset into the target symbol's storage
				// (e.g. __io_vtables+336).  Accept any value; relocate() adds it
				// to the symbol VA before writing the 8-byte field.
				kind = object.RelocAbs64
				addend = rAddend

			case rX64_32S:
				// Same reasoning as R_X86_64_64; the addend may be non-zero for
				// intra-object references to data symbols.
				kind = object.RelocAbs32S
				addend = rAddend

			case rX64_GOTPCREL, rX64_GOTPCRELX, rX64_REX_GOTPCRELX:
				if rAddend != -4 {
					continue // non-standard addend; cannot relax or patch safely
				}
				kind = object.RelocGOTPCRel32

			case rX64_GOTTPOFF:
				if rAddend != -4 {
					return nil, fmt.Errorf("elf obj: rela[%d] %s GOTTPOFF: addend %d (want -4)",
						i, symName, rAddend)
				}
				kind = object.RelocTLSGOTPCRel32

			case rX64_TPOFF32:
				kind = object.RelocTPOFF32

			default:
				return nil, fmt.Errorf("elf obj: rela[%d] %s: unsupported reloc type %d",
					i, symName, relType)
			}

			relocs = append(relocs, object.Reloc{
				Section: relocSec,
				Offset:  int(rOffset),
				Symbol:  symName,
				Kind:    kind,
				Addend:  addend,
			})
		}
	}

	code := make([]byte, len(textBytes))
	copy(code, textBytes)
	rodata := make([]byte, len(rodataBytes))
	copy(rodata, rodataBytes)
	ddata := make([]byte, len(dataBytes))
	copy(ddata, dataBytes)
	tdata := make([]byte, len(tdataBytes))
	copy(tdata, tdataBytes)

	return &object.WasmObj{
		Code:       code,
		ROData:     rodata,
		Data:       ddata,
		BSS:        bssSize,
		TLSData:    tdata,
		TLSBSSSize: tbssSize,
		Symbols:    symbols,
		Relocs:     relocs,
	}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// elfSecBytes returns the raw content of section i (must have file bytes).
func elfSecBytes(data []byte, shdrs []elfShdr, i int) ([]byte, error) {
	if i < 0 || i >= len(shdrs) {
		return nil, fmt.Errorf("section index %d out of range", i)
	}
	sh := shdrs[i]
	end := sh.off + sh.size
	if end > uint64(len(data)) {
		return nil, fmt.Errorf("section %d [%#x, %#x) out of file bounds", i, sh.off, end)
	}
	return data[sh.off:end], nil
}

// elfSecBytesOpt is like elfSecBytes but returns nil (no error) if idx < 0.
func elfSecBytesOpt(data []byte, shdrs []elfShdr, idx int) ([]byte, error) {
	if idx < 0 {
		return nil, nil
	}
	return elfSecBytes(data, shdrs, idx)
}

// elfStrAt reads a null-terminated string from a string table at byte off.
func elfStrAt(strtab []byte, off uint32) string {
	start := int(off)
	if start >= len(strtab) {
		return ""
	}
	end := start
	for end < len(strtab) && strtab[end] != 0 {
		end++
	}
	return string(strtab[start:end])
}
