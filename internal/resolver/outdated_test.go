package resolver_test

import (
	"context"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
)

func TestOutdated_SemverDetectsNewer(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v1.0.0", Commit: "c1"},
		{Name: "v1.3.0", Commit: "c2"},
	}}
	current := resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.0.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Version: "^1.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if !res.Available || res.Latest != "1.3.0" {
		t.Errorf("got %+v, want available latest 1.3.0", res)
	}
}

func TestOutdated_SemverUpToDate(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{{Name: "v1.3.0", Commit: "c2"}}}
	current := resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.3.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Version: "^1.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available {
		t.Errorf("got %+v, want up to date", res)
	}
}

func TestOutdated_CommitAndLocalNeverOutdated(t *testing.T) {
	t.Parallel()

	commit := resolver.Revision{RefKind: resolver.RefKindCommit, Commit: "abc"}
	if res, _ := resolver.Outdated(context.Background(), fakeRunner{}, gitRef(), resolver.Requested{}, commit); res.Available {
		t.Error("commit pin reported outdated")
	}

	local := resolver.Revision{RefKind: resolver.RefKindLocal}
	if res, _ := resolver.Outdated(context.Background(), fakeRunner{}, gitRef(), resolver.Requested{}, local); res.Available {
		t.Error("local pin reported outdated")
	}
}

func TestOutdated_TagComparesLatest(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v2.0.0", Commit: "c1"},
		{Name: "v2.1.0", Commit: "c2"},
	}}
	current := resolver.Revision{RefKind: resolver.RefKindTag, Tag: "v2.0.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "v2.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if !res.Available {
		t.Errorf("got %+v, want a newer tag available", res)
	}
}

func TestOutdated_BranchComparesHead(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{refs: map[string]string{"main": "newhead"}}
	current := resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main", Commit: "oldhead"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "main"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if !res.Available {
		t.Errorf("got %+v, want branch head moved", res)
	}
}
