package macho

import "sort"

// ──────────────────────────────────────────────────────────────────────────────
// Rebase / bind / export constants
// ──────────────────────────────────────────────────────────────────────────────

// RebaseType is a REBASE_TYPE_* pointer kind.
type RebaseType uint8

const (
	RebaseTypePointer        RebaseType = 1 // REBASE_TYPE_POINTER
	RebaseTypeTextAbsolute32 RebaseType = 2 // REBASE_TYPE_TEXT_ABSOLUTE32
	RebaseTypeTextPCRel32    RebaseType = 3 // REBASE_TYPE_TEXT_PCREL32
)

// BindType is a BIND_TYPE_* pointer kind.
type BindType uint8

const (
	BindTypePointer        BindType = 1 // BIND_TYPE_POINTER
	BindTypeTextAbsolute32 BindType = 2 // BIND_TYPE_TEXT_ABSOLUTE32
	BindTypeTextPCRel32    BindType = 3 // BIND_TYPE_TEXT_PCREL32
)

// BindSpecial represents a negative dylib ordinal for special binding sources.
type BindSpecial int

const (
	BindSpecialSelf        BindSpecial = 0  // BIND_SPECIAL_DYLIB_SELF
	BindSpecialMainExec    BindSpecial = -1 // BIND_SPECIAL_DYLIB_MAIN_EXECUTABLE
	BindSpecialFlatLookup  BindSpecial = -2 // BIND_SPECIAL_DYLIB_FLAT_LOOKUP
	BindSpecialWeakLookup  BindSpecial = -3 // BIND_SPECIAL_DYLIB_WEAK_LOOKUP
)

// ExportFlags is an EXPORT_SYMBOL_FLAGS_* bitmask stored in the export trie.
type ExportFlags uint64

const (
	// Kind bits (low 2 bits).
	ExportKindRegular    ExportFlags = 0x00 // EXPORT_SYMBOL_FLAGS_KIND_REGULAR
	ExportKindAbsolute   ExportFlags = 0x02 // EXPORT_SYMBOL_FLAGS_KIND_ABSOLUTE
	ExportKindThreadLocal ExportFlags = 0x03 // EXPORT_SYMBOL_FLAGS_KIND_THREAD_LOCAL

	// Attribute bits.
	ExportWeakDefinition  ExportFlags = 0x04 // EXPORT_SYMBOL_FLAGS_WEAK_DEFINITION
	ExportReexport        ExportFlags = 0x08 // EXPORT_SYMBOL_FLAGS_REEXPORT
	ExportStubAndResolver ExportFlags = 0x10 // EXPORT_SYMBOL_FLAGS_STUB_AND_RESOLVER
	ExportStaticResolver  ExportFlags = 0x20 // EXPORT_SYMBOL_FLAGS_STATIC_RESOLVER
)

// ──────────────────────────────────────────────────────────────────────────────
// Entry types
// ──────────────────────────────────────────────────────────────────────────────

// RebaseEntry is one address location requiring an ASLR slide fixup.
type RebaseEntry struct {
	// SegIndex is the zero-based segment index (matching the order segments
	// are added to the Builder, excluding __PAGEZERO).
	SegIndex  int
	SegOffset uint64
	Type      RebaseType
}

// BindEntry is one external-symbol binding operation.
type BindEntry struct {
	SegIndex   int
	SegOffset  uint64
	// LibOrdinal is 1-based (index into the LC_LOAD_DYLIB list) or a
	// BindSpecial* value (cast to int).
	LibOrdinal int
	Name       string
	Type       BindType
	Addend     int64
	// Weak places this entry in the weak-bind table rather than the bind table.
	Weak bool
	// Lazy places this entry in the lazy-bind table.
	Lazy bool
}

// ExportEntry is one symbol exported by this image via the export trie.
type ExportEntry struct {
	Name    string
	// Address is the VM offset from the image base (slide not included).
	// Set to 0 for re-export entries.
	Address uint64
	Flags   ExportFlags

	// ReexportLibOrdinal and ReexportName are used when ExportReexport is set.
	// ReexportName may be "" to indicate the symbol has the same name in the
	// re-exported dylib.
	ReexportLibOrdinal int
	ReexportName       string

	// StubOffset and ResolverOffset are used when ExportStubAndResolver is set.
	StubOffset     uint64
	ResolverOffset uint64
}

