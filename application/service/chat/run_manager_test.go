package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/conversation"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type controllableRunner struct {
	mu       sync.Mutex
	requests []conversation.RunRequest
	started  chan struct{}
	release  chan error
	canceled chan struct{}
}

func (r *controllableRunner) Run(ctx context.Context, request conversation.RunRequest, reporter pi.Reporter) (pi.RunResult, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	if r.started != nil {
		r.started <- struct{}{}
	}
	select {
	case err := <-r.release:
		if err == nil {
			reporter.Report(ctx, pi.NewMessageStartEvent())
			reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("answer")))
			reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}}))
		}
		return pi.RunResult{}, err
	case <-ctx.Done():
		if r.canceled != nil {
			close(r.canceled)
		}
		return pi.RunResult{}, ctx.Err()
	}
}

type runRepoFake struct {
	managementRepoFake
	found        bool
	foundValue   *conversationentity.Conversation
	findCalls    int
	renameTitles []string
}

func (f *runRepoFake) FindByUserIDAndConversationID(_ context.Context, userID, conversationID string) (*conversationentity.Conversation, bool, error) {
	f.findCalls++
	if f.operationErr != nil {
		return nil, false, f.operationErr
	}
	return f.foundValue, f.found, nil
}

func (f *runRepoFake) RenameIfUntitled(_ context.Context, userID, conversationID, title string) error {
	f.renameTitles = append(f.renameTitles, strings.Join([]string{userID, conversationID, title}, ","))
	return f.operationErr
}

func newRunService(repo *runRepoFake, runner conversation.Runner, ids ...string) *Service {
	return NewService(repo, &idFake{values: ids}, runner, testCatalog())
}

func receiveUntilTerminal(t *testing.T, events <-chan vo.RunEventVO) []vo.RunEventVO {
	t.Helper()
	var got []vo.RunEventVO
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, event)
		case <-timer.C:
			t.Fatal("run events did not close")
		}
	}
}

func TestRunLifecycleUsesOwnedConversationAndRenamesOnSuccess(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat-1", ProfileCode: "general"}}
	runner := &controllableRunner{started: make(chan struct{}, 1), release: make(chan error, 1)}
	service := newRunService(repo, runner, "run-1")
	run, err := service.StartRun(context.Background(), "visitor-1", "chat-1", dto.StartRunDTO{Content: "  A title for this chat  "})
	if err != nil {
		t.Fatal(err)
	}
	if event := <-run.Events; event.Type != vo.RunEventRunStarted || event.RunID != "run-1" {
		t.Fatalf("first event = %#v", event)
	}
	<-runner.started
	runner.release <- nil
	events := receiveUntilTerminal(t, run.Events)
	if len(events) != 4 || events[0].Type != vo.RunEventMessageStarted || events[1].Type != vo.RunEventMessageDelta ||
		events[2].Type != vo.RunEventMessageCompleted || events[3].Type != vo.RunEventRunCompleted {
		t.Fatalf("events = %#v", events)
	}
	runner.mu.Lock()
	request := runner.requests[0]
	runner.mu.Unlock()
	text, textErr := ai.TextContent(request.Input.Content)
	if textErr != nil || request.UserID != "visitor-1" || request.ConversationID != "chat-1" || request.RunID != "run-1" ||
		request.Input.Role != ai.RoleUser || text != "A title for this chat" {
		t.Fatalf("request = %#v, text = %q, err = %v", request, text, textErr)
	}
	if len(repo.renameTitles) != 1 || repo.renameTitles[0] != "visitor-1,chat-1,A title for this chat" {
		t.Fatalf("titles = %#v", repo.renameTitles)
	}
}

func TestRunUsesPersistedProfileContextAndSkills(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat-1", ProfileCode: "writing"}}
	runner := &controllableRunner{started: make(chan struct{}, 1), release: make(chan error, 1)}
	service := newRunService(repo, runner, "run-profile")
	run, err := service.StartRun(context.Background(), "visitor", "chat-1", dto.StartRunDTO{Content: "写一篇文章"})
	if err != nil {
		t.Fatal(err)
	}
	<-run.Events
	<-runner.started
	runner.release <- nil
	receiveUntilTerminal(t, run.Events)

	runner.mu.Lock()
	request := runner.requests[0]
	runner.mu.Unlock()
	if len(request.Context) != 2 || request.Context[0].Name != "agent-profile" || request.Context[0].Priority != 900 ||
		!strings.Contains(request.Context[0].Content, "擅长写作") || request.Context[1].Name != "agent-profile-skills" ||
		request.Context[1].Priority != 800 || !strings.Contains(request.Context[1].Content, "profiles/writing/skills/content-writing/SKILL.md") ||
		!strings.Contains(request.Context[1].Content, "Write content &lt;carefully&gt;.") || strings.Contains(request.Context[1].Content, "guided-learning") {
		t.Fatalf("Profile Context = %#v", request.Context)
	}
}

