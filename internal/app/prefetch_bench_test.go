package app_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

// BenchmarkInstallFromLock measures a fresh-clone install of S sources ×
// 3 skills with 5 ms simulated latency per network round trip. Baseline
// (pre-optimization): ~S×3 resolution round trips + S fetches, sequential.
// Target: ≤ ~2×S resolution round trips, parallel.
func BenchmarkInstallFromLock(b *testing.B) {
	const sources = 8
	root := projectWithAgent(b)
	seed := onboardApp()
	ctx := context.Background()
	for s := range sources {
		// Lock entries are keyed by skill name, so names must be unique
		// across sources.
		n := strconv.Itoa(s)
		repo := gitMultiSkillRepo(b, "src"+n, "gcs"+n, "gke"+n, "iam"+n)
		if _, err := seed.Add(ctx, app.AddRequest{Root: root, Source: repo, All: true}); err != nil {
			b.Fatalf("Add: %v", err)
		}
	}

	for b.Loop() {
		b.StopTimer()
		stripGskillExt(b, root)
		counting := &testutil.CountingGit{Inner: git.NewSystemRunner(), Latency: 5 * time.Millisecond}
		a := countingGitApp(b, counting)
		b.StartTimer()
		// Foreign entries (ext stripped) declare no agents; select explicitly.
		if _, err := a.InstallFromLock(ctx, app.InstallFromLockRequest{Root: root, Agents: []string{agent.DefaultID}}); err != nil {
			b.Fatalf("InstallFromLock: %v", err)
		}
	}
}
