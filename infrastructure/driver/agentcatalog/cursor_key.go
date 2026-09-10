package agentcatalog

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const cursorKeySize = 32

func LoadCursorKey(dataDir string) ([]byte, error) {
	stateDir := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cursor state directory: %w", err)
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil || !stateInfo.IsDir() || stateInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cursor state path must be a real directory")
	}
	path := filepath.Join(stateDir, "cursor.key")
	key, err := readCursorKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, cursorKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate cursor key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readCursorKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create cursor key: %w", err)
	}
	remove := true
	defer func() {
		file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(key); err != nil {
		return nil, fmt.Errorf("write cursor key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync cursor key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close cursor key: %w", err)
	}
	directory, err := os.Open(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open cursor state directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return nil, fmt.Errorf("sync cursor state directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return nil, fmt.Errorf("close cursor state directory: %w", err)
	}
	remove = false
	return append([]byte(nil), key...), nil
}

func readCursorKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("cursor key must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cursor key: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, errors.New("cursor key changed during validation")
	}
	key, err := io.ReadAll(io.LimitReader(file, cursorKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read cursor key: %w", err)
	}
	if len(key) != cursorKeySize {
		return nil, errors.New("cursor key has invalid length")
	}
	return key, nil
}
