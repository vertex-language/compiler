// Package linker — dynamic.go
package linker

import (
	"encoding/binary"
)

const (
	dtNull     = 0
	dtNeeded   = 1
	dtPltRelSz = 2
	dtPltGot   = 3
	dtHash     = 4
	dtStrTab   = 5
	dtSymTab   = 6
	dtRela     = 7
	dtRelaSz   = 8
	dtRelaEnt  = 9
	dtStrSz    = 10
	dtSymEnt   = 11
	dtInit     = 12
	dtFini     = 13
	dtRel      = 17
	dtPltRel   = 20
	dtJmpRel   = 23
)

const InterpPath = "/lib64/ld-linux-x86-64.so.2\x00"
const LibcName = "libc.so.6\x00"

type dynamicState struct {
	dynSyms []string
	plt     []byte
	gotPlt  []byte
	relaPlt []byte
	dynStr  []byte
	dynSym  []byte // <-- NEW: Actually hold the dynamic symbols!
	dynamic []byte
}

func (lnk *linker) buildDynamic() *dynamicState {
	ds := &dynamicState{}

	for name := range lnk.sym.refs {
		ds.dynSyms = append(ds.dynSyms, name)
	}

	ds.dynStr = []byte{0}
	libcStrOff := len(ds.dynStr)
	ds.dynStr = append(ds.dynStr, LibcName...)
	
	// 1. Build .dynsym (Entry 0 is always NULL)
	ds.dynSym = make([]byte, 24)

	// Add symbol names and build .dynsym entries
	for _, sym := range ds.dynSyms {
		nameOff := len(ds.dynStr)
		ds.dynStr = append(ds.dynStr, sym...)
		ds.dynStr = append(ds.dynStr, 0)

		// Elf64_Sym: st_name(4), st_info(1), st_other(1), st_shndx(2), st_value(8), st_size(8)
		symBuf := make([]byte, 24)
		binary.LittleEndian.PutUint32(symBuf[0:], uint32(nameOff))
		symBuf[4] = (1 << 4) | 2 // STB_GLOBAL | STT_FUNC
		ds.dynSym = append(ds.dynSym, symBuf...)
	}

	// 2. Build PLT and GOT.PLT
	ds.gotPlt = make([]byte, 24) 
	
	ds.plt = []byte{
		0xff, 0x35, 0x08, 0x00, 0x00, 0x00, // pushq GOT+8(%rip)
		0xff, 0x25, 0x10, 0x00, 0x00, 0x00, // jmpq *GOT+16(%rip)
		0x0f, 0x1f, 0x40, 0x00,             // nopl 0x0(%rax)
	}

	for i := range ds.dynSyms {
		ds.gotPlt = append(ds.gotPlt, 0, 0, 0, 0, 0, 0, 0, 0)
		ds.plt = append(ds.plt, 
			0xff, 0x25, 0x00, 0x00, 0x00, 0x00, 
			0x68, byte(i), byte(i>>8), byte(i>>16), byte(i>>24), 
			0xe9, 0x00, 0x00, 0x00, 0x00,       
		)
		ds.relaPlt = append(ds.relaPlt, make([]byte, 24)...)
	}

	// 3. Build .dynamic section
	addTag := func(tag uint64, val uint64) {
		buf := make([]byte, 16)
		binary.LittleEndian.PutUint64(buf[0:], tag)
		binary.LittleEndian.PutUint64(buf[8:], val)
		ds.dynamic = append(ds.dynamic, buf...)
	}

	addTag(dtNeeded, uint64(libcStrOff))
	addTag(dtStrSz, uint64(len(ds.dynStr)))
	addTag(dtSymEnt, 24) 
	addTag(dtRelaEnt, 24) // <-- NEW: Safety tag for the loader
	addTag(dtPltRelSz, uint64(len(ds.relaPlt)))
	addTag(dtPltRel, 7)  // DT_RELA
	
	addTag(dtStrTab, 0) 
	addTag(dtSymTab, 0)
	addTag(dtJmpRel, 0)
	addTag(dtPltGot, 0)
	addTag(dtNull, 0) 

	return ds
}