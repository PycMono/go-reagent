package ai

import "testing"

func TestImageBlock(t *testing.T) {
	block := ImageBlock("https://example.com/a.png")
	if block.Type != ContentTypeImage {
		t.Fatalf("type = %q, want image", block.Type)
	}
	if block.Image == nil || block.Image.URL != "https://example.com/a.png" {
		t.Fatalf("image = %+v, want URL https://example.com/a.png", block.Image)
	}
	if block.Text != "" {
		t.Fatalf("text = %q, want empty", block.Text)
	}
	if err := block.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestContentBlockValidate(t *testing.T) {
	tests := []struct {
		name    string
		block   ContentBlock
		wantErr bool
	}{
		{name: "valid text", block: TextBlock("hi")},
		{name: "valid image", block: ImageBlock("https://example.com/a.png")},
		{
			name:    "text carries image",
			block:   ContentBlock{Type: ContentTypeText, Text: "hi", Image: &ImageContent{URL: "https://example.com/a.png"}},
			wantErr: true,
		},
		{
			name:    "image carries text",
			block:   ContentBlock{Type: ContentTypeImage, Text: "txt", Image: &ImageContent{URL: "https://example.com/a.png"}},
			wantErr: true,
		},
		{
			name:    "image without content",
			block:   ContentBlock{Type: ContentTypeImage},
			wantErr: true,
		},
		{
			name:    "ftp scheme",
			block:   ImageBlock("ftp://example.com/a.png"),
			wantErr: true,
		},
		{
			name:    "file scheme",
			block:   ImageBlock("file:///tmp/a.png"),
			wantErr: true,
		},
		{
			name:    "no host",
			block:   ImageBlock("https:///a.png"),
			wantErr: true,
		},
		{
			name:    "empty url",
			block:   ImageBlock(""),
			wantErr: true,
		},
		{
			name:    "unknown type",
			block:   ContentBlock{Type: "audio", Text: "hi"},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.block.Validate()
			if test.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTextContentRejectsImage(t *testing.T) {
	blocks := []ContentBlock{TextBlock("a"), ImageBlock("https://example.com/a.png")}
	if _, err := TextContent(blocks); err == nil {
		t.Fatalf("expected error for image block, got nil")
	}
}