package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
)

// stubForceExit 拦截强退，返回记录器与恢复函数。
func stubForceExit(t *testing.T) *atomic.Int32 {
	t.Helper()
	called := &atomic.Int32{}
	called.Store(-1)
	original := forceExit
	forceExit = func(code int) { called.Store(int32(code)) }
	t.Cleanup(func() { forceExit = original })
	return called
}

func TestSignalCancelRunningTurn(t *testing.T) {
	controller := newSignalController(&bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	unregister := controller.register(cancel)
	defer unregister()

	controller.handle(os.Interrupt)
	if ctx.Err() != context.Canceled {
		t.Fatal("运行态 SIGINT 应取消当轮")
	}
	select {
	case <-controller.stopCh:
		t.Fatal("运行态 SIGINT 不应请求退出")
	default:
	}
}

func TestSignalIdleInterruptRequestsStop(t *testing.T) {
	controller := newSignalController(&bytes.Buffer{})
	controller.handle(os.Interrupt)
	select {
	case <-controller.stopCh:
	default:
		t.Fatal("空闲态 SIGINT 应请求优雅退出")
	}
}

func TestSignalTermCancelsAndStops(t *testing.T) {
	controller := newSignalController(&bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	unregister := controller.register(cancel)
	defer unregister()

	controller.handle(syscall.SIGTERM)
	if ctx.Err() != context.Canceled {
		t.Fatal("SIGTERM 应取消当前运行")
	}
	select {
	case <-controller.stopCh:
	default:
		t.Fatal("SIGTERM 应请求优雅退出")
	}
}

func TestSecondInterruptForceExits(t *testing.T) {
	called := stubForceExit(t)
	controller := newSignalController(&bytes.Buffer{})
	controller.now = time.Now

	controller.handle(os.Interrupt) // 第一次：优雅
	if called.Load() != -1 {
		t.Fatal("第一次 SIGINT 不应强退")
	}
	controller.handle(os.Interrupt) // 3 秒内第二次：强退
	if called.Load() != exitInterrupted {
		t.Fatalf("二次 SIGINT 应强退 130, got %d", called.Load())
	}
}

func TestREPLMultiTurnAndCommands(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{
		{NewMessages: []ai.Message{assistantText("回答一")}},
		{NewMessages: []ai.Message{assistantText("回答二")}},
	}}
	stdin := strings.NewReader("问题一\n/new\n问题二\n/exit\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := runSession(runner, stdin, stdout, stderr, sessionOptions{historyLimit: 100})
	if code != exitOK {
		t.Fatalf("exit code = %d", code)
	}
	if stdout.String() != "回答一\n回答二\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	// /new 清空了历史：第二轮请求不带第一轮历史。
	if len(runner.requests) != 2 || len(runner.requests[1].History) != 0 {
		t.Fatalf("/new 后历史应清空, requests = %#v", runner.requests)
	}
}

func TestREPLSingleTurnFailureContinues(t *testing.T) {
	runner := &fakeRunner{
		results: []pi.RunResult{
			{},
			{NewMessages: []ai.Message{assistantText("恢复后的回答")}},
		},
		errs: []error{errors.New("provider boom"), nil},
	}
	stdin := strings.NewReader("会失败\n再来\n/exit\n")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := runSession(runner, stdin, stdout, stderr, sessionOptions{historyLimit: 100})
	if code != exitOK {
		t.Fatalf("REPL 单轮失败不应退出进程, code = %d", code)
	}
	if !strings.Contains(stderr.String(), "provider boom") {
		t.Fatalf("失败应提示, stderr = %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "恢复后的回答") {
		t.Fatalf("会话应继续, stdout = %q", stdout.String())
	}
}

func TestREPLCtrlDExits(t *testing.T) {
	runner := &fakeRunner{}
	stdin := strings.NewReader("") // 直接 EOF
	code := runSession(runner, stdin, &bytes.Buffer{}, &bytes.Buffer{}, sessionOptions{historyLimit: 100})
	if code != exitOK {
		t.Fatalf("Ctrl-D 应正常退出, code = %d", code)
	}
}

func TestREPLRejectsOversizedLine(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{
		{NewMessages: []ai.Message{assistantText("ok")}},
	}}
	oversized := strings.Repeat("x", maxInputBytes+10)
	stdin := strings.NewReader(oversized + "\n正常输入\n/exit\n")
	stderr := &bytes.Buffer{}

	code := runSession(runner, stdin, &bytes.Buffer{}, stderr, sessionOptions{historyLimit: 100})
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stderr.String(), "已忽略该行") {
		t.Fatalf("超长行应被拒绝, stderr = %q", stderr.String())
	}
	if len(runner.requests) != 1 || runner.requests[0].Input.Content != "正常输入" {
		t.Fatalf("超长行尾部不应成为输入, requests = %#v", runner.requests)
	}
}

func TestPromptModeSuccess(t *testing.T) {
	runner := &fakeRunner{results: []pi.RunResult{
		{NewMessages: []ai.Message{assistantText("单轮回答")}},
	}}
	stdout := &bytes.Buffer{}
	code := runSession(runner, strings.NewReader(""), stdout, &bytes.Buffer{},
		sessionOptions{prompt: "一次性任务", promptSet: true, historyLimit: 100})
	if code != exitOK || stdout.String() != "单轮回答\n" {
		t.Fatalf("code = %d, stdout = %q", code, stdout.String())
	}
}

func TestPromptModeEmptyPrompt(t *testing.T) {
	code := runSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{},
		sessionOptions{prompt: "  ", promptSet: true, historyLimit: 100})
	if code != exitError {
		t.Fatalf("空 -prompt 应退出 1, code = %d", code)
	}
}

func TestPromptModeFailureExit1(t *testing.T) {
	runner := &fakeRunner{errs: []error{errors.New("boom")}}
	code := runSession(runner, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{},
		sessionOptions{prompt: "任务", promptSet: true, historyLimit: 100})
	if code != exitError {
		t.Fatalf("单轮失败应退出 1, code = %d", code)
	}
}

// 运行中被信号取消：-prompt 模式退出 130。
func TestPromptModeCanceledExit130(t *testing.T) {
	runner := &fakeRunner{
		results: []pi.RunResult{{Termination: governor.Termination{Reason: governor.TerminationCanceled}}},
		errs:    []error{context.Canceled},
	}
	code := runSession(runner, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{},
		sessionOptions{prompt: "任务", promptSet: true, historyLimit: 100})
	if code != exitInterrupted {
		t.Fatalf("取消应退出 130, code = %d", code)
	}
}

// 信号竞态：运行中反复 SIGINT/SIGTERM 与注册/注销并发，-race 下必须干净。
func TestSignalRaceWithRunRegistration(t *testing.T) {
	stubForceExit(t)
	controller := newSignalController(&bytes.Buffer{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			controller.handle(os.Interrupt)
			controller.handle(syscall.SIGTERM)
		}
	}()
	for i := 0; i < 50; i++ {
		_, cancel := context.WithCancel(context.Background())
		unregister := controller.register(cancel)
		unregister()
		cancel()
	}
	<-done
}
