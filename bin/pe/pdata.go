package pe

import "encoding/binary"

// PdataBuilder accumulates RUNTIME_FUNCTION records and their corresponding
// UNWIND_INFO structures, then produces the paired .pdata and .xdata sections.
//
// Usage:
//
//	pb := pe.NewPdataBuilder()
//	// Option A: supply pre-built RuntimeFunction records directly.
//	pb.Add(pe.RuntimeFunction{BeginRVA: 0x1000, EndRVA: 0x1100, UnwindInfoRVA: 0x5000})
//	// Option B: supply a full UnwindInfo and let PdataBuilder manage .xdata layout.
//	rva := pb.AddWithUnwindInfo(0x1100, 0x1200, &pe.UnwindInfo{...})
//	pdataSection, xdataSection := pb.Build(pdataBaseRVA, xdataBaseRVA)
type PdataBuilder struct {
	funcs    []RuntimeFunction
	xdataBuf []byte // accumulated .xdata bytes
}

// NewPdataBuilder returns a new PdataBuilder.
func NewPdataBuilder() *PdataBuilder { return &PdataBuilder{} }

// Add appends a pre-built RUNTIME_FUNCTION. Use this when the linker has already
// assigned all RVAs (including UnwindInfoRVA into an existing .xdata section).
func (pb *PdataBuilder) Add(rf RuntimeFunction) { pb.funcs = append(pb.funcs, rf) }

// AddWithUnwindInfo appends a RUNTIME_FUNCTION and serializes the given UnwindInfo
// into the internal .xdata buffer. It returns the xdata-section-relative offset
// of the new UNWIND_INFO record, which must be added to the .xdata section's base
// RVA by the caller when constructing the final RuntimeFunction if needed.
//
// For convenience, Build() will do this automatically when called.
func (pb *PdataBuilder) AddWithUnwindInfo(beginRVA, endRVA uint32, ui *UnwindInfo) uint32 {
	xdataOff := uint32(len(pb.xdataBuf))
	pb.xdataBuf = append(pb.xdataBuf, marshalUnwindInfo(ui)...)
	pb.xdataBuf = padToAlignment(pb.xdataBuf, 4)
	pb.funcs = append(pb.funcs, RuntimeFunction{
		BeginRVA:      beginRVA,
		EndRVA:        endRVA,
		UnwindInfoRVA: xdataOff, // will be fixed up to xdataBaseRVA+xdataOff in Build()
	})
	return xdataOff
}

// Build returns the raw .pdata and .xdata section bytes.
// pdataBaseRVA and xdataBaseRVA are the virtual addresses of the two sections.
//
// For RUNTIME_FUNCTION records added via Add(), UnwindInfoRVA is used as-is.
// For records added via AddWithUnwindInfo(), the xdata-relative offset stored
// during AddWithUnwindInfo is added to xdataBaseRVA here.
func (pb *PdataBuilder) Build(pdataBaseRVA, xdataBaseRVA uint32) (pdataSection, xdataSection []byte) {
	le := binary.LittleEndian
	pdataSection = make([]byte, len(pb.funcs)*12)
	for i, rf := range pb.funcs {
		le.PutUint32(pdataSection[i*12+0:], rf.BeginRVA)
		le.PutUint32(pdataSection[i*12+4:], rf.EndRVA)
		unwindRVA := rf.UnwindInfoRVA
		if unwindRVA < xdataBaseRVA {
			// Was stored as a section-relative offset; adjust to final RVA.
			unwindRVA += xdataBaseRVA
		}
		le.PutUint32(pdataSection[i*12+8:], unwindRVA)
	}
	xdataSection = padToAlignment(pb.xdataBuf, 4)
	_ = pdataBaseRVA
	return
}

// Funcs returns the accumulated RuntimeFunction slice (UnwindInfoRVAs are
// section-relative for AddWithUnwindInfo entries until Build is called).
func (pb *PdataBuilder) Funcs() []RuntimeFunction { return pb.funcs }

// XdataBlob returns the accumulated .xdata bytes.
func (pb *PdataBuilder) XdataBlob() []byte { return pb.xdataBuf }

// ── Internal helpers ─────────────────────────────────────────────────────────

