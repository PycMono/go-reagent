package schema

import (
	"fmt"
	"strings"
)

type ContentType string

const ContentTypeText ContentType = "text"

type ContentBlock struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

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
