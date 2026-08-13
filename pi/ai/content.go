package ai

import (
	"fmt"
	"strings"
)

// ContentType 表示消息内容块的类型。
type ContentType string

// ContentTypeText 表示纯文本内容块。
const ContentTypeText ContentType = "text"

// ContentBlock 表示消息中的一个内容块。
type ContentBlock struct {
	// Type 表示内容块的类型。
	Type ContentType `json:"type"`
	// Text 保存文本内容。
	Text string `json:"text"`
}

// TextBlock 创建一个纯文本内容块。
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

// TextContent 按顺序拼接内容块中的文本；遇到非文本内容块时返回错误。
func TextContent(blocks []ContentBlock) (string, error) {
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type != ContentTypeText {
			return "", fmt.Errorf("unsupported content type %q", block.Type)
		}
		builder.WriteString(block.Text)
	}
	return builder.String(), nil
}
