package resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
)

// fakeRunner is a canned git.Runner for resolver tests.
type fakeRunner struct {
	tags     []git.TagRef
	tagsErr  error
	heads    []git.BranchRef
	headsErr error
	refs     map[string]string // ref -> commit
	headSHA  string
}

func (f fakeRunner) LsRemoteTags(_ context.Context, _ string) ([]git.TagRef, error) {
	return f.tags, f.tagsErr
}

func (f fakeRunner) LsRemoteHeads(_ context.Context, _ string) ([]git.BranchRef, error) {
	return f.heads, f.headsErr
}

func (f fakeRunner) ResolveRef(_ context.Context, _, ref string) (string, error) {
	if ref == "HEAD" && f.headSHA != "" {
		return f.headSHA, nil
	}
	if sha, ok := f.refs[ref]; ok {
		return sha, nil
	}
	return "", errors.New("unknown ref")
}

func (f fakeRunner) FetchCommit(_ context.Context, _, _, _ string) error { return nil }

func gitRef() source.Ref {
	r, _ := source.Parse("github.com/acme/widgets")
	return r
}

func TestResolve_SemverPicksHighestMatching(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v1.0.0", Commit: "c1"},
		{Name: "v2.0.0", Commit: "c2"},
		{Name: "v2.1.3", Commit: "c3"},
		{Name: "v3.0.0", Commit: "c4"},
	}}

	rev, _, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{Version: "^2.0.0"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindSemver {
		t.Errorf("RefKind = %q, want semver", rev.RefKind)
	}
	if rev.Version != "2.1.3" || rev.Tag != "v2.1.3" || rev.Commit != "c3" {
		t.Errorf("resolved = %+v, want 2.1.3/v2.1.3/c3", rev)
	}
	if rev.MutableRef {
		t.Error("semver resolution must be immutable")
	}
}

func TestResolve_LatestPicksHighestTag(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v1.0.0", Commit: "c1"},
		{Name: "v3.0.0", Commit: "c4"},
	}}
	rev, _, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.Version != "3.0.0" || rev.Commit != "c4" {
		t.Errorf("resolved = %+v, want 3.0.0/c4", rev)
	}
}

func TestResolve_ExplicitTag(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{{Name: "v2.0.0", Commit: "c2"}}}
	rev, _, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{Ref: "v2.0.0"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindTag || rev.Commit != "c2" {
		t.Errorf("resolved = %+v, want tag/c2", rev)
	}
	if rev.MutableRef {
		t.Error("tag resolution must be immutable")
	}
}

func TestResolve_BranchIsMutableAndWarns(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{
		tags: []git.TagRef{{Name: "v1.0.0", Commit: "c1"}},
		refs: map[string]string{"main": "deadbeef"},
	}
	rev, warnings, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{Ref: "main"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindBranch || !rev.MutableRef {
		t.Errorf("resolved = %+v, want mutable branch", rev)
	}
	if len(warnings) == 0 {
		t.Error("expected a mutable-ref warning (SC-008)")
	}
}

func TestResolve_ExplicitCommit(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{}
	sha := "6c58cfd49a71d86d7d225c61ea63d98c3df19bd1"
	rev, _, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{Commit: sha})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindCommit || rev.Commit != sha || rev.MutableRef {
		t.Errorf("resolved = %+v, want immutable commit", rev)
	}
}

func TestResolve_NoTagsFallsBackToBranchWithWarning(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{headSHA: "headsha"}
	rev, warnings, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindBranch || !rev.MutableRef || rev.Commit != "headsha" {
		t.Errorf("resolved = %+v, want mutable branch at headsha", rev)
	}
	if len(warnings) == 0 {
		t.Error("expected unversioned fallback warning")
	}
}

func TestResolve_SemverNoMatch(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{{Name: "v1.0.0", Commit: "c1"}}}
	_, _, err := resolver.Resolve(context.Background(), runner, gitRef(), resolver.Requested{Version: "^5.0.0"})
	if err == nil {
		t.Fatal("expected no-match error")
	}
	if !errors.Is(err, resolver.ErrNoMatchingVersion) {
		t.Errorf("error = %v, want ErrNoMatchingVersion", err)
	}
}

func TestResolve_LocalSourceIsMutable(t *testing.T) {
	t.Parallel()

	local, _ := source.Parse("./my-skill")
	rev, _, err := resolver.Resolve(context.Background(), fakeRunner{}, local, resolver.Requested{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rev.RefKind != resolver.RefKindLocal || !rev.MutableRef {
		t.Errorf("resolved = %+v, want mutable local", rev)
	}
}
