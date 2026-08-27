package main

import (
	"bytes"
	"context"
	"testing"

	logsdk "github.com/PycMono/go-logger-sdk"
)

func TestStderrLoggerLevelFiltering(t *testing.T) {
	var out bytes.Buffer
	logger := newStderrLogger(&out, false)

	logger.Debug(context.Background(), "debug-msg")
	logger.Info(context.Background(), "info-msg")
	logger.Warn(context.Background(), "warn-msg")
	logger.Error(context.Background(), "error-msg", logsdk.Fields{"k": "v"})

	got := out.String()
	if bytes.Contains(out.Bytes(), []byte("debug-msg")) || bytes.Contains(out.Bytes(), []byte("info-msg")) {
		t.Fatalf("default level should drop debug/info, got %q", got)
	}
	if !bytes.Contains(out.Bytes(), []byte("warn-msg")) || !bytes.Contains(out.Bytes(), []byte("error-msg")) {
		t.Fatalf("default level should keep warn/error, got %q", got)
	}
	if !bytes.Contains(out.Bytes(), []byte("k=v")) {
		t.Fatalf("fields should be rendered, got %q", got)
	}
}

func TestStderrLoggerVerboseIncludesInfo(t *testing.T) {
	var out bytes.Buffer
	logger := newStderrLogger(&out, true)
	logger.Info(context.Background(), "info-msg")
	if !bytes.Contains(out.Bytes(), []byte("info-msg")) {
		t.Fatalf("verbose should keep info, got %q", out.String())
	}
}

func TestStderrLoggerFatalDoesNotExit(t *testing.T) {
	var out bytes.Buffer
	logger := newStderrLogger(&out, false)
	// 不退出进程即通过（os.Exit 会直接杀死测试进程）。
	logger.Fatal(context.Background(), "fatal-msg")
	if !bytes.Contains(out.Bytes(), []byte("fatal-msg")) {
		t.Fatalf("fatal should be logged as error, got %q", out.String())
	}
}
