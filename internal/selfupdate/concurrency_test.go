package selfupdate_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

// blockedFixture serves v0.10.0 but holds the archive until release is closed.
func blockedFixture(t *testing.T) (upgradeFixture, chan struct{}) {
	t.Helper()
	good := readBin(t, selfupdatetest.BuildFake(t, newVersion, "ok"))
	release := make(chan struct{})
	name := selfupdate.ArchiveName(newVersion, runtime.GOOS, runtime.GOARCH)
	f := newUpgradeFixture(t, selfupdatetest.ServerOptions{
		Releases: []selfupdatetest.ReleaseSpec{spec(t, good, nil)},
		Block:    map[string]chan struct{}{name: release},
	}, good)
	return f, release
}

func startBlocked(t *testing.T, f upgradeFixture) (selfupdate.UpgradePlan, chan error) {
	t.Helper()
	p, err := selfupdate.Plan(context.Background(), f.env, selfupdate.PlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := selfupdate.Apply(context.Background(), f.env, p, nil)
		done <- err
	}()
	for !f.srv.AssetRequested(f.archive) {
		time.Sleep(5 * time.Millisecond)
	}
	return p, done
}

func TestApply_SecondRunTimesOutOnTheLock(t *testing.T) {
	t.Parallel()

	f, release := blockedFixture(t)
	p, done := startBlocked(t, f)

	second := f.env
	second.LockTimeout = 200 * time.Millisecond
	_, err := selfupdate.Apply(context.Background(), second, p, nil)
	if errs.ExitCode(err) != 12 || err.Error() != "another gskill self upgrade is in progress for "+f.exe {
		t.Errorf("second run: err = %v, want exit 12 in progress", err)
	}
	if errs.HintOf(err) != "wait for it to finish, then run gskill version" {
		t.Errorf("hint = %q", errs.HintOf(err))
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
}

func TestApply_SecondRunRechecksAfterWaiting(t *testing.T) {
	t.Parallel()

	f, release := blockedFixture(t)
	p, done := startBlocked(t, f)

	second := f.env
	second.LockTimeout = 10 * time.Second
	result := make(chan selfupdate.Outcome, 1)
	errc := make(chan error, 1)
	go func() {
		out, err := selfupdate.Apply(context.Background(), second, p, nil)
		result <- out
		errc <- err
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("second run: %v", err)
	}
	if out := <-result; out.Kind != selfupdate.OutcomeUpToDate || out.InstalledVersion != newVersion {
		t.Errorf("second run outcome = %s installed %s, want up_to_date at %s", out.Kind, out.InstalledVersion, newVersion)
	}
}
