//go:build !darwin && !linux

package agentstate

import (
	"errors"
	"os"
)

func Lock(string) (*os.File, error) {
	return nil, errors.New("Agent platform requires Linux or macOS file locking and process isolation")
}
