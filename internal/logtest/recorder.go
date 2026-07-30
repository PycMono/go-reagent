package logtest

import (
	"context"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
)

// Event captures one Logger call before SDK-specific field formatting.
type Event struct {
	Level   string
	Message string
	Fields  logsdk.Fields
}

// Recorder is a concurrency-safe logsdk.Logger for tests.
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *Recorder) record(level, message string, fields ...logsdk.Fields) {
	merged := logsdk.N()
	for _, group := range fields {
		for key, value := range group {
			merged[key] = value
		}
	}

	r.mu.Lock()
	r.events = append(r.events, Event{Level: level, Message: message, Fields: merged})
	r.mu.Unlock()
}

func (r *Recorder) Debug(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("debug", message, fields...)
}

func (r *Recorder) Info(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("info", message, fields...)
}

func (r *Recorder) Warn(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("warn", message, fields...)
}

func (r *Recorder) Error(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("error", message, fields...)
}

func (r *Recorder) Fatal(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("fatal", message, fields...)
}

func (r *Recorder) Panic(_ context.Context, message string, fields ...logsdk.Fields) {
	r.record("panic", message, fields...)
}

// Find returns the first event with the requested level and message.
func (r *Recorder) Find(level, message string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.Level == level && event.Message == message {
			return event, true
		}
	}
	return Event{}, false
}
