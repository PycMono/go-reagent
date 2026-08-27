package main

// REPL 与信号处理。信号 goroutine 与 REPL 之间通过 mutex 保护的
// signalController 交互：运行中 SIGINT 取消当轮，空闲 SIGINT/SIGTERM 请求
// 优雅退出，3 秒内第二次 SIGINT 强制退出（唯一跳过 app.Stop 的例外）。

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/governor"
)

const (
	exitOK          = 0
	exitError       = 1
	exitInterrupted = 130

	maxInputBytes = 256 * 1024
)

// forceExit 是二次 SIGINT 的强退出口；测试中替换以避免杀死测试进程。
var forceExit = os.Exit

// signalController 持有信号 goroutine 与 REPL 共享的状态。
type signalController struct {
	mu         sync.Mutex
	cancel     context.CancelFunc // 当前 Run 的 cancel；nil 表示空闲
	lastSignal time.Time
	stopCh     chan struct{}
	stopOnce   sync.Once
	sigCh      chan os.Signal
	done       chan struct{}
	stderr     io.Writer
	now        func() time.Time
}

func newSignalController(stderr io.Writer) *signalController {
	return &signalController{
		stopCh: make(chan struct{}),
		sigCh:  make(chan os.Signal, 4),
		done:   make(chan struct{}),
		stderr: stderr,
		now:    time.Now,
	}
}

// start 安装 OS 信号转发并启动处理循环。
func (c *signalController) start() {
	signal.Notify(c.sigCh, os.Interrupt, syscall.SIGTERM)
	go c.loop()
}

func (c *signalController) close() {
	signal.Stop(c.sigCh)
	close(c.done)
}

func (c *signalController) loop() {
	for {
		select {
		case <-c.done:
			return
		case sig := <-c.sigCh:
			c.handle(sig)
		}
	}
}

func (c *signalController) handle(sig os.Signal) {
	c.mu.Lock()
	sinceLast := c.now().Sub(c.lastSignal)
	c.lastSignal = c.now()
	cancel := c.cancel
	c.mu.Unlock()

	if sig == os.Interrupt && sinceLast < 3*time.Second {
		fmt.Fprintln(c.stderr, "⚠ 强制退出，后台进程可能未回收。")
		forceExit(exitInterrupted)
		return
	}
	if cancel != nil {
		fmt.Fprintln(c.stderr, "（取消当前运行…）")
		cancel()
	}
	if sig == syscall.SIGTERM || cancel == nil {
		c.requestStop()
	}
}

func (c *signalController) requestStop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

// register 登记当前 Run 的 cancel；返回注销函数。
func (c *signalController) register(cancel context.CancelFunc) func() {
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		c.cancel = nil
		c.mu.Unlock()
	}
}

type sessionOptions struct {
	prompt       string // -prompt 单轮内容；promptSet 为 true 时生效
	promptSet    bool
	historyLimit int
	limits       governor.Limits
	verbose      bool
}

// runSession 运行 -prompt 单轮或 REPL 多轮，返回进程退出码。
func runSession(runner pi.Runner, stdin io.Reader, stdout, stderr io.Writer, options sessionOptions) int {
	listener := newTerminalListener(stdout, stderr, options.verbose)
	sess := newSession(runner, listener, stderr, options.limits, options.historyLimit)
	controller := newSignalController(stderr)
	controller.start()
	defer controller.close()

	if options.promptSet {
		return runPromptOnce(sess, controller, stderr, options.prompt)
	}
	return runREPL(sess, controller, stdin, stderr)
}

func runPromptOnce(sess *session, controller *signalController, stderr io.Writer, prompt string) int {
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(stderr, "错误：-prompt 不能为空")
		return exitError
	}
	ctx, cancel := context.WithCancel(context.Background())
	unregister := controller.register(cancel)
	err := sess.runTurn(ctx, prompt)
	unregister()
	cancel()
	if errors.Is(err, context.Canceled) {
		return exitInterrupted
	}
	if err != nil {
		return exitError
	}
	return exitOK
}

func runREPL(sess *session, controller *signalController, stdin io.Reader, stderr io.Writer) int {
	lines := readLines(stdin)
	fmt.Fprintln(stderr, "输入 /help 查看命令，/exit 退出。")
	for {
		fmt.Fprint(stderr, "> ")
		select {
		case <-controller.stopCh:
			fmt.Fprintln(stderr, "再见。")
			return exitOK
		case item, ok := <-lines:
			if !ok { // Ctrl-D
				fmt.Fprintln(stderr, "再见。")
				return exitOK
			}
			if item.tooLong {
				fmt.Fprintf(stderr, "⚠ 输入超过 %d KiB，已忽略该行。\n", maxInputBytes/1024)
				continue
			}
			line := item.text
			if handleCommand(sess, line, stderr) {
				return exitOK
			}
			if line == "" || strings.HasPrefix(line, "/") {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			unregister := controller.register(cancel)
			sess.runTurn(ctx, line)
			unregister()
			cancel()
		}
	}
}

// handleCommand 处理 / 前缀命令；返回 true 表示退出 REPL。
func handleCommand(sess *session, line string, stderr io.Writer) bool {
	switch strings.TrimSpace(line) {
	case "/exit", "/quit":
		fmt.Fprintln(stderr, "再见。")
		return true
	case "/new":
		sess.reset()
		fmt.Fprintln(stderr, "已开启新会话（历史与用量已清空）。")
	case "/help":
		fmt.Fprintln(stderr, `命令：
  /exit, /quit  退出
  /new          清空历史，开启新会话
  /help         本帮助
其余输入视为与 Agent 的对话。`)
	case "":
	default:
		if strings.HasPrefix(line, "/") {
			fmt.Fprintf(stderr, "未知命令 %q，输入 /help 查看命令。\n", line)
		}
	}
	return false
}

// inputLine 是后台读线 goroutine 交付的一行输入；tooLong 标记超上限被拒
// 的行（警告由 REPL 主 goroutine 打印，避免与提示符输出竞争 stderr）。
type inputLine struct {
	text    string
	tooLong bool
}

// readLines 后台读入 stdin 行。单行超过 maxInputBytes 时整行标记拒绝（该行
// 已被完整读出，尾部不会误作下一条输入）。
func readLines(stdin io.Reader) <-chan inputLine {
	lines := make(chan inputLine)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(stdin)
		for {
			line, err := reader.ReadString('\n')
			switch {
			case len(line) > maxInputBytes:
				lines <- inputLine{tooLong: true}
			default:
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					lines <- inputLine{text: trimmed}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return lines
}
