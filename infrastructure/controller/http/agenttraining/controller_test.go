package agenttraining

import (
	"context"
	"encoding/json"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTrainingRoutesRequireHostAdministrator(t *testing.T) {
	for _, mode := range []string{"", config.IdentityModeHost} {
		cfg := &config.Config{}
		cfg.Identity.Mode = mode
		r := gin.New()
		RegisterRoutes(r, nil, cfg)
		for _, path := range []string{"/api/v1/training-sessions/t", "/api/v1/training-sessions/t/messages", "/api/v1/agents/a/training-sessions"} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if mode == "" && w.Code != 404 {
				t.Fatal(w.Code)
			}
			if mode != "" && w.Code != 401 && w.Code != 403 {
				t.Fatal(w.Code)
			}
		}
	}
}

func TestTrainingStreamFlushesTextBeforeRunCompletes(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &trainingStream{c: c, cancel: cancel}
	stream.OnEvent(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("逐步输出")))
	if !w.Flushed || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(w.Body.String(), "逐步输出") {
		t.Fatalf("stream not flushed: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "run.completed") {
		t.Fatal("premature completion")
	}
	stream.send("run.completed", gin.H{"type": "run.completed"})
	if !strings.Contains(w.Body.String(), "event: run.completed") {
		t.Fatal(w.Body.String())
	}
}

func TestTrainingStreamSerializesParallelToolEvents(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	stream := &trainingStream{c: c, cancel: func() {}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewStartEvent(ai.ToolCall{ID: "call", Name: "training_file"})))
		}()
	}
	wg.Wait()
	frames := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	if len(frames) != 20 {
		t.Fatalf("frames=%d", len(frames))
	}
	for _, frame := range frames {
		_, data, ok := strings.Cut(frame, "data: ")
		if !ok || !json.Valid([]byte(data)) {
			t.Fatalf("corrupted event: %s", frame)
		}
	}
}
