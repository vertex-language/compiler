package driver

import "github.com/vertex-language/compiler/context"

// Target is the standard interface for all code emitters (x86_64, cuda, etc.).
type Target interface {
	// ID returns the unique identifier for this backend (e.g., "x86_64", "cuda").
	ID() string
	
	// Emit compiles the requested Wasm function indices and appends the resulting
	// machine code, data, and symbols directly into the shared BuildContext.
	Emit(ctx *context.BuildContext, funcIndices []int) error
}