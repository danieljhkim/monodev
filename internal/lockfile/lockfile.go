// Package lockfile provides bounded process-wide advisory file locks.
package lockfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Mode controls whether a lock excludes readers or only other writers.
type Mode int

const (
	Shared Mode = iota
	Exclusive

	// DefaultTimeout bounds user-visible contention instead of waiting forever.
	DefaultTimeout = 2 * time.Second
	pollInterval   = 10 * time.Millisecond
)

// ErrContended identifies a lock that could not be acquired before its timeout.
var ErrContended = errors.New("resource lock contention")

// ContentionError describes the resource and wait bound that were exhausted.
type ContentionError struct {
	Path    string
	Mode    Mode
	Timeout time.Duration
}

func (e *ContentionError) Error() string {
	return fmt.Sprintf("%v: %s lock %q was busy for %s", ErrContended, e.Mode, e.Path, e.Timeout)
}

func (e *ContentionError) Unwrap() error { return ErrContended }

func (m Mode) String() string {
	if m == Shared {
		return "shared"
	}
	return "exclusive"
}

// Lock is held until Close or process exit. The kernel releases locks for
// abruptly terminated processes when their file descriptors are closed.
type Lock struct {
	file *os.File
}

// Acquire obtains a lock, polling so context cancellation and timeout remain
// bounded and deterministic. Lock files are durable coordination names; their
// presence does not mean the resource is currently locked.
func Acquire(ctx context.Context, path string, mode Mode, timeout time.Duration) (*Lock, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	op := unix.LOCK_EX | unix.LOCK_NB
	if mode == Shared {
		op = unix.LOCK_SH | unix.LOCK_NB
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		err = unix.Flock(int(file.Fd()), op)
		if err == nil {
			return &Lock{file: file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("acquire %s lock %q: %w", mode, path, err)
		}

		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = file.Close()
			return nil, &ContentionError{Path: path, Mode: mode, Timeout: timeout}
		case <-ticker.C:
		}
	}
}

// Close releases the advisory lock. It is safe to call more than once.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock file: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock file: %w", closeErr)
	}
	return nil
}