func TestRunAllowsUnselectablePersistedProfile(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat", ProfileCode: "retired"}}
	runner := &controllableRunner{release: make(chan error, 1)}
	service := newRunService(repo, runner, "run-retired")
	run, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	<-run.Events
	runner.release <- nil
	receiveUntilTerminal(t, run.Events)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.requests[0].Context) != 2 || !strings.Contains(runner.requests[0].Context[0].Content, "旧助手规则") {
		t.Fatalf("Context = %#v", runner.requests[0].Context)
	}
}

func TestRunRejectsPersistedProfileMissingFromCatalog(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat", ProfileCode: "removed"}}
	service := newRunService(repo, &controllableRunner{}, "unused-run")
	_, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "hello"})
	if !errors.Is(err, commonerrors.ErrInternal) {
		t.Fatalf("StartRun() error = %v, want ErrInternal", err)
	}
	if len(service.active) != 0 {
		t.Fatalf("missing Profile created active run: %#v", service.active)
	}
}

func TestRunRejectsUnownedAndDuplicateConversation(t *testing.T) {
	repo := &runRepoFake{}
	service := newRunService(repo, &controllableRunner{}, "run-1")
	if _, err := service.StartRun(context.Background(), "visitor", "missing", dto.StartRunDTO{Content: "hello"}); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("unowned error = %v", err)
	}
	repo.found = true
	repo.foundValue = &conversationentity.Conversation{ConversationID: "chat", ProfileCode: "general"}
	runner := &controllableRunner{release: make(chan error)}
	service = newRunService(repo, runner, "run-2", "run-3")
	first, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "two"}); !errors.Is(err, commonerrors.ErrConflict) {
		t.Fatalf("duplicate error = %v", err)
	}
	if repo.findCalls != 3 {
		t.Fatalf("ownership checks = %d, want 3", repo.findCalls)
	}
	_ = service.CancelRun(context.Background(), "visitor", "chat", first.ID)
	receiveUntilTerminal(t, first.Events)
}

func TestCancelRunChecksRunIdentityAndCancelsRunner(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat", ProfileCode: "general"}}
	runner := &controllableRunner{started: make(chan struct{}, 1), release: make(chan error), canceled: make(chan struct{})}
	service := newRunService(repo, runner, "run-1")
	run, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-run.Events
	<-runner.started
	if err := service.CancelRun(context.Background(), "other", "chat", run.ID); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("wrong owner error = %v", err)
	}
	if err := service.CancelRun(context.Background(), "visitor", "chat", "other-run"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatalf("wrong run error = %v", err)
	}
	if err := service.CancelRun(context.Background(), "visitor", "chat", run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.canceled:
	case <-time.After(time.Second):
		t.Fatal("runner did not observe cancellation")
	}
	events := receiveUntilTerminal(t, run.Events)
	if len(events) != 1 || events[0].Type != vo.RunEventRunFailed {
		t.Fatalf("events = %#v", events)
	}
}

func TestRunFailureEmitsOnceAndReleasesSlot(t *testing.T) {
	repo := &runRepoFake{found: true, foundValue: &conversationentity.Conversation{ConversationID: "chat", ProfileCode: "general"}}
	runner := &controllableRunner{release: make(chan error, 2)}
	service := newRunService(repo, runner, "run-1", "run-2")
	run, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-run.Events
	runner.release <- errors.New("model failed")
	events := receiveUntilTerminal(t, run.Events)
	if len(events) != 1 || events[0].Type != vo.RunEventRunFailed || events[0].Error == nil {
		t.Fatalf("events = %#v", events)
	}
	second, err := service.StartRun(context.Background(), "visitor", "chat", dto.StartRunDTO{Content: "again"})
	if err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
	<-second.Events
	runner.release <- nil
	receiveUntilTerminal(t, second.Events)
}
