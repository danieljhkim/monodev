package lockfile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireExclusiveContentionIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource.lock")
	first, err := Acquire(context.Background(), path, Exclusive, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLock(t, first)

	started := time.Now()
	_, err = Acquire(context.Background(), path, Exclusive, 40*time.Millisecond)
	if !errors.Is(err, ErrContended) {
		t.Fatalf("Acquire error = %v, want ErrContended", err)
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("contention elapsed = %s, want bounded wait", elapsed)
	}
}

func TestAcquireSharedLocksCanOverlapAndExcludeWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource.lock")
	one, err := Acquire(context.Background(), path, Shared, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLock(t, one)
	two, err := Acquire(context.Background(), path, Shared, time.Second)
	if err != nil {
		t.Fatalf("second shared lock: %v", err)
	}
	defer closeLock(t, two)

	if _, err := Acquire(context.Background(), path, Exclusive, 30*time.Millisecond); !errors.Is(err, ErrContended) {
		t.Fatalf("exclusive Acquire error = %v, want ErrContended", err)
	}
}

func TestLockIsReleasedWhenProcessExits(t *testing.T) {
	if os.Getenv("MONODEV_LOCK_HELPER") == "1" {
		lock, err := Acquire(context.Background(), os.Getenv("MONODEV_LOCK_PATH"), Exclusive, time.Second)
		if err != nil {
			os.Exit(2)
		}
		_ = lock // Exit intentionally without Close.
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "crash.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockIsReleasedWhenProcessExits$")
	cmd.Env = append(os.Environ(), "MONODEV_LOCK_HELPER=1", "MONODEV_LOCK_PATH="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper failed: %v: %s", err, output)
	}

	lock, err := Acquire(context.Background(), path, Exclusive, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("lock remained held after helper exit: %v", err)
	}
	defer closeLock(t, lock)
}

func TestUnrelatedLocksProceedConcurrently(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(context.Background(), filepath.Join(dir, "one.lock"), Exclusive, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLock(t, first)

	second, err := Acquire(context.Background(), filepath.Join(dir, "two.lock"), Exclusive, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("unrelated lock was blocked: %v", err)
	}
	defer closeLock(t, second)
}

func closeLock(t *testing.T, lock *Lock) {
	t.Helper()
	if err := lock.Close(); err != nil {
		t.Errorf("close lock: %v", err)
	}
}
