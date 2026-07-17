package globalstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/globalstore"
)

// agedObject admits an object and backdates its metadata so it is past any
// grace period.
func agedObject(t *testing.T, s *globalstore.Store, body string) string {
	t.Helper()
	src, key := writeSkillDir(t, body)
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{
		SkillPath: "skills/demo", Version: "1.0.0", Commit: "c",
	}); err != nil {
		t.Fatal(err)
	}
	meta, err := globalstore.ReadMetadata(s.MetadataPath(key))
	if err != nil {
		t.Fatal(err)
	}
	meta.CreatedAt = time.Now().Add(-90 * 24 * time.Hour)
	meta.LastUsedAt = time.Time{}
	if err := globalstore.WriteMetadata(s.MetadataPath(key), meta); err != nil {
		t.Fatal(err)
	}
	return key
}

// TestGC_SafetyMatrix (spec 015 FR-024/025, US6 scenarios 2–5): referenced,
// pinned, recent, and invalid-metadata objects are never candidates; only an
// old, unreferenced, unpinned object is — and only --apply deletes it.
func TestGC_SafetyMatrix(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	referenced, pinned, deletable, recentKey, badMeta := buildGCFixture(t, s)
	marks := func() map[string]bool { return map[string]bool{referenced: true} }

	// Dry run: only the deletable object is proposed; nothing is removed.
	rep, err := s.GC(t.Context(), globalstore.GCOptions{MarkRefs: marks})
	if err != nil {
		t.Fatalf("GC dry-run: %v", err)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Key != deletable {
		t.Fatalf("candidates = %+v, want only %s", rep.Candidates, deletable)
	}
	if rep.ReclaimableBytes <= 0 {
		t.Error("ReclaimableBytes not reported")
	}
	if len(rep.Deleted) != 0 {
		t.Error("dry run deleted objects")
	}
	for _, key := range []string{referenced, pinned, deletable, recentKey, badMeta} {
		if !s.Has(key) {
			t.Fatalf("dry run removed %s", key)
		}
	}

	assertApplyDeletesOnlyDeletable(t, s, marks, deletable, []string{referenced, pinned, recentKey, badMeta})
}

// assertApplyDeletesOnlyDeletable runs GC apply and checks exactly the
// deletable object goes while every protected key survives.
func assertApplyDeletesOnlyDeletable(t *testing.T, s *globalstore.Store, marks func() map[string]bool, deletable string, protected []string) {
	t.Helper()
	rep, err := s.GC(t.Context(), globalstore.GCOptions{MarkRefs: marks, Apply: true})
	if err != nil {
		t.Fatalf("GC apply: %v", err)
	}
	if len(rep.Deleted) != 1 || rep.Deleted[0] != deletable {
		t.Errorf("Deleted = %v, want [%s]", rep.Deleted, deletable)
	}
	if s.Has(deletable) {
		t.Error("deletable object survived apply")
	}
	for _, key := range protected {
		if !s.Has(key) {
			t.Errorf("apply deleted protected object %s", key)
		}
	}
}

// TestGC_OlderThanOverride: a shorter per-run grace period exposes newer
// unreferenced objects.
func TestGC_OlderThanOverride(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# fresh but unreferenced\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}

	// Default grace: protected.
	rep, err := s.GC(t.Context(), globalstore.GCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Candidates) != 0 {
		t.Errorf("fresh object proposed under default grace: %+v", rep.Candidates)
	}
	// Zero-ish override: exposed.
	rep, err = s.GC(t.Context(), globalstore.GCOptions{GracePeriod: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Candidates) != 1 {
		t.Errorf("candidates = %+v, want the fresh object under --older-than 0", rep.Candidates)
	}
	if rep.Degraded != true {
		t.Error("nil MarkRefs did not report degraded reference marking")
	}
}

// TestGC_SkipsObjectWithHeldLock (FR-031, T050): an object mid-admission
// (its lock held by another process) is skipped as a gc-conflict, and GC
// continues.
func TestGC_SkipsObjectWithHeldLock(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	busy := agedObject(t, s, "# busy\n")
	gone := agedObject(t, s, "# gone\n")

	locker := globalstore.NewLocker(h, time.Second, nil)
	held, err := locker.LockObject(context.Background(), busy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()

	rep, err := s.GC(t.Context(), globalstore.GCOptions{Apply: true})
	if err != nil {
		t.Fatalf("GC with a held object lock: %v", err)
	}
	if s.Has(busy) != true {
		t.Error("GC deleted an object whose lock was held")
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != busy {
		t.Errorf("Skipped = %v, want [%s]", rep.Skipped, busy)
	}
	if s.Has(gone) {
		t.Error("GC did not continue past the conflict")
	}
}

// TestGC_RemarksUnderLockBeforeDelete: a reference appearing between the
// caller's scan and the locked delete protects the object.
func TestGC_RemarksUnderLockBeforeDelete(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	key := agedObject(t, s, "# late reference\n")

	calls := 0
	marks := func() map[string]bool {
		calls++
		if calls >= 2 {
			// The object became referenced after the first scan.
			return map[string]bool{key: true}
		}
		return map[string]bool{}
	}
	rep, err := s.GC(t.Context(), globalstore.GCOptions{MarkRefs: marks, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Has(key) {
		t.Fatal("GC deleted an object that became referenced before deletion")
	}
	if len(rep.Deleted) != 0 {
		t.Errorf("Deleted = %v, want none", rep.Deleted)
	}
}

// TestPins_RoundTrip (FR-026): pin/unpin/list and the unknown-hash error.
func TestPins_RoundTrip(t *testing.T) {
	t.Parallel()

	h := newTestHome(t)
	s := globalstore.New(h)
	src, key := writeSkillDir(t, "# pin me\n")
	if _, err := s.Admit(t.Context(), key, src, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}

	if err := s.Pin("sha256:unknown"); err == nil {
		t.Error("pinning an unknown object should fail")
	}
	if err := s.Pin(key); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	pins, err := s.Pins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0] != key {
		t.Errorf("Pins = %v, want [%s]", pins, key)
	}
	if !s.Pinned(key) {
		t.Error("Pinned = false after Pin")
	}
	if err := s.Unpin(key); err != nil {
		t.Fatalf("Unpin: %v", err)
	}
	if s.Pinned(key) {
		t.Error("Pinned = true after Unpin")
	}
	if err := s.Unpin(key); err != nil {
		t.Errorf("double Unpin should be a no-op: %v", err)
	}
}

// buildGCFixture seeds one object per sweep gate: referenced, pinned,
// deletable, inside-grace, and invalid-metadata.
func buildGCFixture(t *testing.T, s *globalstore.Store) (referenced, pinned, deletable, recentKey, badMeta string) {
	t.Helper()
	referenced = agedObject(t, s, "# referenced\n")
	pinned = agedObject(t, s, "# pinned\n")
	deletable = agedObject(t, s, "# deletable\n")
	recentSrc, rk := writeSkillDir(t, "# recent\n")
	if _, err := s.Admit(t.Context(), rk, recentSrc, globalstore.Origin{Commit: "c"}); err != nil {
		t.Fatal(err)
	}
	badMeta = agedObject(t, s, "# bad meta\n")
	if err := os.WriteFile(s.MetadataPath(badMeta), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Pin(pinned); err != nil {
		t.Fatal(err)
	}
	return referenced, pinned, deletable, rk, badMeta
}
