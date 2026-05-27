package pe

import "encoding/binary"

// loadConfigSize is the serialized size of the IMAGE_LOAD_CONFIG_DIRECTORY64
// structure as emitted by this package (covers fields through GuardLongJumpTargetCount).
// Windows validates the Size field on load; any value up to the actual struct size
// is accepted. We emit the CFG-era size (Win 8.1 / VS 2015 compatible).
const loadConfigSize = uint32(148)

// buildLoadConfig serializes a LoadConfig into an IMAGE_LOAD_CONFIG_DIRECTORY64 blob.
// All address fields (Security Cookie, CFG tables, etc.) are virtual addresses (VAs).
func buildLoadConfig(lc *LoadConfig) []byte {
	le := binary.LittleEndian
	buf := make([]byte, loadConfigSize)

	// Size (must match the structure size the loader expects for this version).
	le.PutUint32(buf[0:], loadConfigSize)
	// TimeDateStamp [4] – leave zero (reproducible builds).
	// MajorVersion [8], MinorVersion [10] – zero.
	// GlobalFlagsClear [12], GlobalFlagsSet [16] – zero (not used here).
	// CriticalSectionDefaultTimeout [20] – zero.
	// DeCommitFreeBlockThreshold [24] – zero (64-bit, 8 bytes).
	// DeCommitTotalFreeThreshold [32] – zero.
	// LockPrefixTable [40] – zero (VA; set to 0 for non-MP-critical code).
	// MaximumAllocationSize [48] – zero.
	// VirtualMemoryThreshold [56] – zero.
	// ProcessAffinityMask [64] – zero.
	// ProcessHeapFlags [72] – zero (DWORD).
	// CSDVersion [76] – zero (WORD).
	// DependentLoadFlags [78] – WORD.
	le.PutUint16(buf[78:], lc.DependentLoadFlags)
	// EditList [80] – VA; zero (no edit list).
	// SecurityCookie [88] – VA.
	le.PutUint64(buf[88:], lc.SecurityCookieVA)
	// SEHandlerTable [96] – VA (x86 only; leave zero for x64).
	le.PutUint64(buf[96:], lc.SEHandlerTableVA)
	// SEHandlerCount [104] – QWORD.
	le.PutUint64(buf[104:], lc.SEHandlerCount)
	// GuardCFCheckFunctionPointer [112] – VA.
	le.PutUint64(buf[112:], lc.GuardCFCheckFunctionPointerVA)
	// GuardCFDispatchFunctionPointer [120] – VA.
	le.PutUint64(buf[120:], lc.GuardCFDispatchFunctionPointerVA)
	// GuardCFFunctionTable [128] – VA.
	le.PutUint64(buf[128:], lc.GuardCFFunctionTableVA)
	// GuardCFFunctionCount [136] – QWORD.
	le.PutUint64(buf[136:], lc.GuardCFFunctionCount)
	// GuardFlags [144] – DWORD.
	le.PutUint32(buf[144:], lc.GuardFlags)

	return buf
}