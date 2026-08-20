package chat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-context-sdk/bizctx"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/conversation"
	agentprofileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/gin-gonic/gin"
)

type controllerIDs struct {
	mu     sync.Mutex
	values []string
}

type controllerCatalog struct{}

func (controllerCatalog) List() []agentprofileentity.Profile {
	return []agentprofileentity.Profile{
		{Code: "general", Name: "通用助手", Description: "日常问答", Icon: "message-circle", Selectable: true, Welcome: "今天想一起完成什么？", Instructions: "hidden"},
		{Code: "writing", Name: "写作助手", Description: "内容写作", Icon: "pen-line", Selectable: true, Welcome: "想写点什么？"},
	}
}

func (controllerCatalog) Find(code string) (agentprofileentity.Profile, bool) {
	for _, profile := range (controllerCatalog{}).List() {
		if profile.Code == strings.TrimSpace(code) {
			return profile, true
		}
	}
	return agentprofileentity.Profile{}, false
}

func (controllerCatalog) DefaultCode() string { return "general" }

func (f *controllerIDs) NextID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := f.values[0]
	f.values = f.values[1:]
	return value
}

type controllerRepo struct {
	created      *conversationentity.Conversation
	found        bool
	listQuery    conversationrepo.ListQuery
	listPage     conversationrepo.ListPage
	messageQuery conversationrepo.MessageQuery
	messagePage  conversationrepo.MessagePage
	rename       []string
	deleted      []string
	deleteErr    error
}

func (f *controllerRepo) Create(_ context.Context, value *conversationentity.Conversation) error {
	f.created = value
	return nil
}
func (f *controllerRepo) FindByUserIDAndConversationID(_ context.Context, _, _ string) (*conversationentity.Conversation, bool, error) {
	return &conversationentity.Conversation{ConversationID: "chat-1", ProfileCode: "general"}, f.found, nil
}
func (f *controllerRepo) ListByUserID(_ context.Context, query conversationrepo.ListQuery) (conversationrepo.ListPage, error) {
	f.listQuery = query
	return f.listPage, nil
}
func (f *controllerRepo) ListMessages(_ context.Context, query conversationrepo.MessageQuery) (conversationrepo.MessagePage, error) {
	f.messageQuery = query
	return f.messagePage, nil
}
func (f *controllerRepo) Rename(_ context.Context, userID, conversationID, name string) error {
	f.rename = []string{userID, conversationID, name}
	return nil
}
func (f *controllerRepo) RenameIfUntitled(context.Context, string, string, string) error { return nil }
func (f *controllerRepo) Delete(_ context.Context, userID, conversationID string) error {
	f.deleted = []string{userID, conversationID}
	return f.deleteErr
}

type controllerRunner func(context.Context, conversation.RunRequest, pi.Reporter) (pi.RunResult, error)

func (f controllerRunner) Run(ctx context.Context, request conversation.RunRequest, reporter pi.Reporter) (pi.RunResult, error) {
	return f(ctx, request, reporter)
}

func testRouter(service *chatservice.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(bizctx.WithKV(c.Request.Context(), bizctx.UserID("visitor-1")))
		c.Next()
	})
	ctl := NewController(service)
	router.GET("/api/v1/agent-profiles", ctl.ListAgentProfiles)
	routes := router.Group("/api/v1/conversations")
	routes.POST("", ctl.CreateConversation)
	routes.GET("", ctl.ListConversations)
	routes.PATCH("/:id", ctl.RenameConversation)
	routes.DELETE("/:id", ctl.DeleteConversation)
	routes.GET("/:id/messages", ctl.ListMessages)
	routes.POST("/:id/runs", ctl.StartRun)
	routes.POST("/:id/runs/:run_id/cancel", ctl.CancelRun)
	return router
}

