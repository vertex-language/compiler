package raw

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	bootSigOffset = 510  // byte offset of the MBR/VBR boot signature within the sector
	bootSigLow    = 0x55 // first byte of the boot signature
	bootSigHigh   = 0xAA // second byte of the boot signature
)

// Builder constructs a flat raw binary placed at a fixed origin address.
//
// The output contains no file format header of any kind — it is plain machine
// code written directly to disk or ROM and executed by hardware. Internal
// cross-section references are resolved at Emit time by patching the byte
// slots described by the registered Reloc entries.
//
// Typical usage for a BIOS MBR bootloader:
//
//	b := raw.NewBuilder()
//	b.SetOrigin(0x7C00)
//	b.AddSection(raw.Section{Data: machineCode, Align: 1})
//	b.SetBootSignature()
//	out, err := b.Emit()
//	os.WriteFile("stage1.bin", out, 0o644)
type Builder struct {
	origin   uint32
	sections []Section
	symbols  []Symbol
	relocs   []Reloc
	padTo    uint32
	bootSig  bool
}

// NewBuilder returns a Builder with origin 0 and no sections, symbols, or
// relocations registered.
func NewBuilder() *Builder {
	return &Builder{}
}

// SetOrigin sets the physical load address of the binary.
//
// All absolute relocations are resolved against this base. For BIOS MBR and
// VBR bootloaders the conventional address is 0x7C00. For stage-2 images
// loaded by a stage-1 MBR a common choice is 0x8000 or 0x9000. Embedded
// targets use whatever address the ROM or flash is mapped to.
//
// The default is 0.
func (b *Builder) SetOrigin(addr uint32) {
	b.origin = addr
}

// Origin returns the current load address.
func (b *Builder) Origin() uint32 {
	return b.origin
}

// AddSection appends a section to the binary.
//
// Sections are laid out in the order they are added. If a section's Align
// value is greater than 1, zero-padding bytes are inserted before it until
// the running file offset is a multiple of Align.
func (b *Builder) AddSection(s Section) {
	b.sections = append(b.sections, s)
}

// AddSymbol registers a named address that can be used as a relocation target.
//
// The symbol's absolute address is computed during Emit as:
//
//	origin + section_file_offset + sym.Offset
//
// AddSymbol may be called before or after the target section is added;
// resolution is deferred to Emit.
func (b *Builder) AddSymbol(sym Symbol) {
	b.symbols = append(b.symbols, sym)
}

// AddReloc registers a relocation to be applied during Emit.
//
// The relocation site must fall within a registered section and the target
// symbol must be defined before Emit is called, otherwise Emit returns an
// error.
func (b *Builder) AddReloc(r Reloc) {
	b.relocs = append(b.relocs, r)
}

// SetPadSize pads the emitted binary to exactly size bytes by appending zero
// bytes. Emit returns an error if the unpadded binary would exceed size.
func (b *Builder) SetPadSize(size uint32) {
	b.padTo = size
}

// SetBootSignature writes the MBR/VBR boot signature (0x55, 0xAA) at byte
// offsets 510 and 511 of the output, overwriting whatever was there.
//
// SetPadSize(512) is implied automatically if no larger pad has been set,
// ensuring the output is a full 512-byte sector.
//
// Use this for any binary that will be placed in a BIOS boot sector, whether
// that is an MBR (disk LBA 0) or a VBR (partition LBA 0).
func (b *Builder) SetBootSignature() {
	b.bootSig = true
	if b.padTo < 512 {
		b.padTo = 512
	}
}

