package agenttemplate

import "context"

// Assembler copies one server-owned template into an empty destination.
type Assembler interface {
	Assemble(context.Context, string, string) error
}
