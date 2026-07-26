package resolver_test

import (
	"context"
	"errors"
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
	if !res.Available() || res.Latest != "1.3.0" {
		t.Errorf("got %+v, want available latest 1.3.0", res)
	}
	if res.Status != resolver.StatusUpdateAvailable {
		t.Errorf("status = %q, want %q", res.Status, resolver.StatusUpdateAvailable)
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
	if res.Available() {
		t.Errorf("got %+v, want up to date", res)
	}
	if res.Status != resolver.StatusUpToDate {
		t.Errorf("status = %q, want %q", res.Status, resolver.StatusUpToDate)
	}
}

func TestOutdated_SemverNoCompatibleUpdate(t *testing.T) {
	t.Parallel()

	// The highest satisfying tag is already locked, but a newer major exists
	// outside the constraint: not actionable, reported as informational only.
	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v1.3.0", Commit: "c2"},
		{Name: "v2.0.0", Commit: "c3"},
	}}
	current := resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.3.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Version: "^1.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() {
		t.Errorf("got %+v, want not actionable", res)
	}
	if res.Status != resolver.StatusNoCompatibleUpdate {
		t.Errorf("status = %q, want %q", res.Status, resolver.StatusNoCompatibleUpdate)
	}
	if res.Informational != "2.0.0" {
		t.Errorf("informational = %q, want 2.0.0", res.Informational)
	}
}

func TestOutdated_SemverNoSatisfyingTagAtAll(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{tags: []git.TagRef{{Name: "v9.0.0", Commit: "c9"}}}
	current := resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.3.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Version: "^1.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() || res.Status != resolver.StatusNoCompatibleUpdate {
		t.Errorf("got %+v, want no-compatible-update", res)
	}
	if res.Informational != "9.0.0" {
		t.Errorf("informational = %q, want 9.0.0", res.Informational)
	}
}

func TestOutdated_SemverNoTagsIsUpToDate(t *testing.T) {
	t.Parallel()

	res, err := resolver.Outdated(context.Background(), fakeRunner{}, gitRef(),
		resolver.Requested{Version: "^1.0.0"},
		resolver.Revision{RefKind: resolver.RefKindSemver, Version: "1.3.0"})
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() || res.Status != resolver.StatusUpToDate {
		t.Errorf("got %+v, want up to date", res)
	}
}

func TestOutdated_CommitAndLocalNeverOutdated(t *testing.T) {
	t.Parallel()

	commit := resolver.Revision{RefKind: resolver.RefKindCommit, Commit: "abc"}
	res, err := resolver.Outdated(context.Background(), fakeRunner{}, gitRef(), resolver.Requested{}, commit)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() || res.Status != resolver.StatusPinnedCommit {
		t.Errorf("commit pin: got %+v, want pinned-commit, not actionable", res)
	}

	local := resolver.Revision{RefKind: resolver.RefKindLocal}
	res, err = resolver.Outdated(context.Background(), fakeRunner{}, gitRef(), resolver.Requested{}, local)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() || res.Status != resolver.StatusLocalSource {
		t.Errorf("local source: got %+v, want local-source, not actionable", res)
	}
}

func TestOutdated_TagPinNeverActionable(t *testing.T) {
	t.Parallel()

	// Regression: an exact tag pin used to be compared against the highest
	// repository tag with no constraint and reported as an available update,
	// even though normal update preserves the pin and cannot apply it.
	runner := fakeRunner{tags: []git.TagRef{
		{Name: "v2.0.0", Commit: "c1"},
		{Name: "v2.1.0", Commit: "c2"},
	}}
	current := resolver.Revision{RefKind: resolver.RefKindTag, Tag: "v2.0.0"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "v2.0.0"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() {
		t.Errorf("got %+v, want exact tag pin never actionable", res)
	}
	if res.Status != resolver.StatusPinnedTag {
		t.Errorf("status = %q, want %q", res.Status, resolver.StatusPinnedTag)
	}
	if res.Informational != "2.1.0" {
		t.Errorf("informational = %q, want 2.1.0", res.Informational)
	}
	if res.Current != "v2.0.0" || res.Latest != "v2.0.0" {
		t.Errorf("got current %q latest %q, want the pin itself", res.Current, res.Latest)
	}
}

func TestOutdated_TagPinLookupErrorPropagates(t *testing.T) {
	t.Parallel()

	// A dead remote must not be reported as a healthy pin: the error
	// propagates and the caller decides how to classify (the app layer keeps
	// the pin classification and records the lookup failure).
	wantErr := errors.New("network down")
	runner := fakeRunner{tagsErr: wantErr}
	current := resolver.Revision{RefKind: resolver.RefKindTag, Tag: "v2.0.0"}

	_, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "v2.0.0"}, current)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want the lookup error propagated", err)
	}
}

func TestOutdated_BranchComparesHead(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{refs: map[string]string{"main": "newheadnewheadnewhead"}}
	current := resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main", Commit: "oldheadoldheadoldhead"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "main"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if !res.Available() || res.Status != resolver.StatusUpdateAvailable {
		t.Errorf("got %+v, want branch head moved actionable", res)
	}
	if len(res.Current) != 12 || len(res.Latest) != 12 {
		t.Errorf("got current %q latest %q, want 12-char short SHAs", res.Current, res.Latest)
	}
}

func TestOutdated_BranchUpToDate(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{refs: map[string]string{"main": "samehead"}}
	current := resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main", Commit: "samehead"}

	res, err := resolver.Outdated(context.Background(), runner, gitRef(), resolver.Requested{Ref: "main"}, current)
	if err != nil {
		t.Fatalf("Outdated: %v", err)
	}
	if res.Available() || res.Status != resolver.StatusUpToDate {
		t.Errorf("got %+v, want up to date", res)
	}
}
