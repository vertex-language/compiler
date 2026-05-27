package macho

import "fmt"

// ResolveSymbolAddresses computes the final virtual address of every defined
// symbol.  Must be called after AssignLayout.
func ResolveSymbolAddresses(symtab *SymbolTable, layout *Layout) error {
	for _, rs := range symtab.All() {
		switch rs.Kind {
		case kindDefined:
			if rs.IsAbs {
				rs.VAddr = rs.Value
				continue
			}
			ms := layout.SectionByKey(rs.SegmentName, rs.SectionName)
			if ms == nil {
				return fmt.Errorf("symbol %q: section %s,%s not found in layout",
					rs.Name, rs.SegmentName, rs.SectionName)
			}
			// Find the piece contributed by this object.
			pieceOff := uint64(0)
			found := false
			for _, p := range ms.Pieces {
				if p.Obj == rs.Obj && p.Sec.SectName == rs.SectionName {
					pieceOff = p.Offset
					found = true
					break
				}
			}
			if !found {
				// Symbol may be in a section contributed by a different input.
				// Try matching by object only.
				for _, p := range ms.Pieces {
					if p.Obj == rs.Obj {
						pieceOff = p.Offset
						found = true
						break
					}
				}
			}
			rs.VAddr = ms.VAddr + pieceOff + rs.Value

		case kindCommon:
			// Common symbols are placed in __DATA,__common by BuildStubs/
			// the common-block allocator; their VAddr is set there.

		case kindDylib:
			// Dylib symbols get a GOT slot address set by BuildStubs.

		case kindUndef:
			if rs.IsWeak {
				rs.VAddr = 0
			}
		}
	}
	return nil
}