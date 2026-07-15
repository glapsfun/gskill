package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestInstallFromLock_CancelBetweenSkills (spec 014 US4, FR-024/FR-025):
// cancelling after the first skill's terminal event stops the run between
// skills — the second entry is never attempted, the first stays installed
// and recorded in a valid lockfile, and the run maps to exit 130.
func TestInstallFromLock_CancelBetweenSkills(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rec eventRecorder
	res, err := lockApp().InstallFromLock(ctx, app.InstallFromLockRequest{
		Root:   root,
		Agents: []string{testAgent},
		Progress: func(e app.InstallProgressEvent) {
			rec.record(e)
			// Cancel the moment the first skill (alpha, sorted order) lands.
			if e.SkillName == "alpha" && e.Status.IsTerminal() {
				cancel()
			}
		},
	})
	if err == nil {
		t.Fatal("cancelled run returned nil error")
	}
	if !errors.Is(err, errs.ErrCancelled) {
		t.Errorf("run error = %v, want ErrCancelled", err)
	}
	if got := errs.ExitCode(err); got != int(errs.CodeCancelled) {
		t.Errorf("exit code = %d, want 130", got)
	}

	alpha := resultByName(t, res, "alpha")
	if alpha.Status != app.LockSkillInstalled {
		t.Errorf("alpha status = %q, want installed (completed work preserved)", alpha.Status)
	}
	beta := resultByName(t, res, "beta")
	if beta.Status != string(app.InstallStatusNotAttempted) {
		t.Errorf("beta status = %q, want not-attempted (never started)", beta.Status)
	}

	// The summary classifies the run as cancelled and the counters still sum.
	sum := app.Aggregate(res.Skills)
	if sum.Outcome != app.InstallOutcomeCancelled {
		t.Errorf("Outcome = %q, want cancelled", sum.Outcome)
	}
	if sum.Total != sum.Installed+sum.Repaired+sum.UpToDate+sum.Skipped+sum.Failed+sum.Cancelled+sum.NotAttempted+sum.Planned {
		t.Errorf("counter invariant violated: %+v", sum)
	}

	// beta still emitted exactly one terminal event so the progress bar
	// reaches 100% (contract guarantee 4).
	term := terminalBySkill(t, rec.skillEvents())
	if e, ok := term["beta"]; !ok || e.Status != app.InstallStatusNotAttempted {
		t.Errorf("beta terminal event = %+v, want not-attempted", e)
	}

	assertLockPersistedAfterCancel(t, root)
	assertAgentTargets(t, root, "alpha")
}

// assertLockPersistedAfterCancel checks that the interrupted run still wrote
// a valid lockfile recording the completed alpha entry (FR-024).
func assertLockPersistedAfterCancel(t *testing.T, root string) {
	t.Helper()
	l, err := skillslock.Load(filepath.Join(root, skillslock.FileName))
	if err != nil {
		t.Fatalf("lockfile invalid after cancellation: %v", err)
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("lockfile does not validate after cancellation: %v", err)
	}
	e, ok := l.Entry("alpha")
	if !ok || e.Ext == nil || e.Ext.StoreHash == "" {
		t.Errorf("alpha lock entry not enriched after cancellation: %+v", e)
	}
}

// TestInstallFromLock_CancelBeforeStart: a context cancelled before the run
// begins fails fast as cancelled with zero writes — no auto-init, no lock
// enrichment, no targets.
func TestInstallFromLock_CancelBeforeStart(t *testing.T) {
	t.Parallel()
	repo, hashAlpha, hashBeta := lockRepo(t)
	root := t.TempDir()
	writeLockOnly(t, root, repo, hashAlpha, hashBeta)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := lockApp().InstallFromLock(ctx, app.InstallFromLockRequest{
		Root: root, Agents: []string{testAgent},
	})
	if err == nil || !errors.Is(err, errs.ErrCancelled) {
		t.Fatalf("pre-cancelled run error = %v, want ErrCancelled", err)
	}
	if len(res.Skills) != 0 {
		t.Errorf("pre-cancelled run attempted %d skills, want 0", len(res.Skills))
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gskill")); statErr == nil {
		t.Error("pre-cancelled run auto-initialized the project (must write nothing)")
	}
}
