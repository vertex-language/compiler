package pe

import "encoding/binary"

// TLSBuilder constructs the .tls section and IMAGE_TLS_DIRECTORY64 for a PE image.
//
// Usage:
//
//	tb := pe.NewTLSBuilder()
//	tb.SetTemplate(myTLSInitData)
//	tb.AddCallback(tlsCallbackRVA1)
//	section, dir := tb.Build(tlsSectionVA, imageBase)
type TLSBuilder struct {
	template  []byte
	zeroFill  uint32
	callbacks []uint32 // RVAs of TLS callback functions
}

// NewTLSBuilder returns a new TLSBuilder.
func NewTLSBuilder() *TLSBuilder { return &TLSBuilder{} }

// SetTemplate sets the TLS template data (the initialized part of thread-local storage).
func (tb *TLSBuilder) SetTemplate(data []byte) { tb.template = data }

// SetZeroFill sets the number of additional zero-filled bytes appended after the
// template (for uninitialized TLS variables).
func (tb *TLSBuilder) SetZeroFill(n uint32) { tb.zeroFill = n }

// AddCallback appends a TLS callback function RVA. Callbacks are invoked (as VAs)
// on thread attach and detach. They are stored in the .tls section as an array of VAs
// terminated by a null entry; base relocations are required for each VA.
func (tb *TLSBuilder) AddCallback(rva uint32) { tb.callbacks = append(tb.callbacks, rva) }

// Build produces the raw .tls section bytes and a populated TLSDirectory.
// sectionVA is the virtual address of the .tls section.
// imageBase is used to convert RVAs to VAs for the directory fields.
//
// Section layout:
//
//	[0]          IMAGE_TLS_DIRECTORY64  (40 bytes)
//	[40]         TLS template data      (len(template) bytes)
//	[40+tmpl]    TLS index DWORD        (4 bytes; set to 0, filled by loader)
//	[44+tmpl]    padding to 8-byte boundary
//	[48+tmpl...] TLS callback array     ((len(callbacks)+1) × 8 bytes)
func (tb *TLSBuilder) Build(sectionVA uint32, imageBase uint64) (TLSDirectory, []byte) {
	le := binary.LittleEndian

	templateLen := uint32(len(tb.template))
	// Template start and end within section (directory is at offset 0).
	// We place template data right after the 40-byte directory.
	tmplOff    := uint32(40)
	indexOff   := align32(tmplOff+templateLen, 4)
	callbackOff := align32(indexOff+4, 8)
	totalSize  := callbackOff + uint32(len(tb.callbacks)+1)*8

	buf := make([]byte, totalSize)

	startVA    := imageBase + uint64(sectionVA+tmplOff)
	endVA      := startVA + uint64(templateLen)
	indexVA    := imageBase + uint64(sectionVA+indexOff)
	callbackVA := imageBase + uint64(sectionVA+callbackOff)
	if len(tb.callbacks) == 0 {
		callbackVA = 0
	}

	// IMAGE_TLS_DIRECTORY64 (40 bytes at offset 0).
	le.PutUint64(buf[0:], startVA)
	le.PutUint64(buf[8:], endVA)
	le.PutUint64(buf[16:], indexVA)
	le.PutUint64(buf[24:], callbackVA)
	le.PutUint32(buf[32:], tb.zeroFill)
	le.PutUint32(buf[36:], 0) // Characteristics: reserved

	// Template data.
	copy(buf[tmplOff:], tb.template)

	// TLS index (4 bytes at indexOff; zero-initialized, filled by the loader).

	// Callback array (each entry is a VA; null-terminated).
	for i, rva := range tb.callbacks {
		va := imageBase + uint64(rva)
		le.PutUint64(buf[callbackOff+uint32(i)*8:], va)
	}
	// Null terminator already zero from make().

	dir := TLSDirectory{
		StartAddressOfRawData: startVA,
		EndAddressOfRawData:   endVA,
		AddressOfIndex:        indexVA,
		AddressOfCallbacks:    callbackVA,
		SizeOfZeroFill:        tb.zeroFill,
	}
	return dir, buf
}

// buildTLSSection is the internal helper called by build.go.
func buildTLSSection(template []byte, callbackRVAs []uint32, imageBase uint64, sectionVA uint32) (TLSDirectory, []byte) {
	tb := &TLSBuilder{template: template, callbacks: callbackRVAs}
	return tb.Build(sectionVA, imageBase)
}