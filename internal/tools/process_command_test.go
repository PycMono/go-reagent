package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
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
