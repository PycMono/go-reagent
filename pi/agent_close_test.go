package pi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
)

func TestAgentCloseRejectsNewRunsWhileWaitingForActiveRun(t *testing.T) {
	actionStarted := make(chan struct{})
	releaseAction := make(chan struct{})
	sdk := newTestAgentWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, hasTools := body["tools"]; !hasTools {
			writeOpenAIMessage(t, w, "plan")
			return
		}
		close(actionStarted)
		<-releaseAction
		writeOpenAIMessage(t, w, "done")
	})

	runDone := make(chan error, 1)
	go func() {
		_, err := sdk.Run(context.Background(), pi.RunRequest{Input: pi.UserMessage("active")})
		runDone <- err
	}()
	<-actionStarted

	closeDone := make(chan error, 1)
	go func() { closeDone <- sdk.Close(context.Background()) }()

	deadline := time.After(2 * time.Second)
	for {
		_, err := sdk.Run(context.Background(), pi.RunRequest{})
		if errors.Is(err, pi.ErrClosed) {
			if pi.ErrorCodeOf(err) != pi.ErrorCodeClosed {
				t.Fatalf("Run() error code = %q, error = %v", pi.ErrorCodeOf(err), err)
			}
			break
		}
		if pi.ErrorCodeOf(err) != pi.ErrorCodeRequestInvalid {
			t.Fatalf("Run() before Close admission error = %v", err)
		}
		select {
		case <-deadline:
			t.Fatal("Close did not reject new Runs")
		default:
			runtime.Gosched()
		}
	}

	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned before the active Run completed: %v", err)
	default:
	}
	close(releaseAction)
	if err := <-runDone; err != nil {
		t.Fatalf("active Run() error = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAgentCloseCachesDeadlineError(t *testing.T) {
	sdk := newTestAgentWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("model server should not be called")
	})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	first := sdk.Close(ctx)
	if !errors.Is(first, context.DeadlineExceeded) || pi.ErrorCodeOf(first) != pi.ErrorCodeDeadlineExceeded {
		t.Fatalf("first Close() error = %v", first)
	}
	second := sdk.Close(context.Background())
	if second != first {
		t.Fatalf("second Close() error = %v, want cached first error %v", second, first)
	}
}
