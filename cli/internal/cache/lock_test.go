package cache

import (
	"errors"
	"testing"
)

func TestTryLockExcludesSecondOwner(t *testing.T) {
	root := t.TempDir()

	first, err := TryLock(root)
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	defer first.Close()

	if _, err := TryLock(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("second TryLock error = %v, want ErrLocked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := TryLock(root)
	if err != nil {
		t.Fatalf("TryLock after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