func TestControllerCRUDUsesBusinessContextIdentity(t *testing.T) {
	repo := &controllerRepo{found: true, deleteErr: commonerrors.ErrNotFound}
	service := chatservice.NewService(repo, &controllerIDs{values: []string{"internal-1", "chat-1"}}, nil, controllerCatalog{})
	router := testRouter(service)

	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{"profile_code":"writing"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(create, createRequest)
	if create.Code != http.StatusOK || repo.created == nil || repo.created.UserID != "visitor-1" || repo.created.ProfileCode != "writing" || !strings.Contains(create.Body.String(), `"profile_code":"writing"`) {
		t.Fatalf("create status/repo = %d / %#v", create.Code, repo.created)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/conversations?limit=5&keyword=docs&profile_code=general", nil))
	if list.Code != http.StatusOK || repo.listQuery.UserID != "visitor-1" || repo.listQuery.Limit != 5 || repo.listQuery.Keyword != "docs" || repo.listQuery.ProfileCode != "general" {
		t.Fatalf("list status/query = %d / %#v", list.Code, repo.listQuery)
	}

	messages := httptest.NewRecorder()
	router.ServeHTTP(messages, httptest.NewRequest(http.MethodGet, "/api/v1/conversations/chat-1/messages?limit=8", nil))
	if messages.Code != http.StatusOK || repo.messageQuery.UserID != "visitor-1" || repo.messageQuery.ConversationID != "chat-1" || repo.messageQuery.Limit != 8 {
		t.Fatalf("messages status/query = %d / %#v", messages.Code, repo.messageQuery)
	}

	rename := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/chat-1", strings.NewReader(`{"name":"New"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rename, request)
	if rename.Code != http.StatusOK || strings.Join(repo.rename, ",") != "visitor-1,chat-1,New" {
		t.Fatalf("rename status/args = %d / %#v", rename.Code, repo.rename)
	}

	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/chat-1", nil))
	if deleteResponse.Code != http.StatusNotFound || strings.Join(repo.deleted, ",") != "visitor-1,chat-1" {
		t.Fatalf("delete status/args = %d / %#v", deleteResponse.Code, repo.deleted)
	}
}

func TestControllerListsPublicAgentProfiles(t *testing.T) {
	service := chatservice.NewService(&controllerRepo{}, &controllerIDs{}, nil, controllerCatalog{})
	response := httptest.NewRecorder()
	testRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/agent-profiles", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"default_profile":"general"`) ||
		!strings.Contains(response.Body.String(), `"code":"writing"`) || strings.Contains(response.Body.String(), "hidden") {
		t.Fatalf("profile response = %d / %s", response.Code, response.Body.String())
	}
}

func TestControllerRejectsMalformedInput(t *testing.T) {
	repo := &controllerRepo{}
	router := testRouter(chatservice.NewService(repo, &controllerIDs{}, nil, controllerCatalog{}))
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/chat-1", strings.NewReader(`{"name":`)),
		httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/chat-1", strings.NewReader(`{"name":""}`)),
		httptest.NewRequest(http.MethodGet, "/api/v1/conversations?limit=101", nil),
	} {
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, body=%s", request.URL, response.Code, response.Body.String())
		}
	}
}

func TestStartRunStreamsNamedSSEEvents(t *testing.T) {
	repo := &controllerRepo{found: true}
	runner := controllerRunner(func(ctx context.Context, _ conversation.RunRequest, reporter pi.Reporter) (pi.RunResult, error) {
		reporter.Report(ctx, pi.NewThinkingEvent())
		reporter.Report(ctx, pi.NewMessageStartEvent())
		reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("do")))
		reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("ne")))
		reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}))
		return pi.RunResult{}, nil
	})
	service := chatservice.NewService(repo, &controllerIDs{values: []string{"run-1"}}, runner, controllerCatalog{})
	router := testRouter(service)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/chat-1/runs", bytes.NewBufferString(`{"content":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" ||
		response.Header().Get("Cache-Control") != "no-cache" || response.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("status/headers = %d / %#v", response.Code, response.Header())
	}
	body := response.Body.String()
	events := []string{"run.started", "agent.thinking", "message.started", "message.delta", "message.completed", "run.completed"}
	last := -1
	for _, event := range events {
		if !strings.Contains(body, "event: "+event+"\n") || !strings.Contains(body, "data: {") {
			t.Fatalf("missing %q in SSE body:\n%s", event, body)
		}
		index := strings.Index(body[last+1:], "event: "+event+"\n")
		if index < 0 {
			t.Fatalf("event %q is out of order in SSE body:\n%s", event, body)
		}
		last += index + 1
	}
	if strings.Count(body, "event: message.delta\n") != 2 || !strings.Contains(body, `"run_id":"run-1"`) ||
		!strings.Contains(body, `"delta":{"type":"text","text":"do"}`) || !strings.Contains(body, `"text":"done"`) {
		t.Fatalf("SSE data = %s", body)
	}
}