// Emit lays out all sections, applies all relocations, pads the output, and
// returns the finished byte slice.
//
// Emit does not modify the Builder and may be called more than once if the
// same binary needs to be written to multiple destinations.
func (b *Builder) Emit() ([]byte, error) {
	// ------------------------------------------------------------------ //
	// 1. Section layout                                                   //
	// ------------------------------------------------------------------ //

	type laidOut struct {
		name       string
		fileOffset uint32 // byte offset from the start of the output buffer
		data       []byte
	}

	laid := make([]laidOut, len(b.sections))
	cursor := uint32(0)

	for i, s := range b.sections {
		align := s.Align
		if align < 1 {
			align = 1
		}
		if rem := cursor % align; rem != 0 {
			cursor += align - rem
		}
		laid[i] = laidOut{
			name:       s.Name,
			fileOffset: cursor,
			data:       s.Data,
		}
		cursor += uint32(len(s.Data))
	}
	rawSize := cursor

	// ------------------------------------------------------------------ //
	// 2. Output buffer size                                               //
	// ------------------------------------------------------------------ //

	outSize := rawSize
	if b.padTo > 0 {
		if rawSize > b.padTo {
			return nil, fmt.Errorf("raw: binary size %d exceeds pad size %d", rawSize, b.padTo)
		}
		outSize = b.padTo
	}

	// ------------------------------------------------------------------ //
	// 3. Copy section data into output buffer                             //
	// ------------------------------------------------------------------ //

	out := make([]byte, outSize)
	for _, lo := range laid {
		copy(out[lo.fileOffset:], lo.data)
	}

	// ------------------------------------------------------------------ //
	// 4. Build lookup maps                                                //
	// ------------------------------------------------------------------ //

	// section name -> file offset
	secFileOffset := make(map[string]uint32, len(laid))
	for _, lo := range laid {
		if lo.name != "" {
			secFileOffset[lo.name] = lo.fileOffset
		}
	}

	// helper: resolve a section reference to its file offset
	resolveSection := func(name, ctx string) (uint32, error) {
		if name == "" {
			if len(laid) == 0 {
				return 0, fmt.Errorf("raw: %s: no sections have been added", ctx)
			}
			return laid[0].fileOffset, nil
		}
		off, ok := secFileOffset[name]
		if !ok {
			return 0, fmt.Errorf("raw: %s: unknown section %q", ctx, name)
		}
		return off, nil
	}

	// symbol name -> absolute address (origin-relative)
	symAddr := make(map[string]uint32, len(b.symbols))
	for _, sym := range b.symbols {
		secOff, err := resolveSection(sym.Section, fmt.Sprintf("symbol %q", sym.Name))
		if err != nil {
			return nil, err
		}
		symAddr[sym.Name] = b.origin + secOff + sym.Offset
	}

	// ------------------------------------------------------------------ //
	// 5. Apply relocations                                                //
	// ------------------------------------------------------------------ //

	for i, r := range b.relocs {
		secOff, err := resolveSection(r.Section, fmt.Sprintf("reloc[%d]", i))
		if err != nil {
			return nil, err
		}

		siteFileOff := secOff + r.Offset
		patchAbsAddr := b.origin + siteFileOff

		target, ok := symAddr[r.Symbol]
		if !ok {
			return nil, fmt.Errorf("raw: reloc[%d]: undefined symbol %q", i, r.Symbol)
		}

		if err := applyReloc(out, siteFileOff, outSize, r, target, patchAbsAddr); err != nil {
			return nil, err
		}
	}

	// ------------------------------------------------------------------ //
	// 6. Boot signature                                                   //
	// ------------------------------------------------------------------ //

	if b.bootSig {
		out[bootSigOffset]   = bootSigLow
		out[bootSigOffset+1] = bootSigHigh
	}

	return out, nil
}

// applyReloc patches out[siteOff:siteOff+width] according to r.
func applyReloc(out []byte, siteOff, outSize uint32, r Reloc, symAddr, patchAddr uint32) error {
	width := relocWidth(r.Type)
	if width == 0 {
		return fmt.Errorf("raw: unknown relocation type %d", r.Type)
	}
	if uint64(siteOff)+uint64(width) > uint64(outSize) {
		return fmt.Errorf("raw: reloc at 0x%x (type %v, width %d) extends past end of binary (size %d)",
			siteOff, r.Type, width, outSize)
	}

	slot := out[siteOff:]

	switch r.Type {

	case R_ABS8:
		v := int64(symAddr) + int64(r.Addend)
		if v < 0 || v > math.MaxUint8 {
			return fmt.Errorf("raw: R_ABS8 at 0x%x: value 0x%x out of [0, 0xff]", siteOff, v)
		}
		slot[0] = uint8(v)

	case R_ABS16:
		v := int64(symAddr) + int64(r.Addend)
		if v < 0 || v > math.MaxUint16 {
			return fmt.Errorf("raw: R_ABS16 at 0x%x: value 0x%x out of [0, 0xffff]", siteOff, v)
		}
		binary.LittleEndian.PutUint16(slot, uint16(v))

	case R_ABS32:
		v := int64(symAddr) + int64(r.Addend)
		if v < 0 || v > math.MaxUint32 {
			return fmt.Errorf("raw: R_ABS32 at 0x%x: value 0x%x out of [0, 0xffffffff]", siteOff, v)
		}
		binary.LittleEndian.PutUint32(slot, uint32(v))

	case R_REL8:
		// rel = sym - (patch_addr + 1) + addend
		// +1 because the CPU adds the instruction width (1 byte for the rel field)
		// to PC before adding the displacement.
		rel := int64(symAddr) - int64(patchAddr+1) + int64(r.Addend)
		if rel < math.MinInt8 || rel > math.MaxInt8 {
			return fmt.Errorf("raw: R_REL8 at 0x%x: displacement %d out of [-128, 127]", siteOff, rel)
		}
		slot[0] = uint8(int8(rel))

	case R_REL16:
		rel := int64(symAddr) - int64(patchAddr+2) + int64(r.Addend)
		if rel < math.MinInt16 || rel > math.MaxInt16 {
			return fmt.Errorf("raw: R_REL16 at 0x%x: displacement %d out of [-32768, 32767]", siteOff, rel)
		}
		binary.LittleEndian.PutUint16(slot, uint16(int16(rel)))

	case R_REL32:
		rel := int64(symAddr) - int64(patchAddr+4) + int64(r.Addend)
		if rel < math.MinInt32 || rel > math.MaxInt32 {
			return fmt.Errorf("raw: R_REL32 at 0x%x: displacement %d out of [-2147483648, 2147483647]", siteOff, rel)
		}
		binary.LittleEndian.PutUint32(slot, uint32(int32(rel)))

	case R_SEG16:
		// Real-mode segment = linear address >> 4.
		// The offset half of the far pointer is the caller's responsibility.
		v := (uint32(int64(symAddr)+int64(r.Addend)) >> 4)
		if v > math.MaxUint16 {
			return fmt.Errorf("raw: R_SEG16 at 0x%x: segment 0x%x out of [0, 0xffff]", siteOff, v)
		}
		binary.LittleEndian.PutUint16(slot, uint16(v))
	}

	return nil
}