// Package linker
package linker

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/vertex-language/compiler/linker/output"
	"github.com/vertex-language/compiler/object"
)

type Format int

const (
	ELF Format = iota
)

type Options struct {
	Output Format
	Entry  string
	Libs   []string
}

func Link(objs []*object.WasmObj, opts Options) ([]byte, error) {
	if len(objs) == 0 {
		return nil, fmt.Errorf("linker: no input objects")
	}
	if opts.Entry == "" {
		opts.Entry = "_start"
	}

	lnk := &linker{opts: opts, sym: newSymTable(), gotTable: newGOT()}

	if err := lnk.ingest(objs); err != nil {
		return nil, err
	}

	for _, libPath := range opts.Libs {
		data, err := os.ReadFile(libPath)
		if err != nil {
			return nil, fmt.Errorf("linker: reading %s: %w", libPath, err)
		}
		if err := lnk.ingestArchive(data, libPath); err != nil {
			return nil, fmt.Errorf("linker: archive %s: %w", libPath, err)
		}
	}

	totalTData := len(lnk.tdata)
	for _, p := range lnk.pendingTBSS {
		globalOff := totalTData + p.tbssBase + p.offset
		_ = lnk.sym.defineTLS(p.name, globalOff)
	}

	// Fallback logic if the requested entry isn't explicitly defined
	targetEntry := opts.Entry
	if _, ok := lnk.sym.defs[targetEntry]; !ok {
		if _, ok := lnk.sym.defs["main"]; ok {
			targetEntry = "main"
		}
	}

	// ── INJECT WASM SYNTHETIC SYMBOLS ─────────────────────────────────────────
	// Provide standard WebAssembly synthetic symbols if the CPU backend 
	// generated relocations against them.
	if lnk.sym.refs["__wasm_data_base"] {
		_ = lnk.sym.define("__wasm_data_base", object.SymSecData, 0)
	}
	if lnk.sym.refs["__wasm_memory_base"] {
		_ = lnk.sym.define("__wasm_memory_base", object.SymSecBSS, 0)
	}
	// ──────────────────────────────────────────────────────────────────────────

	// INJECT ELF ENTRY STUB
	// Forcefully align the stack to 16 bytes, call the Wasm entry point,
	// and cleanly terminate the process using SYS_exit_group with exit code 0.
	stubOffset := len(lnk.code)
	lnk.code = append(lnk.code,
		0x48, 0x83, 0xE4, 0xF0,       // and rsp, -16 (align stack)
		0xE8, 0x00, 0x00, 0x00, 0x00, // call targetEntry
		0x31, 0xFF,                   // xor edi, edi (exit code 0)
		0xB8, 0xE7, 0x00, 0x00, 0x00, // mov eax, 231 (SYS_exit_group)
		0x0F, 0x05,                   // syscall
	)
	
	_ = lnk.sym.define("__wasm_exit_stub", object.SymSecText, stubOffset)
	lnk.relocs = append(lnk.relocs, relocSite{
		section: secKindText,
		codeOff: stubOffset + 5, // Offset shifted by 4 bytes because of 'and rsp, -16'
		symbol:  targetEntry,
		kind:    object.RelocRel32,
	})
	opts.Entry = "__wasm_exit_stub" // Override the ELF entry point

	if len(lnk.sym.refs) > 0 {
		lnk.ds = lnk.buildDynamic()
	} else {
		if err := lnk.checkResolved(); err != nil {
			return nil, err
		}
	}

	if err := lnk.buildGOT(); err != nil {
		return nil, err
	}
	if err := lnk.layoutTLS(); err != nil {
		return nil, err
	}

	var dynSizes output.DynamicSizes
	if lnk.ds != nil {
		dynSizes = output.DynamicSizes{
			InterpSize:  uint64(len(InterpPath)),
			DynStrSize:  uint64(len(lnk.ds.dynStr)),
			DynSymSize:  uint64(len(lnk.ds.dynSym)),
			RelaPltSize: uint64(len(lnk.ds.relaPlt)),
			PltSize:     uint64(len(lnk.ds.plt)),
			DynamicSize: uint64(len(lnk.ds.dynamic)),
			GotPltSize:  uint64(len(lnk.ds.gotPlt)),
		}
	}

	lnk.lay = output.ComputeLayout(
		uint64(len(lnk.code)), uint64(len(lnk.rodata)),
		uint64(len(lnk.tdata)), lnk.tbss,
		uint64(len(lnk.data)), lnk.bss,
		lnk.gotTable.Size(), dynSizes,
	)

	if lnk.ds != nil {
		lnk.patchDynamicVAs()
	}

	if err := lnk.fillGOT(); err != nil {
		return nil, err
	}

	lnk.relax()
	if err := lnk.relocate(); err != nil {
		return nil, err
	}

	switch opts.Output {
	case ELF:
		entryOff, ok := lnk.sym.offset(opts.Entry)
		if !ok {
			return nil, fmt.Errorf("linker: entry symbol %q not found", opts.Entry)
		}
		
		params := output.BuildParams{
			Lay:      lnk.lay,
			Text:     lnk.code,
			ROData:   lnk.rodata,
			TData:    lnk.tdata,
			TBSSSize: lnk.tbss,
			Data:     lnk.data,
			BSS:      lnk.bss,
			GOT:      lnk.gotTable.Content(),
			Syms:     lnk.sym.defined(),
			EntryVA:  lnk.lay.TextVA + uint64(entryOff),
		}

		if lnk.ds != nil {
			params.Interp = []byte(InterpPath)
			params.DynStr = lnk.ds.dynStr
			params.DynSym = lnk.ds.dynSym 
			params.RelaPlt = lnk.ds.relaPlt
			params.Plt = lnk.ds.plt
			params.Dynamic = lnk.ds.dynamic
			params.GotPlt = lnk.ds.gotPlt
		}

		return output.BuildELF64(params), nil
	default:
		return nil, fmt.Errorf("linker: unsupported output format %d", opts.Output)
	}
}

