package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// eventRecorder captures the full install-event stream. Emission is
// synchronous and sequential (contracts/install-progress-events.md), so no
// locking is needed when the run itself is driven synchronously.
type eventRecorder struct {
	events []app.InstallProgressEvent
}

func (r *eventRecorder) record(e app.InstallProgressEvent) {
	r.events = append(r.events, e)
}

// skillEvents returns the recorded events carrying a skill name (run-scoped
// events like the locking phase have SkillName == "" and are excluded).
func (r *eventRecorder) skillEvents() []app.InstallProgressEvent {
	var out []app.InstallProgressEvent
	for _, e := range r.events {
		if e.SkillName != "" {
			out = append(out, e)
		}
	}
	return out
}

// terminalBySkill returns each skill's single terminal event, failing the test
// on zero or duplicate terminal events for any skill (contract guarantee 1).
func terminalBySkill(t *testing.T, events []app.InstallProgressEvent) map[string]app.InstallProgressEvent {
	t.Helper()
	term := make(map[string]app.InstallProgressEvent)
	for _, e := range events {
		if !e.Status.IsTerminal() {
			continue
		}
		if prev, dup := term[e.SkillName]; dup {
			t.Errorf("skill %q emitted two terminal events: %q then %q", e.SkillName, prev.Status, e.Status)
		}
		term[e.SkillName] = e
	}
	return term
}

// assertStreamInvariants checks the cross-skill guarantees: 1-based
// non-decreasing SkillIndex, stable SkillTotal, per-skill phase monotonicity,
// and Err only on failed/cancelled terminal events (guarantees 2, 5, and the
// phase-order rule).
func assertStreamInvariants(t *testing.T, events []app.InstallProgressEvent, wantTotal int) {
	t.Helper()
	lastIndex := 0
	lastRank := make(map[string]int)
	for i, e := range events {
		if e.SkillIndex < 1 || e.SkillIndex > wantTotal {
			t.Errorf("event %d: SkillIndex = %d, want 1..%d", i, e.SkillIndex, wantTotal)
		}
		if e.SkillIndex < lastIndex {
			t.Errorf("event %d: SkillIndex decreased %d -> %d", i, lastIndex, e.SkillIndex)
		}
		lastIndex = e.SkillIndex
		if e.SkillTotal != wantTotal {
			t.Errorf("event %d: SkillTotal = %d, want %d", i, e.SkillTotal, wantTotal)
		}
		if r, prev := e.Phase.Rank(), lastRank[e.SkillName]; r >= 0 && r < prev {
			t.Errorf("event %d: skill %q phase went backwards to %q", i, e.SkillName, e.Phase)
		} else if r >= 0 {
			lastRank[e.SkillName] = r
		}
		if e.Err != nil && e.Status != app.InstallStatusFailed && e.Status != app.InstallStatusCancelled {
			t.Errorf("event %d: Err set on status %q", i, e.Status)
		}
	}
}

func TestInstallFromLock_ProgressEvents_AllSuccess(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	var rec eventRecorder
	res, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root:     root,
		Agents:   []string{testAgent},
		Progress: rec.record,
	})
	if err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	if len(res.Skills) != 2 {
		t.Fatalf("results = %d skills, want 2", len(res.Skills))
	}

	skillEvents := rec.skillEvents()
	if len(skillEvents) == 0 {
		t.Fatal("no per-skill progress events emitted")
	}
	assertStreamInvariants(t, skillEvents, 2)

	term := terminalBySkill(t, skillEvents)
	for _, name := range []string{"alpha", "beta"} {
		e, ok := term[name]
		if !ok {
			t.Errorf("skill %q emitted no terminal event", name)
			continue
		}
		if e.Status != app.InstallStatusInstalled {
			t.Errorf("skill %q terminal status = %q, want installed", name, e.Status)
		}
		if e.Phase != app.InstallPhaseComplete {
			t.Errorf("skill %q terminal phase = %q, want complete", name, e.Phase)
		}
		if e.Source == "" {
			t.Errorf("skill %q terminal event has empty Source", name)
		}
	}

	// The run itself records the lock: a run-scoped locking event with no
	// skill name (guarantee 6).
	var sawLocking bool
	for _, e := range rec.events {
		if e.Phase == app.InstallPhaseLocking {
			sawLocking = true
			if e.SkillName != "" {
				t.Errorf("locking event carries SkillName %q, want empty", e.SkillName)
			}
		}
	}
	if !sawLocking {
		t.Error("no run-scoped locking event emitted")
	}
}

func TestInstallFromLock_ProgressEvents_UpToDateFastPath(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	a := lockApp()
	if _, err := a.InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent},
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	var rec eventRecorder
	if _, err := a.InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Progress: rec.record,
	}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	skillEvents := rec.skillEvents()
	assertStreamInvariants(t, skillEvents, 2)
	term := terminalBySkill(t, skillEvents)
	for _, name := range []string{"alpha", "beta"} {
		e, ok := term[name]
		if !ok {
			t.Errorf("skill %q emitted no terminal event on the fast path", name)
			continue
		}
		if e.Status != app.InstallStatusUpToDate {
			t.Errorf("skill %q terminal status = %q, want up-to-date", name, e.Status)
		}
	}
}

func TestInstallFromLock_ProgressEvents_EmptyLock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lock := "{\n  \"version\": 1,\n  \"skills\": {}\n}\n"
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}

	var rec eventRecorder
	if _, err := lockApp().InstallFromLock(context.Background(), app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent}, Progress: rec.record,
	}); err != nil {
		t.Fatalf("InstallFromLock: %v", err)
	}
	if got := rec.skillEvents(); len(got) != 0 {
		t.Errorf("empty lock emitted %d per-skill events, want 0", len(got))
	}
}
