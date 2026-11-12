package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LockFileName is the cross-process writer lock stored at the cache root.
const LockFileName = ".devlog.lock"

// ErrLocked reports that another devlog-learn process owns the cache writer
// lock. Callers should surface this as a retryable conflict.
var ErrLocked = errors.New("cache is busy")

// Lock is the exclusive cross-process writer lock for one cache directory.
// It also coordinates independent CLI and Web UI processes.
type Lock struct {
	file     *os.File
	platform platformLock

	closeOnce sync.Once
	closeErr  error
}

// TryLock acquires the exclusive cache writer lock without waiting.
func TryLock(root string) (*Lock, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("cache root must not be empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create cache root: %w", err)
	}
	path := filepath.Join(root, LockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}
	platform, err := tryPlatformLock(file)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, ErrLocked) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, root)
		}
		return nil, fmt.Errorf("lock cache %s: %w", root, err)
	}
	return &Lock{file: file, platform: platform}, nil
}

// Close releases the lock. It is safe to call more than once.
func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		unlockErr := unlockPlatform(l.file, &l.platform)
		closeErr := l.file.Close()
		l.closeErr = errors.Join(unlockErr, closeErr)
	})
	return l.closeErr
}