type linker struct {
	opts Options
	code, rodata, data, tdata []byte
	bss, tbss uint64
	sym         symTable
	relocs      []relocSite
	gotTable    *got
	lay         output.ELFLayout
	pendingTBSS []pendingTBSSym
	ds          *dynamicState 
}

type relocSite struct {
	section sectionKind
	codeOff int
	symbol  string
	kind    object.RelocKind
}

type pendingTBSSym struct {
	name     string
	tbssBase int
	offset   int
}

func (lnk *linker) resolveSymbolVA(name string) (uint64, bool) {
	if lnk.ds != nil {
		for i, dynName := range lnk.ds.dynSyms {
			if name == dynName {
				return lnk.lay.PltVA + 16 + uint64(i*16), true
			}
		}
	}
	d, ok := lnk.sym.defs[name]
	if !ok {
		return 0, false
	}
	return lnk.lay.SymbolVA(d.section, d.off), true
}

func (lnk *linker) patchDynamicVAs() {
	le := binary.LittleEndian

	for i := 0; i < len(lnk.ds.dynamic); i += 16 {
		tag := le.Uint64(lnk.ds.dynamic[i:])
		switch tag {
		case 5: le.PutUint64(lnk.ds.dynamic[i+8:], lnk.lay.DynStrVA)
		case 6: le.PutUint64(lnk.ds.dynamic[i+8:], lnk.lay.DynSymVA)
		case 23: le.PutUint64(lnk.ds.dynamic[i+8:], lnk.lay.RelaPltVA)
		case 3: le.PutUint64(lnk.ds.dynamic[i+8:], lnk.lay.GotPltVA)
		}
	}

	le.PutUint64(lnk.ds.gotPlt[0:], lnk.lay.DynamicVA)

	for i := range lnk.ds.dynSyms {
		gotSlotIdx := i + 3
		gotSlotVA := lnk.lay.GotPltVA + uint64(gotSlotIdx*8)
		
		pltStubVA := lnk.lay.PltVA + 16 + uint64(i*16)
		pushVA := pltStubVA + 6

		le.PutUint64(lnk.ds.gotPlt[gotSlotIdx*8:], pushVA)

		relaBase := i * 24
		le.PutUint64(lnk.ds.relaPlt[relaBase:], gotSlotVA)
		rInfo := (uint64(i+1) << 32) | 7 
		le.PutUint64(lnk.ds.relaPlt[relaBase+8:], rInfo)
		le.PutUint64(lnk.ds.relaPlt[relaBase+16:], 0) 

		disp32 := int64(gotSlotVA) - int64(pltStubVA+6)
		le.PutUint32(lnk.ds.plt[16+i*16+2:], uint32(int32(disp32)))

		dispPlt0 := int64(lnk.lay.PltVA) - int64(pltStubVA+16)
		le.PutUint32(lnk.ds.plt[16+i*16+12:], uint32(int32(dispPlt0)))
	}

	dispGot8 := int64(lnk.lay.GotPltVA+8) - int64(lnk.lay.PltVA+6)
	le.PutUint32(lnk.ds.plt[2:], uint32(int32(dispGot8)))

	dispGot16 := int64(lnk.lay.GotPltVA+16) - int64(lnk.lay.PltVA+12)
	le.PutUint32(lnk.ds.plt[8:], uint32(int32(dispGot16)))
}