// ──────────────────────────────────────────────────────────────────────────────
// DyldInfoBuilder — legacy LC_DYLD_INFO_ONLY
// ──────────────────────────────────────────────────────────────────────────────

// DyldInfoBuilder encodes the rebase, bind, weak-bind, lazy-bind, and export
// data for the legacy LC_DYLD_INFO_ONLY load command (used on macOS < 12 /
// iOS < 14).
//
// For modern targets prefer ChainedFixupsBuilder (LC_DYLD_CHAINED_FIXUPS)
// paired with the export trie from this builder.
type DyldInfoBuilder struct {
	rebases  []RebaseEntry
	binds    []BindEntry
	exports  []ExportEntry
}

// NewDyldInfoBuilder returns a DyldInfoBuilder ready to accept entries.
func NewDyldInfoBuilder() *DyldInfoBuilder {
	return &DyldInfoBuilder{}
}

// AddRebase records an address that needs ASLR sliding at load time.
func (d *DyldInfoBuilder) AddRebase(e RebaseEntry) {
	d.rebases = append(d.rebases, e)
}

// AddBind records a symbol that must be bound by dyld.
// Set e.Weak=true for weak bindings, e.Lazy=true for lazy (PLT) bindings.
func (d *DyldInfoBuilder) AddBind(e BindEntry) {
	d.binds = append(d.binds, e)
}

// AddExport records a symbol exported by this image.
func (d *DyldInfoBuilder) AddExport(e ExportEntry) {
	d.exports = append(d.exports, e)
}

// Build serialises all tables and returns the five __LINKEDIT blobs.
// The caller passes them to Builder.SetDyldInfo.
func (d *DyldInfoBuilder) Build() (rebase, bind, weakBind, lazyBind, exportTrie []byte) {
	rebase = d.buildRebase()
	bind, weakBind, lazyBind = d.buildBindTables()
	exportTrie = d.buildExportTrie()
	return
}

// ─── rebase opcodes ───────────────────────────────────────────────────────────

const (
	rebaseOpcDone                          = 0x00
	rebaseOpcSetTypeImm                    = 0x10
	rebaseOpcSetSegAndOffULEB              = 0x20
	rebaseOpcAddAddrULEB                   = 0x30
	rebaseOpcAddAddrImmScaled              = 0x40
	rebaseOpcDoRebaseImmTimes              = 0x50
	rebaseOpcDoRebaseULEBTimes             = 0x60
	rebaseOpcDoRebaseAddAddrULEB           = 0x70
	rebaseOpcDoRebaseULEBTimesSkippingULEB = 0x80
)

func (d *DyldInfoBuilder) buildRebase() []byte {
	if len(d.rebases) == 0 {
		return nil
	}
	var b []byte
	curType := RebaseType(0)
	for _, r := range d.rebases {
		if r.Type != curType {
			b = append(b, rebaseOpcSetTypeImm|byte(r.Type))
			curType = r.Type
		}
		b = append(b, rebaseOpcSetSegAndOffULEB|byte(r.SegIndex&0xf))
		b = appendULEB128(b, r.SegOffset)
		b = append(b, rebaseOpcDoRebaseImmTimes|1)
	}
	b = append(b, rebaseOpcDone)
	// Pad to 8-byte boundary.
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	return b
}

// ─── bind opcodes ─────────────────────────────────────────────────────────────

const (
	bindOpcDone                      = 0x00
	bindOpcSetDylibOrdinalImm        = 0x10
	bindOpcSetDylibOrdinalULEB       = 0x20
	bindOpcSetDylibSpecialImm        = 0x30
	bindOpcSetSymbolTrailingFlagsImm = 0x40
	bindOpcSetTypeImm                = 0x50
	bindOpcSetAddendSLEB             = 0x60
	bindOpcSetSegAndOffULEB          = 0x70
	bindOpcAddAddrULEB               = 0x80
	bindOpcDoBind                    = 0x90
	bindOpcDoBindAddAddrULEB         = 0xa0
	bindOpcDoBindAddAddrImmScaled    = 0xb0
	bindOpcDoBindULEBTimesSkippingULEB = 0xc0

	bindSymFlagWeakImport      = 0x01
	bindSymFlagNonWeakDef      = 0x08
)

