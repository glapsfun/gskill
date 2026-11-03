package fsutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gofrs/flock"

	"github.com/glapsfun/gskill/internal/errs"
)

// LockMode selects the locking discipline for Acquire.
type LockMode int

const (
	// LockExclusive takes a single-writer lock; mutating commands use this.
	LockExclusive LockMode = iota
	// LockShared takes a multi-reader lock; read-only commands may use this.
	LockShared
	// LockNone performs no locking and never blocks.
	LockNone
)

// retryInterval is how often Acquire retries a contended lock.
const retryInterval = 50 * time.Millisecond

// Lock is a held filesystem lock. Release must be called to free it.
type Lock struct {
	fl   *flock.Flock
	mode LockMode
}

// Acquire obtains a lock at path with the given mode, retrying until timeout. On
// timeout it returns an error that maps to the cache/lock-failure exit code 12
// (FR-021). LockNone returns immediately without touching the filesystem.
func Acquire(ctx context.Context, path string, mode LockMode, timeout time.Duration) (*Lock, error) {
	if mode == LockNone {
		return &Lock{mode: LockNone}, nil
	}

	return acquire(ctx, flock.New(path), path, mode, timeout)
}

// AcquireDir takes an exclusive lock on the directory itself, retrying until
// timeout, with the same exit-code mapping as Acquire. Locking the directory
// leaves no lock file behind in places like /usr/local/bin, and the lock keeps
// holding while files inside the directory are renamed.
func AcquireDir(ctx context.Context, dir string, timeout time.Duration) (*Lock, error) {
	return acquire(ctx, flock.New(dir, flock.SetFlag(os.O_RDONLY)), dir, LockExclusive, timeout)
}

// acquire retries fl until timeout. A timeout keeps the "within" message;
// any other failure (the path cannot be opened, the filesystem has no locks)
// is reported with its own cause. Both map to exit code 12.
func acquire(ctx context.Context, fl *flock.Flock, path string, mode LockMode, timeout time.Duration) (*Lock, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		locked bool
		err    error
	)
	switch mode {
	case LockShared:
		locked, err = fl.TryRLockContext(ctx, retryInterval)
	case LockExclusive, LockNone: // LockNone never reaches here; listed for exhaustiveness.
		locked, err = fl.TryLockContext(ctx, retryInterval)
	}
	switch {
	case locked && err == nil:
		return &Lock{fl: fl, mode: mode}, nil
	case err == nil || errors.Is(err, context.DeadlineExceeded):
		return nil, errs.Wrap(errs.CodeCacheLock, fmt.Sprintf("acquire lock %s within %s", path, timeout), err)
	default:
		return nil, errs.Wrap(errs.CodeCacheLock, "acquire lock "+path, err)
	}
}

// Release frees the lock. It is safe to call on a LockNone lock.
func (l *Lock) Release() error {
	if l == nil || l.mode == LockNone || l.fl == nil {
		return nil
	}
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}
