//go:build unix

package cache

import (
	"errors"
	"os"
	"syscall"
)

type platformLock struct{}

func tryPlatformLock(file *os.File) (platformLock, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return platformLock{}, ErrLocked
	}
	return platformLock{}, err
}

func unlockPlatform(file *os.File, _ *platformLock) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