func (d *DyldInfoBuilder) buildBindTables() (bind, weakBind, lazyBind []byte) {
	var regular, weak, lazy []BindEntry
	for _, b := range d.binds {
		switch {
		case b.Weak:
			weak = append(weak, b)
		case b.Lazy:
			lazy = append(lazy, b)
		default:
			regular = append(regular, b)
		}
	}
	bind = encodeBindTable(regular)
	weakBind = encodeBindTable(weak)
	lazyBind = encodeBindTable(lazy)
	return
}

func encodeBindTable(entries []BindEntry) []byte {
	if len(entries) == 0 {
		return nil
	}
	var b []byte
	curOrdinal := 0
	curSym := ""
	curType := BindType(0)
	curAddend := int64(0)

	for _, e := range entries {
		// Dylib ordinal.
		if e.LibOrdinal != curOrdinal {
			switch {
			case e.LibOrdinal > 0 && e.LibOrdinal <= 15:
				b = append(b, byte(bindOpcSetDylibOrdinalImm|e.LibOrdinal))
			case e.LibOrdinal > 15:
				b = append(b, bindOpcSetDylibOrdinalULEB)
				b = appendULEB128(b, uint64(e.LibOrdinal))
			default: // special (negative)
				b = append(b, byte(bindOpcSetDylibSpecialImm|(e.LibOrdinal&0xf)))
			}
			curOrdinal = e.LibOrdinal
		}
		// Symbol name.
		if e.Name != curSym {
			flags := byte(0)
			if e.Weak {
				flags |= bindSymFlagWeakImport
			}
			b = append(b, byte(bindOpcSetSymbolTrailingFlagsImm|int(flags)))
			b = append(b, []byte(e.Name)...)
			b = append(b, 0)
			curSym = e.Name
		}
		// Bind type.
		if e.Type != curType {
			b = append(b, byte(bindOpcSetTypeImm|byte(e.Type)))
			curType = e.Type
		}
		// Addend.
		if e.Addend != curAddend {
			b = append(b, bindOpcSetAddendSLEB)
			b = appendSLEB128(b, e.Addend)
			curAddend = e.Addend
		}
		// Segment + offset.
		b = append(b, byte(bindOpcSetSegAndOffULEB|byte(e.SegIndex&0xf)))
		b = appendULEB128(b, e.SegOffset)
		// Do bind.
		b = append(b, bindOpcDoBind)
	}
	b = append(b, bindOpcDone)
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	return b
}

// ─── export trie ─────────────────────────────────────────────────────────────

// BuildExportTrie serialises a standalone export trie for use with
// LC_DYLD_EXPORTS_TRIE (modern) or as the export chunk of LC_DYLD_INFO_ONLY.
// Callers can use this without a full DyldInfoBuilder.
func BuildExportTrie(exports []ExportEntry) []byte {
	d := &DyldInfoBuilder{exports: exports}
	return d.buildExportTrie()
}

