package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// NewChildProcess builds a guarded shell process without starting it.
func NewChildProcess(command, workDir string, overrides map[string]string) (*exec.Cmd, error) {
	shell, arguments := ShellInvocation(command)
	child := exec.Command(shell, arguments...)
	child.Dir = workDir
	environment, err := processEnvironment(overrides)
	if err != nil {
		return nil, err
	}
	child.Env = environment
	ConfigureProcessGroup(child)
	return child, nil
}

func processEnvironment(overrides map[string]string) ([]string, error) {
	environment := os.Environ()
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("无效环境变量名: %q", key)
		}
		if strings.EqualFold(key, "PATH") {
			return nil, errors.New("禁止通过 exec 覆盖 PATH")
		}
		value := overrides[key]
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("环境变量 %s 包含 NUL", key)
		}
		environment = append(environment, key+"="+value)
	}
	return environment, nil
}
