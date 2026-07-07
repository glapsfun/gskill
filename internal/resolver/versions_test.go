package resolver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
)

func TestListVersions_OrdersReleasesThenBranches(t *testing.T) {
	t.Parallel()

	runner := fakeRunner{
		tags: []git.TagRef{
			{Name: "v1.0.0", Commit: "c1"},
			{Name: "v2.1.0", Commit: "c3"},
			{Name: "v2.0.0", Commit: "c2"},
			{Name: "nightly", Commit: "c4"}, // non-semver tag
		},
		heads: []git.BranchRef{{Name: "main", Commit: "c5"}, {Name: "dev", Commit: "c6"}},
	}

	got, err := resolver.ListVersions(context.Background(), runner, source.Ref{Type: source.TypeGit, URL: "x"})
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}

	var kinds, names []string
	for _, v := range got {
		kinds = append(kinds, v.Kind)
		names = append(names, v.Name)
	}
	wantNames := []string{"v2.1.0", "v2.0.0", "v1.0.0", "nightly", "main", "dev"}
	if len(names) != len(wantNames) {
		t.Fatalf("names = %v, want %v", names, wantNames)
	}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Errorf("names[%d] = %q, want %q (all: %v)", i, names[i], wantNames[i], names)
		}
	}
	if kinds[0] != resolver.VersionKindRelease || kinds[3] != resolver.VersionKindTag || kinds[4] != resolver.VersionKindBranch {
		t.Errorf("kinds = %v, want releases then tags then branches", kinds)
	}
}

func TestListVersions_PropagatesListingErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network down")
	runner := fakeRunner{tagsErr: wantErr}
	_, err := resolver.ListVersions(context.Background(), runner, source.Ref{Type: source.TypeGit, URL: "x"})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v (the app layer degrades, the resolver reports)", err, wantErr)
	}
}
