package testutil_test

import (
	"context"
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

// cannedRunner is a minimal git.Runner returning fixed answers.
type cannedRunner struct{}

func (cannedRunner) LsRemoteTags(context.Context, string) ([]git.TagRef, error) {
	return []git.TagRef{{Name: "v1.0.0", Commit: "c1"}}, nil
}

func (cannedRunner) LsRemoteHeads(context.Context, string) ([]git.BranchRef, error) {
	return []git.BranchRef{{Name: "main", Commit: "c2"}}, nil
}

func (cannedRunner) ResolveRef(context.Context, string, string) (string, error) {
	return "c2", nil
}

func (cannedRunner) FetchCommit(context.Context, string, string, string) error { return nil }

func TestCountingGit_CountsEachNetworkMethod(t *testing.T) {
	t.Parallel()

	c := &testutil.CountingGit{Inner: cannedRunner{}}
	ctx := context.Background()
	if _, err := c.LsRemoteTags(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LsRemoteHeads(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResolveRef(ctx, "u", "main"); err != nil {
		t.Fatal(err)
	}
	if err := c.FetchCommit(ctx, "u", "c2", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := c.ResolutionCalls(); got != 3 {
		t.Errorf("ResolutionCalls = %d, want 3", got)
	}
	if got := c.Fetches.Load(); got != 1 {
		t.Errorf("Fetches = %d, want 1", got)
	}
}

func TestCountingGit_ShaResolveIsNotANetworkCall(t *testing.T) {
	t.Parallel()

	c := &testutil.CountingGit{Inner: cannedRunner{}}
	sha := "0123456789abcdef0123456789abcdef01234567"
	if _, err := c.ResolveRef(context.Background(), "u", sha); err != nil {
		t.Fatal(err)
	}
	if got := c.Refs.Load(); got != 0 {
		t.Errorf("Refs = %d, want 0 (SHA fast-path is local)", got)
	}
}

func TestCountingGit_FailInjectsErrorAfterCounting(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	c := &testutil.CountingGit{Inner: cannedRunner{}, Fail: boom}
	if _, err := c.LsRemoteTags(context.Background(), "u"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := c.Tags.Load(); got != 1 {
		t.Errorf("Tags = %d, want 1", got)
	}
}
