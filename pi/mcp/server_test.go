package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/extension"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
)

type cleanupTransport struct {
	closed int
	err    error
}

func (*cleanupTransport) Send(context.Context, Request) (Response, error) {
	return Response{}, errors.New("unused")
}

func (transport *cleanupTransport) Close(context.Context) error {
	transport.closed++
	return transport.err
}

func TestFinishServerExtensionClosesTransportWhenValidationFails(t *testing.T) {
	transport := &cleanupTransport{}
	_, err := finishServerExtension(ServerOptions{Name: "test"}, transport)
	if err == nil || !strings.Contains(err.Error(), "allow_tools") {
		t.Fatalf("finishServerExtension() error = %v", err)
	}
	if transport.closed != 1 {
		t.Fatalf("transport close count = %d, want 1", transport.closed)
	}
}

type cleanupExtension struct {
	name   string
	closed *[]string
	err    error
}

func (extension *cleanupExtension) Name() string                        { return extension.name }
func (*cleanupExtension) Register(context.Context, extension.API) error { return nil }
func (extension *cleanupExtension) Close(context.Context) error {
	*extension.closed = append(*extension.closed, extension.name)
	return extension.err
}

func TestCloseExtensionsUsesReverseOrderAndJoinsErrors(t *testing.T) {
	order := []string{}
	firstErr := errors.New("first close")
	secondErr := errors.New("second close")
	err := closeExtensions(extension.Extensions{
		&cleanupExtension{name: "first", closed: &order, err: firstErr},
		&cleanupExtension{name: "second", closed: &order, err: secondErr},
	})
	if strings.Join(order, ",") != "second,first" {
		t.Fatalf("close order = %v", order)
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("closeExtensions() error = %v", err)
	}
}

func TestNewRejectsStdioHostPathOverride(t *testing.T) {
	_, err := New([]ServerOptions{{
		Name:       "test",
		Transport:  "stdio",
		Command:    "true",
		Env:        map[string]string{"PATH": "/tmp"},
		AllowTools: []string{"read"},
	}}, tools.Root(t.TempDir()), sandbox.NewHostRunner())
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("New() error = %v, want PATH override rejection", err)
	}
}

func TestNewDoesNotBypassRestrictedOrDisabledStdioRunner(t *testing.T) {
	root := tools.Root(t.TempDir())
	for _, policy := range []sandbox.Policy{
		{Backend: "host", WriteMode: "restricted", TmpDir: filepath.Join(string(root), ".tmp")},
		{Backend: "disabled", WriteMode: "all"},
	} {
		_, err := New([]ServerOptions{{
			Name: "test", Transport: "stdio", Command: "true",
			Env: map[string]string{"HOME": "/override"}, AllowTools: []string{"read"},
		}}, root, &recordingRunner{policy: policy})
		if err == nil {
			t.Fatalf("policy %#v bypassed Runner", policy)
		}
	}
}
