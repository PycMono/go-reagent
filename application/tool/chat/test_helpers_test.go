package chat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
)

func fixedClock(value string) Clock {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return instant }
}

func decodeToolJSON[T any](t *testing.T, output ai.ToolOutput) T {
	t.Helper()
	text, err := ai.TextContent(output.Content)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
