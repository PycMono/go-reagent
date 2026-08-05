package reagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent"
)

func TestAgentConcurrentRunsAreIsolated(t *testing.T) {
	sdk := newTestAgentWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(body.Tools) == 0 {
			writeOpenAIMessage(t, w, "plan")
			return
		}
		input := ""
		for _, message := range body.Messages {
			if message.Role == "user" && strings.HasPrefix(message.Content, "input-") {
				input = message.Content
			}
		}
		writeOpenAIMessage(t, w, "done:"+input)
	})

	const runCount = 16
	type outcome struct {
		result reagent.RunResult
		err    error
	}
	outcomes := make(chan outcome, runCount)
	var wait sync.WaitGroup
	for index := range runCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := sdk.Run(context.Background(), reagent.RunRequest{
				RunID:    fmt.Sprintf("run-%d", index),
				Input:    reagent.UserMessage(fmt.Sprintf("input-%d", index)),
				Metadata: map[string]string{"index": fmt.Sprintf("%d", index)},
			})
			outcomes <- outcome{result: result, err: err}
		}()
	}
	wait.Wait()
	close(outcomes)

	seen := make(map[string]bool, runCount)
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("Run() error = %v", outcome.err)
		}
		if seen[outcome.result.RunID] || len(outcome.result.NewMessages) != 1 {
			t.Fatalf("RunResult = %#v", outcome.result)
		}
		seen[outcome.result.RunID] = true
		text := outcome.result.NewMessages[0].Content[0].Text
		want := "done:input-" + strings.TrimPrefix(outcome.result.RunID, "run-")
		if text != want {
			t.Fatalf("Run(%s) text = %q, want %q", outcome.result.RunID, text, want)
		}
	}
}

func TestAgentRunClassifiesCancellationAndDeadline(t *testing.T) {
	actionStarted := make(chan struct{}, 2)
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
		actionStarted <- struct{}{}
		<-r.Context().Done()
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := sdk.Run(ctx, reagent.RunRequest{Input: reagent.UserMessage("cancel")})
			done <- err
		}()
		<-actionStarted
		cancel()
		err := <-done
		if err == nil || reagent.ErrorCodeOf(err) != reagent.ErrorCodeCanceled || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled error = %v", err)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := sdk.Run(ctx, reagent.RunRequest{Input: reagent.UserMessage("deadline")})
		if err == nil || reagent.ErrorCodeOf(err) != reagent.ErrorCodeDeadlineExceeded || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline error = %v", err)
		}
	})
}

func TestCancelingOneRunDoesNotCancelAnother(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	sdk := newTestAgentWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(body.Tools) == 0 {
			writeOpenAIMessage(t, w, "plan")
			return
		}
		input := ""
		for _, message := range body.Messages {
			if message.Role == "user" && (message.Content == "cancel-one" || message.Content == "keep-running") {
				input = message.Content
			}
		}
		switch input {
		case "cancel-one":
			close(firstStarted)
			<-r.Context().Done()
		case "keep-running":
			close(secondStarted)
			<-releaseSecond
			writeOpenAIMessage(t, w, "second-done")
		default:
			t.Errorf("unexpected action input %q", input)
		}
	})

	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstDone := make(chan error, 1)
	go func() {
		_, err := sdk.Run(firstContext, reagent.RunRequest{RunID: "first", Input: reagent.UserMessage("cancel-one")})
		firstDone <- err
	}()
	<-firstStarted

	type outcome struct {
		result reagent.RunResult
		err    error
	}
	secondDone := make(chan outcome, 1)
	go func() {
		result, err := sdk.Run(context.Background(), reagent.RunRequest{RunID: "second", Input: reagent.UserMessage("keep-running")})
		secondDone <- outcome{result: result, err: err}
	}()
	<-secondStarted
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Run() error = %v", err)
	}
	close(releaseSecond)
	second := <-secondDone
	if second.err != nil || second.result.RunID != "second" || len(second.result.NewMessages) != 1 || second.result.NewMessages[0].Content[0].Text != "second-done" {
		t.Fatalf("second Run() = %#v, %v", second.result, second.err)
	}
}

func newTestAgentWithHandler(t *testing.T, handler http.HandlerFunc) *reagent.Agent {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(workDir, "skills", "test")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test\ndescription: Test behavior\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := &reagent.Config{
		CurrentPlatform: "test",
		Platforms: []reagent.PlatformConfig{{
			ID: "test", Protocol: reagent.ProtocolOpenAI, BaseURL: server.URL + "/v1/", APIKey: "key", Model: "model",
			Pricing: &reagent.PricingConfig{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		}},
	}
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	sdk, newErr := reagent.New(config)
	restoreErr := os.Chdir(originalDir)
	if newErr != nil {
		t.Fatal(newErr)
	}
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	t.Cleanup(func() { _ = sdk.Close(context.Background()) })
	return sdk
}

func writeOpenAIMessage(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion", "created": 1, "model": "model",
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
	}); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
