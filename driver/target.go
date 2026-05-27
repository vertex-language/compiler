// driver/target.go  (unchanged — shown for completeness)
package driver

import "github.com/vertex-language/compiler/context"

// Target is the standard interface for all code emitters (amd64, cuda, etc.).
type Target interface {
	// ID returns the unique routing key for this backend (e.g. "amd64", "cuda").
	ID() string

	// Emit compiles the requested wasm function indices and writes the
	// resulting machine code, data, and symbols into the shared BuildContext.
	Emit(ctx *context.BuildContext, funcIndices []int) error
}