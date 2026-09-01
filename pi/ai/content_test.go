package ai

import "testing"

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

func TestContentBlocksCloneDeepCopiesImage(t *testing.T) {
	blocks := ContentBlocks{TextBlock("a"), ImageBlock("https://example.com/a.png")}
	cloned := blocks.Clone()
	if &cloned[0] == &blocks[0] {
		t.Fatal("Clone must copy the backing slice")
	}
	cloned[1].Image.URL = "https://example.com/mutated.png"
	if blocks[1].Image.URL != "https://example.com/a.png" {
		t.Fatalf("mutation leaked into source: %q", blocks[1].Image.URL)
	}
	if ContentBlocks(nil).Clone() != nil {
		t.Fatal("ContentBlocks(nil).Clone() must return nil")
	}
}

func TestContentBlocksTextRejectsImage(t *testing.T) {
	blocks := ContentBlocks{TextBlock("a"), ImageBlock("https://example.com/a.png")}
	if _, err := blocks.Text(); err == nil {
		t.Fatalf("expected error for image block, got nil")
	}
}

func TestContentBlocksValidateForRoleAndImagePlaceholders(t *testing.T) {
	blocks := ContentBlocks{
		TextBlock("看图"),
		ImageBlock("https://example.com/a.png?sig=secret#fragment"),
	}
	if err := blocks.ValidateForRole(RoleUser); err != nil {
		t.Fatalf("ValidateForRole(user) error = %v", err)
	}
	if err := blocks.ValidateForRole(RoleAssistant); err == nil {
		t.Fatal("assistant image content must be rejected")
	}

	degraded := blocks.WithImagePlaceholders()
	if len(degraded) != 2 || degraded[0].Text != "看图" || degraded[1].Text != "[图片: https://example.com/a.png]" {
		t.Fatalf("WithImagePlaceholders() = %#v", degraded)
	}
	if blocks[1].Type != ContentTypeImage {
		t.Fatal("WithImagePlaceholders must not mutate the source")
	}
}
