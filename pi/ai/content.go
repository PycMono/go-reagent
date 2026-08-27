package ai

import (
	"fmt"
	"net/url"
	"strings"
)

// ContentType 表示消息内容块的类型。
type ContentType string

// ContentTypeText 表示纯文本内容块。
const ContentTypeText ContentType = "text"

// ContentTypeImage 表示 URL 图像内容块。
const ContentTypeImage ContentType = "image"

// ContentBlock 表示消息中的一个内容块。联合类型取值受 Validate 约束：
// text 块不得携带 Image，image 块只携带 Image 不携带 Text。
type ContentBlock struct {
	// Type 表示内容块的类型。
	Type ContentType `json:"type"`
	// Text 保存文本内容。
	Text string `json:"text,omitempty"`
	// Image 保存 URL 图像内容；仅 Type 为 ContentTypeImage 时非空。
	Image *ImageContent `json:"image,omitempty"`
}

// ImageContent 表示一个 URL 图像内容。
type ImageContent struct {
	// URL 是图像的可访问地址；调用方必须保证推理服务商可访问且生命周期足够长。
	URL string `json:"url"`
}

// TextBlock 创建一个纯文本内容块。
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

// ImageBlock 创建一个 URL 图像内容块。
func ImageBlock(imageURL string) ContentBlock {
	return ContentBlock{Type: ContentTypeImage, Image: &ImageContent{URL: imageURL}}
}

// Validate 校验内容块的联合类型取值：text 块不得携带 Image，image 块必须
// 只携带合法 URL 的 Image，未知类型报错。校验集中在入口边界复用本函数，
// 不散落到使用方。
func (block ContentBlock) Validate() error {
	switch block.Type {
	case ContentTypeText:
		if block.Image != nil {
			return fmt.Errorf("text block must not carry an image")
		}
	case ContentTypeImage:
		if block.Text != "" {
			return fmt.Errorf("image block must not carry text")
		}
		if block.Image == nil {
			return fmt.Errorf("image block requires image content")
		}
		if err := validateImageURL(block.Image.URL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported content type %q", block.Type)
	}
	return nil
}

func validateImageURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("image url %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("image url %q must use http or https", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("image url %q requires a host", raw)
	}
	return nil
}

// ImagePlaceholderText 生成图像块的脱敏占位文本：只保留 scheme、host 与
// path，剥离查询参数与片段，避免签名、临时 Token 泄漏到模型上下文。降级
// 占位与压缩摘要投影共用本函数。
func ImagePlaceholderText(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "[图片]"
	}
	brief := parsed.Scheme + "://" + parsed.Host + parsed.Path
	if parsed.Path == "" || parsed.Path == "/" {
		brief = parsed.Scheme + "://" + parsed.Host
	}
	return "[图片: " + brief + "]"
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