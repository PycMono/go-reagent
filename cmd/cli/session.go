package main

// session 持有进程内的多轮会话状态：pi.Runner 无状态，历史由 CLI 逐轮
// 累积并随请求回传。核心不变式：history 永远是完整的 user+assistant 对——
// 提取不到最终 assistant 纯文本的轮次（出错/取消/预算终止停在工具轮）
// 不提交历史，但用量照样累计。

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
)

type session struct {
	runner       pi.Runner
	listener     *terminalListener
	stderr       io.Writer
	limits       governor.Limits
	historyLimit int
	history      []pi.Message
	totals       governor.Totals
}

func newSession(runner pi.Runner, listener *terminalListener, stderr io.Writer, limits governor.Limits, historyLimit int) *session {
	return &session{
		runner:       runner,
		listener:     listener,
		stderr:       stderr,
		limits:       limits,
		historyLimit: historyLimit,
	}
}

// runTurn 执行一轮对话：REPL 与 -prompt 单轮共用。返回 runner 的运行错误
// （可能伴随有效的部分结果），供 -prompt 模式决定退出码。
func (s *session) runTurn(ctx context.Context, input string) error {
	result, err := s.runner.Run(ctx, pi.RunRequest{
		History: s.history,
		Input:   pi.Message{ContentType: "text", SenderType: "customer", Content: input},
		Limits:  s.limits,
	}, s.listener)
	s.listener.closeLine()
	s.commit(result, input)
	s.totals = addTotals(s.totals, result.Termination.Totals)
	s.trim()
	s.printTermination(result.Termination, err)
	return err
}

// commit 按"历史永远是完整对话对"的不变式提交：能提取到最终 assistant
// 纯文本才追加 user+assistant，否则本轮不写入历史。
func (s *session) commit(result pi.RunResult, input string) {
	replies := finalAssistantTexts(result.NewMessages)
	if len(replies) == 0 {
		if len(result.NewMessages) > 0 || len(result.Invocations) > 0 {
			fmt.Fprintln(s.stderr, "⚠ 本轮未产生最终回答，未写入历史；工具可能已修改工作区。")
		}
		return
	}
	s.history = append(s.history, pi.Message{ContentType: "text", SenderType: "customer", Content: input})
	for _, reply := range replies {
		s.history = append(s.history, pi.Message{ContentType: "text", SenderType: "ai", Content: reply})
	}
}

// finalAssistantTexts 提取 NewMessages 中可入历史的 assistant 纯文本，
// 口径与 conversation/mapper.go 一致：Role==assistant、无工具字段、
// 非错误、内容为纯文本。提取失败（如含非文本块）跳过该条。
func finalAssistantTexts(messages []ai.Message) []string {
	var replies []string
	for _, message := range messages {
		if message.Role != ai.RoleAssistant {
			continue
		}
		if len(message.ToolCalls) != 0 || message.ToolCallID != "" || message.ToolName != "" || message.IsError {
			continue
		}
		text, err := ai.TextContent(message.Content)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		replies = append(replies, text)
	}
	return replies
}

// trim 超出上限时从头部成对删除（user+assistant 一起删，不留孤儿消息；
// historyLimit 为奇数时实际保留向下取偶数条）。
func (s *session) trim() {
	limit := s.historyLimit - s.historyLimit%2
	for len(s.history) > limit && len(s.history) >= 2 {
		s.history = s.history[2:]
	}
}

func (s *session) reset() {
	s.history = nil
	s.totals = governor.Totals{}
}

func (s *session) printTermination(termination governor.Termination, err error) {
	if err != nil {
		fmt.Fprintf(s.stderr, "⚠ 运行结束：%v（终止原因: %s）\n", err, termination.Reason)
	} else if termination.Reason != "" && termination.Reason != governor.TerminationCompleted {
		fmt.Fprintf(s.stderr, "（终止原因: %s）\n", termination.Reason)
	}
	fmt.Fprintf(s.stderr, "用量：本轮 %d 次调用 / %d tokens；会话累计 %d 轮 / %d 次调用 / %d tokens / $%.4f\n",
		termination.Totals.Invocations, termination.Totals.TotalTokens,
		s.totals.Turns, s.totals.Invocations, s.totals.TotalTokens, s.totals.CostUSD)
}

func addTotals(a, b governor.Totals) governor.Totals {
	return governor.Totals{
		Turns:        a.Turns + b.Turns,
		Invocations:  a.Invocations + b.Invocations,
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		TotalTokens:  a.TotalTokens + b.TotalTokens,
		CostUSD:      a.CostUSD + b.CostUSD,
	}
}
