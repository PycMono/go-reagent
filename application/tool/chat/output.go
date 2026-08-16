package chat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type Clock func() time.Time

func newSystemClock() Clock { return time.Now }

func jsonOutput(value any) (ai.ToolOutput, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("encode tool output: %w", err)
	}
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(data))}}, nil
}

func decodeArguments[T any](raw json.RawMessage) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, invalidArguments("invalid JSON arguments", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return value, invalidArguments("invalid JSON arguments", err)
	}
	return value, nil
}

func invalidArguments(message string, cause error) error {
	if cause == nil {
		cause = errors.New(message)
	}
	return pierrors.Wrap(pierrors.ErrorCodeToolInvalidArguments, "chat tool arguments", cause)
}
