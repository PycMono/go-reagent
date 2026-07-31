package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestNewApplicationLoggerEmitsJSONWithProjectModule(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writeEnd
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	logger := newApplicationLogger()
	logger.Info(context.Background(), "logger ready")
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}

	var event map[string]any
	if err := json.Unmarshal(encoded, &event); err != nil {
		t.Fatalf("log output = %q, error = %v", encoded, err)
	}
	if event["module"] != "go-reagent" || event["msg"] != "logger ready" || event["level"] != "info" {
		t.Fatalf("log event = %#v", event)
	}
}
