package wasm

// Module is the top-level WebAssembly module.
// Populate the section fields, then pass the Module to encoder.Encode or
// decoder.Decode. The Module itself carries no serialisation logic.
type Module struct {
	Types     TypeSection
	Imports   ImportSection
	Functions FunctionSection
	Tables    TableSection
	Memories  MemorySection
	Globals   GlobalSection
	Exports   ExportSection
	Start     *uint32 // nil = no start function
	Elements  ElementSection
	Codes     CodeSection
	Datas     DataSection
	Customs   []CustomSection
}

func NewModule() *Module { return &Module{} }

func (m *Module) SetStart(funcIdx uint32) { m.Start = &funcIdx }