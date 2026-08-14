package chat

import (
	"testing"
	"time"

	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

func TestConversationCursorRoundTrip(t *testing.T) {
	want := conversationrepo.ListCursor{
		UpdatedAt: time.Date(2026, 8, 14, 10, 5, 20, 123, time.UTC),
		ID:        "internal-1",
	}
	encoded := encodeConversationCursor(want)
	got, err := decodeConversationCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

func TestMessageCursorRoundTrip(t *testing.T) {
	want := conversationrepo.MessageCursor{TurnVersion: 7, Ordinal: 3}
	encoded := encodeMessageCursor(want)
	got, err := decodeMessageCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != want {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

func TestCursorRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "%%%", "e30", "eyJpZCI6IngifXh"} {
		if _, err := decodeConversationCursor(value); err == nil {
			t.Errorf("decodeConversationCursor(%q) succeeded", value)
		}
	}
	for _, value := range []string{"", "%%%", "e30", "eyJ0dXJuX3ZlcnNpb24iOjF9eA"} {
		if _, err := decodeMessageCursor(value); err == nil {
			t.Errorf("decodeMessageCursor(%q) succeeded", value)
		}
	}
}
