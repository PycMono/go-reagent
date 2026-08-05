package main

import (
	"net/http/httptest"
	"testing"
)

func TestPingHandler(t *testing.T) {
	response := httptest.NewRecorder()
	pingHandler(response, httptest.NewRequest("GET", "/ping", nil))
	if response.Body.String() != "pong" {
		t.Fatalf("body = %q, want pong", response.Body.String())
	}
}
