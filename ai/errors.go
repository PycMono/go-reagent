package ai

import (
	"errors"
	"fmt"
)

// ErrGeneration classifies model-generation failures independently of a provider SDK.
var ErrGeneration = errors.New("ai generation failed")

// GenerationError preserves the provider error while adding an AI operation.
type GenerationError struct {
	Op  string
	Err error
}

func (e *GenerationError) Error() string {
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e *GenerationError) Unwrap() error {
	return e.Err
}

func (e *GenerationError) Is(target error) bool {
	return target == ErrGeneration
}

// WrapGeneration classifies err as a generation failure without hiding its cause.
func WrapGeneration(op string, err error) error {
	if err == nil {
		return nil
	}
	return &GenerationError{Op: op, Err: err}
}