// buildPdataSection serializes a slice of RUNTIME_FUNCTIONs (already fully resolved).
func buildPdataSection(funcs []RuntimeFunction) []byte {
	le := binary.LittleEndian
	buf := make([]byte, len(funcs)*12)
	for i, rf := range funcs {
		le.PutUint32(buf[i*12+0:], rf.BeginRVA)
		le.PutUint32(buf[i*12+4:], rf.EndRVA)
		le.PutUint32(buf[i*12+8:], rf.UnwindInfoRVA)
	}
	return buf
}

// marshalUnwindInfo serializes an UnwindInfo into its binary representation.
// The result is always DWORD-aligned in length.
//
// Binary layout (per x64 ABI spec):
//
//	byte 0: Version(3 bits) | Flags(5 bits)
//	byte 1: SizeOfProlog
//	byte 2: CountOfCodes
//	byte 3: FrameRegister(4 bits) | FrameOffset(4 bits)
//	[CountOfCodes × 2-byte UNWIND_CODE entries, rounded up to even]
//	[if EHANDLER/UHANDLER: ExceptionHandlerRVA uint32 + HandlerData bytes]
//	[if CHAININFO: RUNTIME_FUNCTION (12 bytes)]
func marshalUnwindInfo(ui *UnwindInfo) []byte {
	// Count the total number of 2-byte UNWIND_CODE slots needed.
	slots := 0
	for _, c := range ui.Codes {
		slots += unwindCodeSlots(c)
	}
	// Round up to even.
	codeSlots := slots
	if codeSlots&1 != 0 {
		codeSlots++
	}

	size := 4 + codeSlots*2
	if ui.Flags&UNW_FLAG_CHAININFO != 0 {
		size += 12 // chained RUNTIME_FUNCTION
	} else if ui.Flags&(UNW_FLAG_EHANDLER|UNW_FLAG_UHANDLER) != 0 {
		size += 4 + len(ui.HandlerData) // ExceptionHandlerRVA + handler data
	}
	size = int(align32(uint32(size), 4))

	buf := make([]byte, size)
	le := binary.LittleEndian

	buf[0] = (1 & 0x7) | ((ui.Flags & 0x1F) << 3) // Version=1, Flags
	buf[1] = ui.SizeOfProlog
	buf[2] = uint8(slots)
	buf[3] = (ui.FrameRegister & 0xF) | ((ui.FrameOffset & 0xF) << 4)

	off := 4
	for _, c := range ui.Codes {
		buf[off] = c.PrologOffset
		buf[off+1] = (c.Op & 0xF) | ((c.OpInfo & 0xF) << 4)
		off += 2
		n := unwindCodeSlots(c)
		if n >= 2 {
			le.PutUint16(buf[off:], c.Extra)
			off += 2
		}
		if n >= 3 {
			le.PutUint16(buf[off:], c.Extra2)
			off += 2
		}
	}
	off = 4 + codeSlots*2 // skip to after codes (including padding slot)

	if ui.Flags&UNW_FLAG_CHAININFO != 0 && ui.Chained != nil {
		le.PutUint32(buf[off+0:], ui.Chained.BeginRVA)
		le.PutUint32(buf[off+4:], ui.Chained.EndRVA)
		le.PutUint32(buf[off+8:], ui.Chained.UnwindInfoRVA)
	} else if ui.Flags&(UNW_FLAG_EHANDLER|UNW_FLAG_UHANDLER) != 0 {
		le.PutUint32(buf[off:], ui.ExceptionHandlerRVA)
		off += 4
		copy(buf[off:], ui.HandlerData)
	}

	return buf
}

// unwindCodeSlots returns the number of 2-byte UNWIND_CODE slots an UnwindCode occupies.
func unwindCodeSlots(c UnwindCode) int {
	switch c.Op {
	case UWOP_ALLOC_LARGE:
		if c.OpInfo == 0 {
			return 2 // 1 extra slot holding size/8
		}
		return 3 // 2 extra slots holding 32-bit size
	case UWOP_SAVE_NONVOL, UWOP_SAVE_XMM128, UWOP_EPILOG:
		return 2
	case UWOP_SAVE_NONVOL_FAR, UWOP_SAVE_XMM128_FAR:
		return 3
	default:
		return 1
	}
}