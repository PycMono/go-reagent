package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestToolCommandHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return
	}
	arguments := os.Args[separator+1:]
	if len(arguments) == 0 {
		os.Exit(2)
	}
	switch arguments[0] {
	case "output-exit":
		_, _ = fmt.Fprint(os.Stdout, "stdout")
		_, _ = fmt.Fprint(os.Stderr, "stderr")
		os.Exit(7)

	case "print":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		_, _ = fmt.Fprint(os.Stdout, arguments[1])

	case "cwd-env":
		workDir, err := os.Getwd()
		if err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s|%s", workDir, os.Getenv("REAGENT_TEST_VALUE"))

	case "sleep":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		milliseconds, err := strconv.Atoi(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)

	case "sleep-output":
		if len(arguments) != 3 {
			os.Exit(2)
		}
		milliseconds, err := strconv.Atoi(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
		_, _ = fmt.Fprint(os.Stdout, arguments[2])

	case "large-output":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		size, err := strconv.Atoi(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), size))

	case "copy-stdin":
		_, _ = io.Copy(os.Stdout, os.Stdin)

	case "spawn-child":
		if len(arguments) < 2 || len(arguments) > 3 {
			os.Exit(2)
		}
		marker := arguments[1]
		delay := "300"
		if len(arguments) == 3 {
			delay = arguments[2]
		}
		child := exec.Command(os.Args[0], "-test.run=^TestToolCommandHelper$", "--", "delayed-write", delay, marker)
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(marker+".ready", []byte("ready"), 0o600); err != nil {
			_ = child.Process.Kill()
			os.Exit(2)
		}
		if err := child.Wait(); err != nil {
			os.Exit(1)
		}

	case "delayed-write":
		if len(arguments) != 3 {
			os.Exit(2)
		}
		milliseconds, err := strconv.Atoi(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
		if err := os.WriteFile(arguments[2], []byte("survived"), 0o600); err != nil {
			os.Exit(2)
		}

	case "paced-output":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		milliseconds, err := strconv.Atoi(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprint(os.Stdout, "early")
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
		_, _ = fmt.Fprint(os.Stderr, "late")

	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func toolHelperCommand(arguments ...string) string {
	command := []string{quoteToolHelperArgument(os.Args[0]), "-test.run=^TestToolCommandHelper$", "--"}
	for _, argument := range arguments {
		command = append(command, quoteToolHelperArgument(argument))
	}
	return strings.Join(command, " ")
}

func quoteToolHelperArgument(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
