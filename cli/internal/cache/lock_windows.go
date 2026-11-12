//go:build windows

package cache

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = syscall.Errno(33)
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

type platformLock struct {
	overlapped syscall.Overlapped
}

func tryPlatformLock(file *os.File) (platformLock, error) {
	lock := platformLock{}
	result, _, callErr := procLockFileEx.Call(
		file.Fd(),
		lockfileFailImmediately|lockfileExclusiveLock,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&lock.overlapped)),
	)
	if result != 0 {
		return lock, nil
	}
	if errors.Is(callErr, errorLockViolation) {
		return platformLock{}, ErrLocked
	}
	if callErr == syscall.Errno(0) {
		callErr = errors.New("LockFileEx failed")
	}
	return platformLock{}, callErr
}

func unlockPlatform(file *os.File, lock *platformLock) error {
	result, _, callErr := procUnlockFileEx.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&lock.overlapped)),
	)
	if result != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		return errors.New("UnlockFileEx failed")
	}
	return callErr
}
