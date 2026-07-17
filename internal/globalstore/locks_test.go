package globalstore_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
)

func TestLockPaths_Naming(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	locks := globalstore.NewLocker(h, time.Second, nil)

	cases := []struct {
		got  string
		want string
	}{
		{locks.ObjectLockPath("sha256:abcd"), filepath.Join(h.LocksDir(), "store-sha256-abcd.lock")},
		{locks.GCLockPath(), filepath.Join(h.LocksDir(), "gc.lock")},
		{locks.RegistryLockPath(), filepath.Join(h.LocksDir(), "projects.lock")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("lock path = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestLocker_ExclusionAndRelease(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	locks := globalstore.NewLocker(h, 200*time.Millisecond, nil)

	l1, err := locks.LockObject(t.Context(), "sha256:abcd")
	if err != nil {
		t.Fatalf("first LockObject: %v", err)
	}
	// Second acquisition of the same object times out with the lock error.
	_, err = locks.LockObject(t.Context(), "sha256:abcd")
	if err == nil {
		t.Fatal("second LockObject succeeded while held")
	}
	if !errors.Is(err, errs.ErrCacheLock) {
		t.Errorf("lock timeout error = %v, want ErrCacheLock (code 12)", err)
	}

	// A different object locks fine in parallel.
	l2, err := locks.LockObject(t.Context(), "sha256:other")
	if err != nil {
		t.Fatalf("different-object LockObject: %v", err)
	}
	if err := l2.Release(); err != nil {
		t.Errorf("release l2: %v", err)
	}

	if err := l1.Release(); err != nil {
		t.Fatalf("release l1: %v", err)
	}
	l3, err := locks.LockObject(t.Context(), "sha256:abcd")
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	_ = l3.Release()
}

func TestLocker_WaitNoticeAfterContention(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	var buf bytes.Buffer
	// Notice threshold trips quickly for the test; timeout is generous.
	locks := globalstore.NewLockerWithNotice(h, 3*time.Second, &buf, 50*time.Millisecond)

	held, err := locks.LockObject(t.Context(), "sha256:busy")
	if err != nil {
		t.Fatalf("holder LockObject: %v", err)
	}
	release := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = held.Release()
		close(release)
	}()

	waiter, err := locks.LockObject(t.Context(), "sha256:busy")
	if err != nil {
		t.Fatalf("waiter LockObject: %v", err)
	}
	<-release
	_ = waiter.Release()

	if !strings.Contains(buf.String(), "waiting") {
		t.Errorf("no waiting notice emitted after contention; got %q", buf.String())
	}
}