func (d *DyldInfoBuilder) buildExportTrie() []byte {
	if len(d.exports) == 0 {
		// A valid but empty trie is a single terminal-size=0, child-count=0 byte.
		return []byte{0, 0}
	}

	// Sort by name so the trie is deterministic.
	sorted := make([]ExportEntry, len(d.exports))
	copy(sorted, d.exports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	root := buildTrieNode(sorted, "")
	// Iteratively assign offsets until stable.
	for {
		offset := uint32(0)
		assignOffsets(root, &offset)
		offset2 := uint32(0)
		assignOffsets(root, &offset2)
		if offset == offset2 {
			break
		}
	}
	var out []byte
	serializeNode(root, &out)
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	return out
}

// ─── trie node ───────────────────────────────────────────────────────────────

type trieNode struct {
	prefix   string
	entry    *ExportEntry // non-nil for terminal nodes
	children []*trieNode
	offset   uint32 // file offset of this node within the trie blob
}

func buildTrieNode(exports []ExportEntry, prefix string) *trieNode {
	node := &trieNode{prefix: prefix}
	// Find entries that exactly match prefix (terminal).
	for i := range exports {
		if exports[i].Name == prefix {
			e := exports[i]
			node.entry = &e
			break
		}
	}
	// Group remaining by next character after prefix.
	groups := map[byte][]ExportEntry{}
	for _, e := range exports {
		if e.Name != prefix && len(e.Name) > len(prefix) && e.Name[:len(prefix)] == prefix {
			next := e.Name[len(prefix)]
			groups[next] = append(groups[next], e)
		}
	}
	// For each group find the longest common prefix and recurse.
	keys := make([]byte, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		g := groups[k]
		edgePrefix := longestCommonPrefix(g, prefix)
		child := buildTrieNode(g, edgePrefix)
		node.children = append(node.children, child)
	}
	return node
}

func longestCommonPrefix(exports []ExportEntry, base string) string {
	if len(exports) == 0 {
		return base
	}
	ref := exports[0].Name
	end := len(ref)
	for _, e := range exports[1:] {
		for i := range end {
			if i >= len(e.Name) || e.Name[i] != ref[i] {
				end = i
				break
			}
		}
	}
	if end <= len(base) {
		return base
	}
	return ref[:end]
}

func assignOffsets(node *trieNode, offset *uint32) {
	node.offset = *offset
	nodeSize := trieNodeSize(node)
	*offset += nodeSize
	for _, child := range node.children {
		assignOffsets(child, offset)
	}
}

func trieNodeSize(node *trieNode) uint32 {
	var terminalBytes []byte
	if node.entry != nil {
		terminalBytes = encodeTerminal(node.entry)
	}
	terminalSize := uint32(len(terminalBytes))
	sz := uint32(len(appendULEB128(nil, uint64(terminalSize)))) + terminalSize
	sz++ // child count byte
	for _, child := range node.children {
		edge := child.prefix[len(node.prefix):]
		sz += uint32(len(edge)) + 1            // edge string + NUL
		sz += uint32(len(appendULEB128(nil, uint64(child.offset))))
	}
	return sz
}

func serializeNode(node *trieNode, out *[]byte) {
	var terminalBytes []byte
	if node.entry != nil {
		terminalBytes = encodeTerminal(node.entry)
	}
	*out = appendULEB128(*out, uint64(len(terminalBytes)))
	*out = append(*out, terminalBytes...)
	*out = append(*out, byte(len(node.children)))
	for _, child := range node.children {
		edge := child.prefix[len(node.prefix):]
		*out = append(*out, []byte(edge)...)
		*out = append(*out, 0)
		*out = appendULEB128(*out, uint64(child.offset))
	}
	for _, child := range node.children {
		serializeNode(child, out)
	}
}

func encodeTerminal(e *ExportEntry) []byte {
	var b []byte
	b = appendULEB128(b, uint64(e.Flags))
	if e.Flags&ExportReexport != 0 {
		b = appendULEB128(b, uint64(e.ReexportLibOrdinal))
		b = append(b, []byte(e.ReexportName)...)
		b = append(b, 0)
	} else if e.Flags&ExportStubAndResolver != 0 {
		b = appendULEB128(b, e.StubOffset)
		b = appendULEB128(b, e.ResolverOffset)
	} else {
		b = appendULEB128(b, e.Address)
	}
	return b
}

// ──────────────────────────────────────────────────────────────────────────────
// ULEB128 / SLEB128 helpers
// ──────────────────────────────────────────────────────────────────────────────

func appendULEB128(b []byte, v uint64) []byte {
	for {
		bite := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			bite |= 0x80
		}
		b = append(b, bite)
		if v == 0 {
			break
		}
	}
	return b
}

func appendSLEB128(b []byte, v int64) []byte {
	for {
		bite := byte(v & 0x7f)
		v >>= 7
		more := !((v == 0 && bite&0x40 == 0) || (v == -1 && bite&0x40 != 0))
		if more {
			bite |= 0x80
		}
		b = append(b, bite)
		if !more {
			break
		}
	}
	return b
}

// BuildFunctionStarts encodes a function-starts table from a slice of virtual
// addresses (which must be sorted ascending).  The result goes into the
// LC_FUNCTION_STARTS linkedit blob.
//
// The encoding is a ULEB128 delta sequence relative to the start of __TEXT,
// terminated by a zero byte.
func BuildFunctionStarts(textVMAddr uint64, funcVAs []uint64) []byte {
	if len(funcVAs) == 0 {
		return nil
	}
	var b []byte
	prev := textVMAddr
	for _, va := range funcVAs {
		delta := va - prev
		b = appendULEB128(b, delta)
		prev = va
	}
	b = append(b, 0) // terminator
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	return b
}