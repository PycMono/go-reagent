package mcp

import "fmt"

type transportError struct {
	op     string
	kind   string
	status int
	code   int
	cause  error
}

func (err *transportError) Error() string {
	switch {
	case err.status != 0:
		return fmt.Sprintf("mcp %s: HTTP status %d", err.op, err.status)
	case err.code != 0:
		return fmt.Sprintf("mcp %s: remote JSON-RPC error code %d", err.op, err.code)
	case err.cause != nil:
		return fmt.Sprintf("mcp %s: %s: %v", err.op, err.kind, err.cause)
	default:
		return fmt.Sprintf("mcp %s: %s", err.op, err.kind)
	}
}

func (err *transportError) Unwrap() error { return err.cause }
