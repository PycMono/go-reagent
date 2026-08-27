package main

// stderrLogger 实现 logsdk.Logger：SDK 自带的 NewLogrus 硬编码写 stdout 且
// 级别固定 Trace，会污染 CLI 的 stdout 纯净契约（stdout 只出模型正文），
// 因此这里自实现一个全部写 stderr、默认 Warn 的薄 Logger。

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
)

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

// stderrLogger 把所有级别写到 stderr；verbose 为 true 时级别放宽到 Info。
// Debug 始终丢弃（CLI 不暴露 Trace/Debug 开关）。
type stderrLogger struct {
	mu      sync.Mutex
	out     io.Writer
	minimum logLevel
}

func newStderrLogger(out io.Writer, verbose bool) *stderrLogger {
	minimum := levelWarn
	if verbose {
		minimum = levelInfo
	}
	return &stderrLogger{out: out, minimum: minimum}
}

func (l *stderrLogger) log(level logLevel, label, message string, fields logsdk.Fields) {
	if level < l.minimum {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s %-5s %s", time.Now().Format("15:04:05"), label, message)
	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(l.out, " %s=%v", key, fields[key])
		}
	}
	fmt.Fprintln(l.out)
}

func (l *stderrLogger) Debug(_ context.Context, message string, fields ...logsdk.Fields) {
	l.log(levelDebug, "DEBUG", message, mergeFields(fields))
}

func (l *stderrLogger) Info(_ context.Context, message string, fields ...logsdk.Fields) {
	l.log(levelInfo, "INFO", message, mergeFields(fields))
}

func (l *stderrLogger) Warn(_ context.Context, message string, fields ...logsdk.Fields) {
	l.log(levelWarn, "WARN", message, mergeFields(fields))
}

func (l *stderrLogger) Error(_ context.Context, message string, fields ...logsdk.Fields) {
	l.log(levelError, "ERROR", message, mergeFields(fields))
}

// Fatal 在 CLI 中退化为 Error：Start 之后禁止绕过 app.Stop 直接退出进程
// （会跳过 ProcessSupervisor.OnStop 等清理），退出路径统一走 shutdown。
func (l *stderrLogger) Fatal(ctx context.Context, message string, fields ...logsdk.Fields) {
	l.Error(ctx, message, fields...)
}

func (l *stderrLogger) Panic(_ context.Context, message string, fields ...logsdk.Fields) {
	l.log(levelError, "PANIC", message, mergeFields(fields))
	panic(message)
}

func mergeFields(fields []logsdk.Fields) logsdk.Fields {
	var merged logsdk.Fields
	for _, item := range fields {
		if len(item) == 0 {
			continue
		}
		if merged == nil {
			merged = make(logsdk.Fields, len(item))
		}
		for key, value := range item {
			merged[key] = value
		}
	}
	return merged
}
