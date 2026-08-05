package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/ai"
)

var (
	ErrRequestInvalid = errors.New("agent request invalid")
	ErrToolRuntime    = errors.New("agent tool runtime failed")
)

func validateRunRequest(request RunRequest) error {
	if request.Input.Role != ai.RoleUser {
		return fmt.Errorf("%w: input role must be user, got %q", ErrRequestInvalid, request.Input.Role)
	}
	inputText, err := ai.TextContent(request.Input.Content)
	if err != nil {
		return fmt.Errorf("%w: input content: %v", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(inputText) == "" {
		return fmt.Errorf("%w: input content must not be empty", ErrRequestInvalid)
	}
	if len(request.Input.ToolCalls) != 0 || request.Input.ToolCallID != "" || request.Input.ToolName != "" || request.Input.IsError {
		return fmt.Errorf("%w: input must not contain tool fields", ErrRequestInvalid)
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("%w: context block %d name must not be empty", ErrRequestInvalid, index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("%w: context block %d content must not be empty", ErrRequestInvalid, index)
		}
	}
	return nil
}
