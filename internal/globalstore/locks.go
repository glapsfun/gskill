package globalstore

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/home"
)

// defaultNoticeAfter is how long a lock acquisition may block before the
// waiting notice is emitted.
const defaultNoticeAfter = 2 * time.Second

// Locker acquires the gskill lock taxonomy under <home>/locks: per-object
// admission locks, per-project mutation locks, the GC lock, and the registry
// lock. A contended lock waits up to the configured timeout, emitting a
// visible waiting notice, then fails with the lock-failure exit code.
type Locker struct {
	home        *home.Home
	timeout     time.Duration
	notice      io.Writer
	noticeAfter time.Duration
}

// NewLocker returns a Locker with the given acquisition timeout. notice
// receives the waiting message (nil silences it); commands pass stderr.
func NewLocker(h *home.Home, timeout time.Duration, notice io.Writer) *Locker {
	return NewLockerWithNotice(h, timeout, notice, defaultNoticeAfter)
}

// NewLockerWithNotice is NewLocker with a custom notice threshold (tests use
// a short one).
func NewLockerWithNotice(h *home.Home, timeout time.Duration, notice io.Writer, noticeAfter time.Duration) *Locker {
	return &Locker{home: h, timeout: timeout, notice: notice, noticeAfter: noticeAfter}
}

// ObjectLockPath returns the lock file guarding one store object.
func (l *Locker) ObjectLockPath(key string) string {
	return filepath.Join(l.home.LocksDir(), "store-"+safeKeyName(key)+".lock")
}

// GCLockPath returns the lock file guarding a whole GC apply run.
func (l *Locker) GCLockPath() string {
	return filepath.Join(l.home.LocksDir(), "gc.lock")
}

// RegistryLockPath returns the lock file guarding project-registry writes.
func (l *Locker) RegistryLockPath() string {
	return filepath.Join(l.home.LocksDir(), "projects.lock")
}

// LockObject takes the exclusive lock for one store object.
func (l *Locker) LockObject(ctx context.Context, key string) (*fsutil.Lock, error) {
	return l.acquire(ctx, l.ObjectLockPath(key), "store object "+key)
}

// LockGC takes the exclusive garbage-collection lock.
func (l *Locker) LockGC(ctx context.Context) (*fsutil.Lock, error) {
	return l.acquire(ctx, l.GCLockPath(), "store garbage collection")
}

// LockRegistry takes the exclusive project-registry lock.
func (l *Locker) LockRegistry(ctx context.Context) (*fsutil.Lock, error) {
	return l.acquire(ctx, l.RegistryLockPath(), "project registry")
}

// acquire waits for the lock at path, emitting a waiting notice once the
// acquisition has been blocked for the notice threshold. The notice is
// written synchronously between two acquisition attempts, so no goroutine
// races the caller.
func (l *Locker) acquire(ctx context.Context, path, what string) (*fsutil.Lock, error) {
	if l.notice != nil && l.noticeAfter < l.timeout {
		lock, err := fsutil.Acquire(ctx, path, fsutil.LockExclusive, l.noticeAfter)
		if err == nil {
			return lock, nil
		}
		_, _ = fmt.Fprintf(l.notice, "waiting for lock on %s (held by another gskill process)\n", what)
		return fsutil.Acquire(ctx, path, fsutil.LockExclusive, l.timeout-l.noticeAfter)
	}
	return fsutil.Acquire(ctx, path, fsutil.LockExclusive, l.timeout)
}
