package git_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/testutil"
)

// mutableRunner is a canned runner whose answers and error can be swapped
// mid-test to observe caching vs delegation.
type mutableRunner struct {
	mu    sync.Mutex
	tags  []git.TagRef
	sha   string
	err   error
	delay time.Duration
}

func (m *mutableRunner) set(tags []git.TagRef, sha string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tags, m.sha, m.err = tags, sha, err
}

func (m *mutableRunner) LsRemoteTags(context.Context, string) ([]git.TagRef, error) {
	time.Sleep(m.delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tags, m.err
}

func (m *mutableRunner) LsRemoteHeads(context.Context, string) ([]git.BranchRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, m.err
}

func (m *mutableRunner) ResolveRef(_ context.Context, _, ref string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	if git.IsFullSHA(ref) {
		return ref, nil
	}
	return m.sha, nil
}

func (m *mutableRunner) FetchCommit(context.Context, string, string, string) error { return nil }

func memoized(inner git.Runner) (*testutil.CountingGit, git.Runner) {
	c := &testutil.CountingGit{Inner: inner}
	return c, git.Memoize(c)
}

func TestMemoize_PassthroughWithoutArmedContext(t *testing.T) {
	t.Parallel()

	c, m := memoized(&mutableRunner{sha: "a"})
	ctx := context.Background() // no WithMemo
	for range 3 {
		if _, err := m.ResolveRef(ctx, "u", "main"); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Refs.Load(); got != 3 {
		t.Errorf("Refs = %d, want 3 (unarmed ctx must not cache)", got)
	}
}

func TestMemoize_CachesPerArmedContext(t *testing.T) {
	t.Parallel()

	inner := &mutableRunner{tags: []git.TagRef{{Name: "v1", Commit: "c1"}}, sha: "s1"}
	c, m := memoized(inner)
	ctx := git.WithMemo(context.Background())

	for range 3 {
		tags, err := m.LsRemoteTags(ctx, "u")
		if err != nil || len(tags) != 1 || tags[0].Commit != "c1" {
			t.Fatalf("tags = %v, %v", tags, err)
		}
		if _, err := m.ResolveRef(ctx, "u", "main"); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Tags.Load(); got != 1 {
		t.Errorf("Tags = %d, want 1", got)
	}
	if got := c.Refs.Load(); got != 1 {
		t.Errorf("Refs = %d, want 1", got)
	}

	// A second armed context is a fresh view: the memo must not leak across.
	if _, err := m.LsRemoteTags(git.WithMemo(context.Background()), "u"); err != nil {
		t.Fatal(err)
	}
	if got := c.Tags.Load(); got != 2 {
		t.Errorf("Tags = %d, want 2 (new ctx, new memo)", got)
	}
}

func TestMemoize_CachesErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("host down")
	inner := &mutableRunner{err: boom}
	c, m := memoized(inner)
	ctx := git.WithMemo(context.Background())

	for range 3 {
		if _, err := m.LsRemoteTags(ctx, "u"); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
	}
	if got := c.Tags.Load(); got != 1 {
		t.Errorf("Tags = %d, want 1 (error cached, one network attempt)", got)
	}
}

func TestFresh_BypassesMemoReadAndWrite(t *testing.T) {
	t.Parallel()

	inner := &mutableRunner{sha: "old0000000000000000000000000000000000000"}
	_, m := memoized(inner)
	ctx := git.WithMemo(context.Background())

	got1, err := m.ResolveRef(ctx, "u", "main")
	if err != nil {
		t.Fatal(err)
	}
	inner.set(nil, "new0000000000000000000000000000000000000", nil)

	fresh, err := git.Fresh(m).ResolveRef(ctx, "u", "main")
	if err != nil {
		t.Fatal(err)
	}
	if fresh == got1 {
		t.Errorf("Fresh returned the memoized answer %q; must re-ask the remote", fresh)
	}
	// The memoized view stays consistent for the rest of the run.
	again, err := m.ResolveRef(ctx, "u", "main")
	if err != nil {
		t.Fatal(err)
	}
	if again != got1 {
		t.Errorf("memoized answer changed mid-run: %q → %q", got1, again)
	}
}

func TestMemoize_ShaFastPathUncached(t *testing.T) {
	t.Parallel()

	c, m := memoized(&mutableRunner{})
	ctx := git.WithMemo(context.Background())
	sha := "0123456789abcdef0123456789abcdef01234567"
	for range 2 {
		if got, err := m.ResolveRef(ctx, "u", sha); err != nil || got != sha {
			t.Fatalf("ResolveRef(sha) = %q, %v", got, err)
		}
	}
	if got := c.Refs.Load(); got != 0 {
		t.Errorf("Refs = %d, want 0", got)
	}
}

func TestMemoize_SingleflightCollapsesConcurrentCalls(t *testing.T) {
	t.Parallel()

	inner := &mutableRunner{tags: []git.TagRef{{Name: "v1", Commit: "c1"}}, delay: 20 * time.Millisecond}
	c, m := memoized(inner)
	ctx := git.WithMemo(context.Background())

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = m.LsRemoteTags(ctx, "u")
		})
	}
	wg.Wait()
	if got := c.Tags.Load(); got != 1 {
		t.Errorf("Tags = %d, want 1 (singleflight)", got)
	}
}

func TestMemoize_Idempotent(t *testing.T) {
	t.Parallel()

	r := git.Memoize(&mutableRunner{})
	if git.Memoize(r) != r {
		t.Error("Memoize(Memoize(r)) must return the same wrapper")
	}
	ctx := git.WithMemo(context.Background())
	if git.WithMemo(ctx) != ctx {
		t.Error("WithMemo on an armed ctx must return it unchanged")
	}
}