func TestStartRunDoesNotExposeSkillReadsOrReadContents(t *testing.T) {
	repo := &controllerRepo{found: true}
	runner := controllerRunner(func(ctx context.Context, _ conversation.RunRequest, reporter pi.Reporter) (pi.RunResult, error) {
		skillCall := ai.ToolCall{
			ID: "call-skill", Name: "read", Arguments: []byte(`{"path":"skills/writing-assistance/SKILL.md"}`),
		}
		reporter.Report(ctx, pi.NewMessageStartEvent())
		reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{skillCall}}))
		reporter.Report(ctx, pi.NewToolStartEvent(skillCall))
		reporter.Report(ctx, pi.NewToolEndEvent(skillCall, pi.ToolResult{
			ToolCallID: skillCall.ID, ToolName: skillCall.Name,
			Content: []ai.ContentBlock{ai.TextBlock("private skill instructions")},
		}))

		fileCall := ai.ToolCall{ID: "call-file", Name: "read", Arguments: []byte(`{"path":"README.md"}`)}
		reporter.Report(ctx, pi.NewMessageStartEvent())
		reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{fileCall}}))
		reporter.Report(ctx, pi.NewToolStartEvent(fileCall))
		reporter.Report(ctx, pi.NewToolUpdateEvent(fileCall, ai.ToolUpdate{
			Content: []ai.ContentBlock{ai.TextBlock("private streamed file body")}, Details: "private details",
		}))
		reporter.Report(ctx, pi.NewToolEndEvent(fileCall, pi.ToolResult{
			ToolCallID: fileCall.ID, ToolName: fileCall.Name,
			Content: []ai.ContentBlock{ai.TextBlock("private file body")},
		}))
		return pi.RunResult{}, nil
	})
	service := chatservice.NewService(repo, &controllerIDs{values: []string{"run-1"}}, runner, controllerCatalog{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/chat-1/runs", bytes.NewBufferString(`{"content":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	testRouter(service).ServeHTTP(response, request)

	body := response.Body.String()
	for _, forbidden := range []string{"SKILL.md", "private skill instructions", "private streamed file body", "private details", "private file body"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("SSE body exposed %q:\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"path":"README.md"`) || !strings.Contains(body, "event: tool.updated\n") ||
		!strings.Contains(body, "event: tool.completed\n") {
		t.Fatalf("SSE body lost ordinary read metadata:\n%s", body)
	}
}

func TestCancelRunForwardsPathAndBusinessIdentity(t *testing.T) {
	repo := &controllerRepo{found: true}
	started := make(chan struct{})
	canceled := make(chan struct{})
	runner := controllerRunner(func(ctx context.Context, request conversation.RunRequest, _ pi.Reporter) (pi.RunResult, error) {
		if request.UserID != "visitor-1" || request.ConversationID != "chat-1" || request.RunID != "run-1" {
			t.Errorf("request = %#v", request)
		}
		close(started)
		<-ctx.Done()
		close(canceled)
		return pi.RunResult{}, ctx.Err()
	})
	service := chatservice.NewService(repo, &controllerIDs{values: []string{"run-1"}}, runner, controllerCatalog{})
	router := testRouter(service)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/chat-1/runs", bytes.NewBufferString(`{"content":"hello"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run did not start")
	}
	cancelResponse := httptest.NewRecorder()
	router.ServeHTTP(cancelResponse, httptest.NewRequest(http.MethodPost, "/api/v1/conversations/chat-1/runs/run-1/cancel", nil))
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel status/body = %d / %s", cancelResponse.Code, cancelResponse.Body.String())
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("runner was not canceled")
	}
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not finish")
	}
}
