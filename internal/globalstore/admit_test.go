package globalstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/globalstore"
)

func TestAdmit_HashMismatchFailsClosedLeavingNothing(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# real content\n")
	_ = key
	wrongKey := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	_, err := s.Admit(t.Context(), wrongKey, src, globalstore.Origin{Commit: "c"})
	if err == nil {
		t.Fatal("Admit with mismatched key succeeded")
	}
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("err = %v, want ErrIntegrity", err)
	}
	if s.Has(wrongKey) {
		t.Error("mismatched object became visible in the store")
	}
	if _, statErr := os.Stat(s.ObjectPath(wrongKey)); !os.IsNotExist(statErr) {
		t.Error("object dir husk left behind after failed admission")
	}
}

func TestAdmit_UnsafeSymlinkRejected(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}

	_, err := s.Admit(t.Context(), "sha256:whatever", dir, globalstore.Origin{})
	if err == nil {
		t.Fatal("Admit accepted content with an escaping symlink")
	}
	if s.Has("sha256:whatever") {
		t.Error("unsafe content became visible in the store")
	}
}

func TestAdmit_SecondCallReusesAndMergesOrigin(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# shared\n")

	reused, err := s.Admit(t.Context(), key, src, globalstore.Origin{Source: "org-a", Commit: "c1"})
	if err != nil {
		t.Fatalf("first Admit: %v", err)
	}
	if reused {
		t.Error("first Admit reported reused")
	}

	reused, err = s.Admit(t.Context(), key, src, globalstore.Origin{Source: "org-b", Commit: "c2"})
	if err != nil {
		t.Fatalf("second Admit: %v", err)
	}
	if !reused {
		t.Error("second Admit did not report reuse")
	}
	obj, err := s.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(obj.Metadata.Origins) != 2 {
		t.Errorf("origins = %+v, want both sources merged", obj.Metadata.Origins)
	}
}

// TestAdmit_ConcurrentSameObject drives two admissions of the same content in
// parallel: both must succeed and exactly one physical object must exist.
func TestAdmit_ConcurrentSameObject(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	src, key := writeSkillDir(t, "# raced\n")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := globalstore.New(h)
			s.SetLocker(globalstore.NewLocker(h, 10*time.Second, nil))
			_, errs[i] = s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("admitter %d: %v", i, err)
		}
	}
	s := globalstore.New(h)
	keys, err := s.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Errorf("store holds %d objects, want exactly 1", len(keys))
	}
}

// TestAdmit_AbandonedStagingIsDiscoverable simulates an interrupted admission
// (stray staging dir left in tmp/) and asserts the sweep helper finds it.
func TestAdmit_AbandonedStagingIsDiscoverable(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	stray := filepath.Join(h.TmpDir(), "object-deadbeef-12345")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stray, past, past); err != nil {
		t.Fatal(err)
	}

	stale, err := fsutil.ListStaleDirs(h.TmpDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0] != stray {
		t.Errorf("stale staging = %v, want [%s]", stale, stray)
	}
}
