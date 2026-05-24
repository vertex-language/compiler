package pe

import (
	"fmt"
	"strings"
)

// File-level layout constants.
const (
	fileAlignment     = uint32(0x200)
	sectionAlignment  = uint32(0x1000)
	peSignatureSize   = 4
	coffHeaderSize    = 20
	optHeaderSize     = 240
	sectionHeaderSize = 40
	dosStubSize       = 64
	fixedHeaderBytes  = dosStubSize + peSignatureSize + coffHeaderSize + optHeaderSize
)

var dosStub = [dosStubSize]byte{
	0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00,
	0x04, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00,
	0xB8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00,
}

const (
	defaultExeImageBase = uint64(0x0000000140000000)
	defaultDLLImageBase = uint64(0x0000000180000000)
)

const (
	defaultStackReserve = uint64(1 << 20)
	defaultStackCommit  = uint64(1 << 12)
	defaultHeapReserve  = uint64(1 << 20)
	defaultHeapCommit   = uint64(1 << 12)
)

// Builder assembles a PE32+ image from sections, symbols, relocations,
// imports, and exports.
type Builder struct {
	arch               Arch
	sections           []Section
	symbols            []Symbol
	relocs             []Reloc
	imports            []Import
	exports            []Export
	debug              *DebugData
	entry              string
	subsystem          Subsystem
	imageBase          uint64
	stackReserve       uint64
	stackCommit        uint64
	heapReserve        uint64
	heapCommit         uint64
	dllCharacteristics uint16
	extraFileChars     uint16
	dllMode            bool
	dllName            string
}

// NewBuilder returns a Builder configured for the given architecture.
func NewBuilder(arch Arch) *Builder {
	return &Builder{
		arch:      arch,
		subsystem: SubsystemConsole,
		dllCharacteristics: IMAGE_DLLCHARACTERISTICS_HIGH_ENTROPY_VA |
			IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE |
			IMAGE_DLLCHARACTERISTICS_NX_COMPAT,
	}
}

func (b *Builder) AddSection(s Section)  { b.sections = append(b.sections, s) }
func (b *Builder) AddSymbol(s Symbol)    { b.symbols = append(b.symbols, s) }
func (b *Builder) AddReloc(r Reloc)      { b.relocs = append(b.relocs, r) }
func (b *Builder) AddImport(imp Import)  { b.imports = append(b.imports, imp) }
func (b *Builder) AddExport(e Export)    { b.exports = append(b.exports, e) }
func (b *Builder) SetDebug(d DebugData)  { b.debug = &d }
func (b *Builder) SetEntry(name string)  { b.entry = name }

func (b *Builder) SetSubsystem(ss Subsystem)       { b.subsystem = ss }
func (b *Builder) SetImageBase(base uint64)         { b.imageBase = base }
func (b *Builder) SetDLLCharacteristics(f uint16)  { b.dllCharacteristics = f }

func (b *Builder) SetStackSize(reserve, commit uint64) {
	b.stackReserve, b.stackCommit = reserve, commit
}

func (b *Builder) SetHeapSize(reserve, commit uint64) {
	b.heapReserve, b.heapCommit = reserve, commit
}

func (b *Builder) SetDLL(name string) {
	b.dllMode = true
	b.dllName = name
}

// Emit assembles and serializes the PE32+ image.
func (b *Builder) Emit() ([]byte, error) {
    img, err := b.buildImage()
    if err != nil {
        return nil, err
    }
    return serialize(img)
}

var _ = strings.TrimSpace
var _ = fmt.Sprintf