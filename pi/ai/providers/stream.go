package providers

import "github.com/PycMono/go-reagent/pi/ai"

type streamState struct {
	current  ai.StreamEvent
	started  bool
	terminal bool
	result   *ai.Message
	err      error
}

func (s *streamState) Current() ai.StreamEvent { return s.current }

func (s *streamState) Result() (*ai.Message, error) { return s.result, s.err }

func (s *streamState) start() bool {
	if s.started {
		return false
	}
	s.started = true
	s.current = ai.StreamEvent{Type: ai.StreamEventStart}
	return true
}

func (s *streamState) fail(err error) bool {
	s.err = err
	s.terminal = true
	s.current = ai.StreamEvent{Type: ai.StreamEventError}
	return true
}

func (s *streamState) done() bool {
	s.terminal = true
	s.current = ai.StreamEvent{Type: ai.StreamEventDone}
	return true
}

type failedStream struct {
	streamState
}

func newFailedStream(err error) ai.Stream {
	return &failedStream{streamState: streamState{err: err}}
}

func (s *failedStream) Next() bool {
	if s.terminal {
		return false
	}
	if s.start() {
		return true
	}
	return s.fail(s.err)
}

func (s *failedStream) Close() error { return nil }
