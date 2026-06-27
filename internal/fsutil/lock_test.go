package fsutil_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
)

func TestAcquire_ExclusiveTimesOutWhenHeld(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x.lock")

	held, err := fsutil.Acquire(context.Background(), path, fsutil.LockExclusive, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	_, err = fsutil.Acquire(context.Background(), path, fsutil.LockExclusive, 100*time.Millisecond)
	if err == nil {
		t.Fatal("second Acquire succeeded while lock held, want timeout error")
	}
	if got := errs.ExitCode(err); got != 12 {
		t.Errorf("timeout ExitCode = %d, want 12 (cache/lock failure)", got)
	}
}

func TestAcquire_ReleaseAllowsReacquire(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x.lock")

	first, err := fsutil.Acquire(context.Background(), path, fsutil.LockExclusive, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, err := fsutil.Acquire(context.Background(), path, fsutil.LockExclusive, time.Second)
	if err != nil {
		t.Fatalf("re-Acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("Release second: %v", err)
	}
}

func TestAcquire_NoneIsNoop(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x.lock")

	a, err := fsutil.Acquire(context.Background(), path, fsutil.LockNone, time.Second)
	if err != nil {
		t.Fatalf("Acquire none: %v", err)
	}
	// A second none lock must not block.
	b, err := fsutil.Acquire(context.Background(), path, fsutil.LockNone, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("second none Acquire: %v", err)
	}
	if err := a.Release(); err != nil {
		t.Errorf("Release a: %v", err)
	}
	if err := b.Release(); err != nil {
		t.Errorf("Release b: %v", err)
	}
}

func TestAcquire_SharedAllowsConcurrentReaders(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x.lock")

	r1, err := fsutil.Acquire(context.Background(), path, fsutil.LockShared, time.Second)
	if err != nil {
		t.Fatalf("first shared Acquire: %v", err)
	}
	t.Cleanup(func() { _ = r1.Release() })

	r2, err := fsutil.Acquire(context.Background(), path, fsutil.LockShared, time.Second)
	if err != nil {
		t.Fatalf("second shared Acquire should not block: %v", err)
	}
	if err := r2.Release(); err != nil {
		t.Errorf("Release r2: %v", err)
	}
}
