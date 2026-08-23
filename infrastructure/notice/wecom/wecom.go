// Package wecom 是企业微信群机器人通知通道，实现 pi.Notifier。
// 群机器人是无 SDK 的 webhook 模型：POST markdown 到带 key 的 URL。
package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi"
)

const (
	markdownMaxBytes = 4096 // 企业微信 markdown 正文上限
	truncationMarker = "... (已截断)"
)

type Notifier struct {
	webhookURL string
	client     *http.Client
}

// New 创建企业微信群机器人通道。webhookURL 的合法性由 config.Load 保证
// （HTTPS 绝对 URL），这里不再校验。
func New(webhookURL string, client *http.Client) *Notifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Notifier{webhookURL: webhookURL, client: client}
}

// Notify 实现 pi.Notifier：把最终回复以 markdown 发到群机器人。
// 通知是旁路：失败只记日志，不重试、不影响 run。
func (n *Notifier) Notify(ctx context.Context, notification pi.Notification) {
	if err := n.send(ctx, truncateUTF8(notification.Text, markdownMaxBytes)); err != nil {
		logsdk.Error(ctx, "企业微信群通知发送失败",
			logsdk.Any("component", "wecom_notifier"),
			logsdk.Err(err),
		)
	}
}

func (n *Notifier) send(ctx context.Context, content string) error {
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.client.Do(request)
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

// truncateUTF8 按字节截断且不切断多字节字符。
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
