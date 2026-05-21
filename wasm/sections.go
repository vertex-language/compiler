package wasm

// ── Type Section (ID 1) ───────────────────────────────────────────────────────

type TypeSection struct {
	Entries []FuncType
}

func (s *TypeSection) AddFuncType(ft FuncType) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, ft)
	return idx
}

func (s *TypeSection) Len() int { return len(s.Entries) }

// ── Import Section (ID 2) ─────────────────────────────────────────────────────

type ImportKind byte

const (
	ImportFunc   ImportKind = 0x00
	ImportTable  ImportKind = 0x01
	ImportMem    ImportKind = 0x02
	ImportGlobal ImportKind = 0x03
)

type ImportEntry struct {
	Module  string
	Name    string
	Kind    ImportKind
	TypeIdx uint32     // ImportFunc
	Table   TableType  // ImportTable
	Mem     MemoryType // ImportMem
	Global  GlobalType // ImportGlobal
}

type ImportSection struct {
	Entries []ImportEntry
}

func (s *ImportSection) AddFunc(module, name string, typeIdx uint32) {
	s.Entries = append(s.Entries, ImportEntry{Module: module, Name: name, Kind: ImportFunc, TypeIdx: typeIdx})
}

func (s *ImportSection) AddTable(module, name string, tt TableType) {
	s.Entries = append(s.Entries, ImportEntry{Module: module, Name: name, Kind: ImportTable, Table: tt})
}

func (s *ImportSection) AddMemory(module, name string, mt MemoryType) {
	s.Entries = append(s.Entries, ImportEntry{Module: module, Name: name, Kind: ImportMem, Mem: mt})
}

func (s *ImportSection) AddGlobal(module, name string, gt GlobalType) {
	s.Entries = append(s.Entries, ImportEntry{Module: module, Name: name, Kind: ImportGlobal, Global: gt})
}

// NumFuncs returns the number of imported functions.
// Locally-defined functions are indexed starting at this offset.
func (s *ImportSection) NumFuncs() uint32 {
	var n uint32
	for _, e := range s.Entries {
		if e.Kind == ImportFunc {
			n++
		}
	}
	return n
}

func (s *ImportSection) Len() int { return len(s.Entries) }

// ── Function Section (ID 3) ───────────────────────────────────────────────────

type FunctionSection struct {
	TypeIndices []uint32
}

func (s *FunctionSection) Add(typeIdx uint32) uint32 {
	idx := uint32(len(s.TypeIndices))
	s.TypeIndices = append(s.TypeIndices, typeIdx)
	return idx
}

func (s *FunctionSection) Len() int { return len(s.TypeIndices) }

// ── Table Section (ID 4) ──────────────────────────────────────────────────────

type TableSection struct {
	Entries []TableType
}

func (s *TableSection) Add(tt TableType) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, tt)
	return idx
}

func (s *TableSection) Len() int { return len(s.Entries) }

// ── Memory Section (ID 5) ─────────────────────────────────────────────────────

type MemorySection struct {
	Entries []MemoryType
}

func (s *MemorySection) Add(mt MemoryType) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, mt)
	return idx
}

func (s *MemorySection) Len() int { return len(s.Entries) }

// ── Global Section (ID 6) ─────────────────────────────────────────────────────

type GlobalEntry struct {
	Type GlobalType
	Init ConstExpr
}

type GlobalSection struct {
	Entries []GlobalEntry
}

func (s *GlobalSection) Add(gt GlobalType, init ConstExpr) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, GlobalEntry{gt, init})
	return idx
}

func (s *GlobalSection) Len() int { return len(s.Entries) }

// ── Export Section (ID 7) ─────────────────────────────────────────────────────

type ExportKind byte

const (
	ExportFunc   ExportKind = 0x00
	ExportTable  ExportKind = 0x01
	ExportMem    ExportKind = 0x02
	ExportGlobal ExportKind = 0x03
)

type ExportEntry struct {
	Name string
	Kind ExportKind
	Idx  uint32
}

type ExportSection struct {
	Entries []ExportEntry
}

func (s *ExportSection) Add(name string, kind ExportKind, idx uint32) {
	s.Entries = append(s.Entries, ExportEntry{name, kind, idx})
}

func (s *ExportSection) Len() int { return len(s.Entries) }

// ── Element Section (ID 9) ────────────────────────────────────────────────────

type ElemMode uint8

const (
	ElemModeActive      ElemMode = iota // initialises a table at instantiation
	ElemModePassive                      // loaded at runtime via table.init
	ElemModeDeclarative                  // forward-declares func references for ref.func
)

// ElemItems is a sum type: either a plain funcidx list or a constexpr list.
type ElemItems interface{ elemItems() }

type ElemFuncIndices []uint32

func (ElemFuncIndices) elemItems() {}

type ElemExpressions struct {
	RefType ValType
	Exprs   []ConstExpr
}

func (ElemExpressions) elemItems() {}

type ElemSegment struct {
	Mode     ElemMode
	TableIdx uint32    // ElemModeActive only
	Offset   ConstExpr // ElemModeActive only
	Items    ElemItems
}

type ElementSection struct {
	Entries []ElemSegment
}

func (s *ElementSection) Add(seg ElemSegment) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, seg)
	return idx
}

func (s *ElementSection) Len() int { return len(s.Entries) }

// ── Code Section (ID 10) ──────────────────────────────────────────────────────

type CodeSection struct {
	Bodies []*FunctionBody
}

func (s *CodeSection) Add(body *FunctionBody) {
	s.Bodies = append(s.Bodies, body)
}

func (s *CodeSection) Len() int { return len(s.Bodies) }

// ── Data Section (ID 11) ──────────────────────────────────────────────────────

type DataMode interface{ dataMode() }

type DataModePassive struct{}

func (DataModePassive) dataMode() {}

type DataModeActive struct {
	MemIdx uint32
	Offset ConstExpr
}

func (DataModeActive) dataMode() {}

type DataEntry struct {
	Mode DataMode
	Data []byte
}

type DataSection struct {
	Entries []DataEntry
}

func (s *DataSection) Add(mode DataMode, data []byte) uint32 {
	idx := uint32(len(s.Entries))
	s.Entries = append(s.Entries, DataEntry{mode, data})
	return idx
}

func (s *DataSection) Len() int { return len(s.Entries) }

// ── Custom Section (ID 0) ─────────────────────────────────────────────────────

type CustomSection struct {
	Name string
	Data []byte
}