package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/engine"
)

const (
	weComMarkdownMaxBytes = 4096
	truncationMarker      = "... (已截断)"
)

// WeComReporter sends Agent lifecycle events to an enterprise WeChat group robot.
type WeComReporter struct {
	webhookURL string
	client     *http.Client
}

// NewWeComReporter creates an outbound-only enterprise WeChat Reporter.
func NewWeComReporter(webhookURL string, client *http.Client) (*WeComReporter, error) {
	webhookURL = strings.TrimSpace(webhookURL)
	parsed, err := url.Parse(webhookURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("wecom reporter: webhook URL must be an absolute HTTP/HTTPS URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &WeComReporter{webhookURL: webhookURL, client: client}, nil
}

func (r *WeComReporter) OnThinking(ctx context.Context) {
	r.send(ctx, "🤔 模型正在慢思考 (Thinking)...")
}

func (r *WeComReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	r.send(ctx, fmt.Sprintf("🛠️ **正在执行工具**：`%s`\n参数：`%s`", toolName, args))
}

func (r *WeComReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	if isError {
		r.send(ctx, fmt.Sprintf("⚠️ **执行报错** (%s)：\n%s", toolName, result))
		return
	}
	r.send(ctx, fmt.Sprintf("✅ **执行成功** (%s)", toolName))
}

func (r *WeComReporter) OnMessage(ctx context.Context, content string) {
	r.send(ctx, content)
}

func (r *WeComReporter) send(ctx context.Context, content string) {
	if err := r.sendMarkdown(ctx, truncateUTF8(content, weComMarkdownMaxBytes)); err != nil {
		logsdk.Error(ctx, "企业微信群通知发送失败",
			logsdk.Any("component", "wecom_reporter"),
			logsdk.Err(err),
		)
	}
}

func (r *WeComReporter) sendMarkdown(ctx context.Context, content string) error {
	payload := struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}{MsgType: "markdown"}
	payload.Markdown.Content = content

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}

	var result struct {
		ErrorCode int    `json:"errcode"`
		ErrorMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if result.ErrorCode != 0 {
		return fmt.Errorf("wecom error %d: %s", result.ErrorCode, result.ErrorMsg)
	}
	return nil
}

func truncateUTF8(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}
	limit := maxBytes - len(truncationMarker)
	if limit <= 0 {
		return truncationMarker[:maxBytes]
	}
	for limit > 0 && !utf8.RuneStart(content[limit]) {
		limit--
	}
	return content[:limit] + truncationMarker
}

var _ engine.Reporter = (*WeComReporter)(nil)
