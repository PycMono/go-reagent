package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
)

// fakeRunner 按队列返回预设结果，并像真实 pi 一样通过 listener 发流式事件。
type fakeRunner struct {
	results  []pi.RunResult
	errs     []error
	requests []pi.RunRequest
}

func (f *fakeRunner) Run(_ context.Context, request pi.RunRequest, listener pi.EventListener) (pi.RunResult, error) {
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	var result pi.RunResult
	var err error
	if index < len(f.results) {
		result = f.results[index]
	}
	if index < len(f.errs) {
		err = f.errs[index]
	}
	for _, text := range finalAssistantTexts(result.NewMessages) {
		listener.OnEvent(context.Background(), pi.NewMessageStartEvent())
		listener.OnEvent(context.Background(), pi.NewMessageUpdateEvent(ai.TextBlock(text)))
		listener.OnEvent(context.Background(), pi.NewMessageEndEvent(ai.Message{}))
	}
	return result, err
}

func assistantText(text string) ai.Message {
	return ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock(text)}}
}

func newTestSession(runner pi.Runner, historyLimit int) (*session, *bytes.Buffer) {
	stderr := &bytes.Buffer{}
	listener := newTerminalListener(&bytes.Buffer{}, &bytes.Buffer{}, false)
	return newSession(runner, listener, stderr, governor.Limits{}, historyLimit), stderr
}

func TestRunTurnCommitsCompletePair(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{{
		NewMessages: []ai.Message{assistantText("回答一")},
		Termination: governor.Termination{Reason: governor.TerminationCompleted,
			Totals: governor.Totals{Turns: 1, Invocations: 1, TotalTokens: 10}},
	}}}
	s, _ := newTestSession(runner, 100)

	s.runTurn(context.Background(), "问题一")

	if len(s.history) != 2 || s.history[0].Content != "问题一" || s.history[0].SenderType != "customer" ||
		s.history[1].Content != "回答一" || s.history[1].SenderType != "ai" {
		t.Fatalf("history = %#v", s.history)
	}
	if s.totals.TotalTokens != 10 || s.totals.Invocations != 1 {
		t.Fatalf("totals = %#v", s.totals)
	}
}

func TestRunTurnSkipsToolMessages(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{{
		NewMessages: []ai.Message{
			{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("我来读文件")},
				ToolCalls: []ai.ToolCall{{ID: "1", Name: "read"}}},
			{Role: ai.RoleTool, ToolCallID: "1", ToolName: "read",
				Content: []ai.ContentBlock{ai.TextBlock("file content")}},
			assistantText("最终回答"),
		},
	}}}
	s, _ := newTestSession(runner, 100)
	s.runTurn(context.Background(), "读一下")

	if len(s.history) != 2 || s.history[1].Content != "最终回答" {
		t.Fatalf("工具消息不应入历史, history = %#v", s.history)
	}
}

func TestRunTurnErrorWithoutFinalTextDoesNotCommit(t *testing.T) {
	runner := &fakeRunner{
		results: []pi.RunResult{{
			// 有 Invocation、无可提交文本：费用照算，历史不提交。
			NewMessages: []ai.Message{{
				Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("中间话")},
				ToolCalls: []ai.ToolCall{{ID: "1", Name: "exec"}},
			}},
			Invocations: []governor.Invocation{{Sequence: 1}},
			Termination: governor.Termination{Reason: governor.TerminationCanceled,
				Totals: governor.Totals{Invocations: 1, TotalTokens: 5, CostUSD: 0.01}},
		}},
		errs: []error{context.Canceled},
	}
	s, stderr := newTestSession(runner, 100)

	s.runTurn(context.Background(), "跑个任务")

	if len(s.history) != 0 {
		t.Fatalf("无最终文本不应提交历史, history = %#v", s.history)
	}
	if s.totals.TotalTokens != 5 || s.totals.CostUSD != 0.01 {
		t.Fatalf("取消轮用量必须累计, totals = %#v", s.totals)
	}
	if !strings.Contains(stderr.String(), "未产生最终回答") {
		t.Fatalf("应提示未提交, stderr = %q", stderr.String())
	}
}

func TestRunTurnErrorWithFinalTextStillCommits(t *testing.T) {
	// 预算触顶但已拿到最终回答：提交 + 累计 + 提示 err。
	runner := &fakeRunner{
		results: []pi.RunResult{{
			NewMessages: []ai.Message{assistantText("触顶前的完整回答")},
			Termination: governor.Termination{Reason: governor.TerminationMaxTurns},
		}},
		errs: []error{errors.New("max turns reached")},
	}
	s, stderr := newTestSession(runner, 100)
	s.runTurn(context.Background(), "问题")

	if len(s.history) != 2 {
		t.Fatalf("已有最终回答应提交, history = %#v", s.history)
	}
	if !strings.Contains(stderr.String(), "max turns reached") {
		t.Fatalf("应提示 err, stderr = %q", stderr.String())
	}
}

func TestTrimRemovesPairs(t *testing.T) {
	s, _ := newTestSession(&fakeRunner{}, 4)
	for _, text := range []string{"q1", "q2", "q3"} {
		s.history = append(s.history,
			pi.Message{ContentType: "text", SenderType: "customer", Content: text},
			pi.Message{ContentType: "text", SenderType: "ai", Content: "a-" + text})
	}
	s.trim()
	if len(s.history) != 4 || s.history[0].Content != "q2" {
		t.Fatalf("成对裁剪后 history = %#v", s.history)
	}

	// 奇数上限向下取偶。
	s2, _ := newTestSession(&fakeRunner{}, 3)
	s2.history = append(s2.history,
		pi.Message{ContentType: "text", SenderType: "customer", Content: "q1"},
		pi.Message{ContentType: "text", SenderType: "ai", Content: "a1"},
		pi.Message{ContentType: "text", SenderType: "customer", Content: "q2"},
		pi.Message{ContentType: "text", SenderType: "ai", Content: "a2"})
	s2.trim()
	if len(s2.history) != 2 || s2.history[0].Content != "q2" {
		t.Fatalf("奇数上限应向下取偶, history = %#v", s2.history)
	}
}

func TestSecondTurnCarriesHistory(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{
		{NewMessages: []ai.Message{assistantText("回答一")}},
		{NewMessages: []ai.Message{assistantText("回答二")}},
	}}
	s, _ := newTestSession(runner, 100)
	s.runTurn(context.Background(), "问题一")
	s.runTurn(context.Background(), "问题二")

	second := runner.requests[1]
	if len(second.History) != 2 || second.History[0].Content != "问题一" || second.History[1].Content != "回答一" {
		t.Fatalf("第二轮应携带第一轮历史, History = %#v", second.History)
	}
}

func TestResetClearsHistoryAndTotals(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{{
		NewMessages: []ai.Message{assistantText("a")},
		Termination: governor.Termination{Totals: governor.Totals{TotalTokens: 7}},
	}}}
	s, _ := newTestSession(runner, 100)
	s.runTurn(context.Background(), "q")
	s.reset()
	if len(s.history) != 0 || s.totals.TotalTokens != 0 {
		t.Fatalf("reset 后应清空, history=%v totals=%v", s.history, s.totals)
	}
}