func (lnk *linker) ingest(objs []*object.WasmObj) error {
	for i, obj := range objs {
		textBase   := len(lnk.code)
		rodataBase := len(lnk.rodata)
		dataBase   := len(lnk.data)
		tdataBase  := len(lnk.tdata)
		tbssBase   := int(lnk.tbss)

		lnk.code   = append(lnk.code,   obj.Code...)
		lnk.rodata = append(lnk.rodata, obj.ROData...)
		lnk.data   = append(lnk.data,   obj.Data...)
		lnk.bss   += obj.BSS
		lnk.tdata  = append(lnk.tdata,  obj.TLSData...)
		lnk.tbss  += obj.TLSBSSSize

		for _, s := range obj.Symbols {
			switch s.Kind {
			case object.SymDefined:
				var base int
				switch s.Section {
				case object.SymSecText:   base = textBase
				case object.SymSecROData: base = rodataBase
				case object.SymSecData, object.SymSecBSS: base = dataBase
				case object.SymSecTData:  base = tdataBase
				case object.SymSecTBSS:  base = 0 
				}
				if s.Section == object.SymSecTBSS {
					lnk.pendingTBSS = append(lnk.pendingTBSS, pendingTBSSym{
						name:     s.Name,
						tbssBase: tbssBase,
						offset:   s.Offset,
					})
				} else {
					if err := lnk.sym.define(s.Name, s.Section, base+s.Offset); err != nil {
						return fmt.Errorf("linker: object %d: %w", i, err)
					}
				}
			case object.SymTLS:
				if s.Section == object.SymSecTData {
					if err := lnk.sym.defineTLS(s.Name, tdataBase+s.Offset); err != nil {
						return fmt.Errorf("linker: object %d TLS: %w", i, err)
					}
				} else { 
					lnk.pendingTBSS = append(lnk.pendingTBSS, pendingTBSSym{
						name:     s.Name,
						tbssBase: tbssBase,
						offset:   s.Offset,
					})
				}
			case object.SymUndefined:
				lnk.sym.reference(s.Name)
			}
		}

		for _, r := range obj.Relocs {
			var (
				sec     sectionKind
				baseOff int
			)
			switch r.Section {
			case object.RelocSecText:
				sec, baseOff = secKindText, textBase
			case object.RelocSecROData:
				sec, baseOff = secKindROData, rodataBase
			case object.RelocSecData:
				sec, baseOff = secKindData, dataBase
			}
			lnk.relocs = append(lnk.relocs, relocSite{
				section: sec,
				codeOff: baseOff + r.Offset,
				symbol:  r.Symbol,
				kind:    r.Kind,
			})
		}
	}
	return nil
}

func (lnk *linker) ingestArchive(data []byte, path string) error {
	symIdx, err := ArchiveSymbolIndex(data)
	if err != nil {
		return fmt.Errorf("reading symbol index: %w", err)
	}

	ingested := make(map[uint32]bool)

	for {
		needed := make(map[uint32]bool)
		for name := range lnk.sym.refs {
			if off, ok := symIdx[name]; ok && !ingested[off] {
				needed[off] = true
			}
		}
		if len(needed) == 0 {
			break
		}
		for off := range needed {
			member, err := readMemberAt(data, off)
			if err != nil {
				return fmt.Errorf("member at offset %d in %s: %w", off, path, err)
			}
			obj, err := parseELF64Obj(member.Data)
			if err != nil {
				return fmt.Errorf("parsing %s(%s): %w", path, member.Name, err)
			}
			if err := lnk.ingest([]*object.WasmObj{obj}); err != nil {
				return fmt.Errorf("ingesting %s(%s): %w", path, member.Name, err)
			}
			ingested[off] = true
		}
	}
	return nil
}