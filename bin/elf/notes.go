// notes.go
package elf

import "encoding/binary"

// ── Note type constants ───────────────────────────────────────────────────────
//
// These are n_type values when the note owner (n_name) is "GNU".

const (
	NT_GNU_ABI_TAG      = 1 // minimum OS/ABI version (.note.ABI-tag)
	NT_GNU_HWCAP        = 2 // hardware capability bitfield
	NT_GNU_BUILD_ID     = 3 // unique build identifier (.note.gnu.build-id)
	NT_GNU_GOLD_VERSION = 4 // GNU gold linker version
	NT_GNU_PROPERTY     = 5 // program properties (.note.gnu.property)
)

// GNU ABI tag OS identifiers — desc[0] in NT_GNU_ABI_TAG notes.
const (
	GNU_ABI_TAG_LINUX    = 0
	GNU_ABI_TAG_HURD     = 1
	GNU_ABI_TAG_SOLARIS  = 2
	GNU_ABI_TAG_FREEBSD  = 3
	GNU_ABI_TAG_NETBSD   = 4
	GNU_ABI_TAG_SYLLABLE = 5
	GNU_ABI_TAG_NACL     = 6
)

// GNU property type constants for NT_GNU_PROPERTY notes.
// Used by the Linux kernel and glibc to enable hardware security features.
const (
	// AMD64 Control-flow Enforcement Technology (CET) properties.
	GNU_PROPERTY_X86_FEATURE_1_AND   = uint32(0xc0000002)
	GNU_PROPERTY_X86_FEATURE_1_IBT   = uint32(0x1) // indirect branch tracking
	GNU_PROPERTY_X86_FEATURE_1_SHSTK = uint32(0x2) // shadow stack
)

// ── Note type ─────────────────────────────────────────────────────────────────

// Note is a single ELF note entry (Elf64_Nhdr + name + desc).
type Note struct {
	Name string // owner name, e.g. "GNU"; written NUL-terminated in the file
	Type uint32 // NT_* constant
	Desc []byte // note payload
}

// ── Builders ──────────────────────────────────────────────────────────────────

// BuildNoteSection serializes a slice of Notes into a SHT_NOTE section body.
// Each entry is 4-byte aligned per the ELF spec. The result may be used
// directly as Section.Data for a section with Type=SHT_NOTE.
func BuildNoteSection(notes []Note) []byte {
	var buf []byte
	for _, n := range notes {
		namez := append([]byte(n.Name), 0) // NUL-terminated
		namesz := uint32(len(namez))
		descsz := uint32(len(n.Desc))

		var hdr [12]byte
		binary.LittleEndian.PutUint32(hdr[0:], namesz)
		binary.LittleEndian.PutUint32(hdr[4:], descsz)
		binary.LittleEndian.PutUint32(hdr[8:], n.Type)
		buf = append(buf, hdr[:]...)
		buf = appendPad4(buf, namez)
		buf = appendPad4(buf, n.Desc)
	}
	return buf
}

// BuildBuildID returns a .note.gnu.build-id section body.
// id is the raw identifier — typically a SHA-1 digest (20 bytes) computed
// over the final binary, or a UUID (16 bytes) for reproducible builds.
func BuildBuildID(id []byte) []byte {
	return BuildNoteSection([]Note{{
		Name: "GNU",
		Type: NT_GNU_BUILD_ID,
		Desc: id,
	}})
}

// BuildABITag returns a .note.ABI-tag section body declaring the minimum Linux
// kernel version required. Example: major=2, minor=6, patch=32 for Linux 2.6.32.
func BuildABITag(major, minor, patch uint32) []byte {
	desc := make([]byte, 16)
	binary.LittleEndian.PutUint32(desc[0:], GNU_ABI_TAG_LINUX)
	binary.LittleEndian.PutUint32(desc[4:], major)
	binary.LittleEndian.PutUint32(desc[8:], minor)
	binary.LittleEndian.PutUint32(desc[12:], patch)
	return BuildNoteSection([]Note{{
		Name: "GNU",
		Type: NT_GNU_ABI_TAG,
		Desc: desc,
	}})
}

// BuildGNUProperty returns a .note.gnu.property section body for AMD64 CET.
// featureFlags is a bitmask of GNU_PROPERTY_X86_FEATURE_1_* values.
// Without this note, the kernel will not enable hardware IBT/shadow stack
// for the process even if the CPU supports it.
func BuildGNUProperty(featureFlags uint32) []byte {
	// GNU property format for a single 4-byte property:
	//   pr_type:   uint32 = GNU_PROPERTY_X86_FEATURE_1_AND
	//   pr_datasz: uint32 = 4
	//   pr_data:   uint32 = featureFlags
	//   padding:   uint32 = 0  (8-byte align on 64-bit)
	desc := make([]byte, 16)
	binary.LittleEndian.PutUint32(desc[0:], GNU_PROPERTY_X86_FEATURE_1_AND)
	binary.LittleEndian.PutUint32(desc[4:], 4) // pr_datasz
	binary.LittleEndian.PutUint32(desc[8:], featureFlags)
	// desc[12:16] = padding (zero)
	return BuildNoteSection([]Note{{
		Name: "GNU",
		Type: NT_GNU_PROPERTY,
		Desc: desc,
	}})
}

// appendPad4 appends data to buf, then zero-pads buf to a 4-byte boundary.
func appendPad4(buf, data []byte) []byte {
	buf = append(buf, data...)
	if r := len(buf) % 4; r != 0 {
		buf = append(buf, make([]byte, 4-r)...)
	}
	return buf
}