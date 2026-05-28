package abi

import "fmt"

// ErrUnrecognisedImport is returned by Analyze when a wasm import module
// path uses a namespace the compiler does not recognise.
type ErrUnrecognisedImport struct {
	FuncIdx int
	Module  string
	Name    string
}

func (e *ErrUnrecognisedImport) Error() string {
	return fmt.Sprintf(
		"abi: unrecognised import namespace at func index %d: %q %q — "+
			"expected one of: linux/*, windows/*, darwin/*, lib/*, hw/*, gpu/*, memory",
		e.FuncIdx, e.Module, e.Name,
	)
}