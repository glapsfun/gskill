package selfupdate_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

func TestCheckPlatformAndProbe(t *testing.T) {
	t.Parallel()

	fake := selfupdatetest.BuildFake(t, "0.10.0", "ok")
	if err := selfupdate.CheckPlatform(fake, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Errorf("CheckPlatform on a native binary: %v", err)
	}
	got, err := selfupdate.ProbeVersion(context.Background(), fake, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.10.0" {
		t.Errorf("ProbeVersion = %q, want 0.10.0", got)
	}
}

func TestCheckPlatform_Foreign(t *testing.T) {
	t.Parallel()

	otherArch := map[string]string{"amd64": "arm64", "arm64": "amd64"}[runtime.GOARCH]
	otherOS := map[string]string{"linux": "darwin", "darwin": "linux"}[runtime.GOOS]
	for _, tc := range []struct{ goos, goarch string }{{runtime.GOOS, otherArch}, {otherOS, runtime.GOARCH}} {
		fake := selfupdatetest.BuildFakeFor(t, tc.goos, tc.goarch, "0.10.0")
		err := selfupdate.CheckPlatform(fake, runtime.GOOS, runtime.GOARCH)
		want := "binary is for " + tc.goos + "/" + tc.goarch + ", this system is " + runtime.GOOS + "/" + runtime.GOARCH
		if err == nil || err.Error() != want || errs.ExitCode(err) != 6 {
			t.Errorf("CheckPlatform(%s/%s) = %v, want %q with exit 6", tc.goos, tc.goarch, err, want)
		}
	}

	text := filepath.Join(t.TempDir(), "gskill")
	if err := os.WriteFile(text, []byte("#!/bin/sh\necho hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := selfupdate.CheckPlatform(text, runtime.GOOS, runtime.GOARCH); errs.ExitCode(err) != 6 {
		t.Errorf("CheckPlatform(script) = %v, want exit 6", err)
	}
}

func TestProbeVersion_Failures(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"badjson", "fail", "hang"} {
		fake := selfupdatetest.BuildFake(t, "0.10.0", mode)
		start := time.Now()
		_, err := selfupdate.ProbeVersion(context.Background(), fake, 200*time.Millisecond)
		if errs.ExitCode(err) != 6 {
			t.Errorf("mode %s: err = %v, want exit 6", mode, err)
		}
		if mode == "hang" && time.Since(start) > 5*time.Second {
			t.Errorf("hang probe took %s, want the timeout to stop it", time.Since(start))
		}
	}
}

// A binary still open for writing elsewhere (a forked child can hold an
// inherited handle for a moment) fails to start with ETXTBSY on Linux; the
// probe waits for it instead of failing the upgrade.
func TestProbeVersion_RetriesWhileTheFileIsBusy(t *testing.T) {
	t.Parallel()

	exe := filepath.Join(t.TempDir(), "gskill")
	selfupdatetest.Copy(t, selfupdatetest.BuildFake(t, "0.10.0", "ok"), exe, 0o755)
	busy, err := os.OpenFile(exe, os.O_WRONLY, 0) //nolint:gosec // the test's own copy of the fake
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = busy.Close()
		close(released)
	}()
	got, err := selfupdate.ProbeVersion(context.Background(), exe, 10*time.Second)
	<-released
	if err != nil || got != "0.10.0" {
		t.Fatalf("ProbeVersion = %q, %v; want 0.10.0 once the file is released", got, err)
	}
}
