package resolver_test

import (
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
)

func TestNewestBeyond(t *testing.T) {
	t.Parallel()
	tags := []git.TagRef{
		{Name: "v1.0.0", Commit: "c1"},
		{Name: "v1.4.0", Commit: "c2"},
		{Name: "v2.0.0", Commit: "c3"},
		{Name: "v3.0.0-rc.1", Commit: "c4"},
	}
	c, ok := resolver.NewestBeyond(tags, "1.4.0", "^1.0.0")
	if !ok || c.Version != "2.0.0" || c.Tag != "v2.0.0" || c.Commit != "c3" {
		t.Errorf("got %+v %v, want 2.0.0 (pre-release skipped)", c, ok)
	}
	if c, ok := resolver.NewestBeyond(tags, "2.0.0", "^2.0.0"); ok {
		t.Errorf("nothing newer than 2.0.0, got %+v", c)
	}
	if c, ok := resolver.NewestBeyond(tags, "2.0.0", "3.0.0-rc.0"); !ok || c.Version != "3.0.0-rc.1" {
		t.Errorf("a pre-release declaration admits pre-releases, got %+v %v", c, ok)
	}
	if c, ok := resolver.NewestBeyond(tags, "", ""); !ok || c.Version != "2.0.0" {
		t.Errorf("no current version: newest wins, got %+v %v", c, ok)
	}
	if _, ok := resolver.NewestBeyond(nil, "1.0.0", "^1.0.0"); ok {
		t.Error("no tags must yield no candidate")
	}
}

func TestFindRelease(t *testing.T) {
	t.Parallel()
	tags := []git.TagRef{{Name: "v1.2.0", Commit: "c1"}, {Name: "release-x", Commit: "c9"}}
	if c, ok := resolver.FindRelease(tags, "1.2.0"); !ok || c.Commit != "c1" || c.Tag != "v1.2.0" {
		t.Errorf("by version: %+v %v", c, ok)
	}
	if c, ok := resolver.FindRelease(tags, "release-x"); !ok || c.Commit != "c9" {
		t.Errorf("by name: %+v %v", c, ok)
	}
	if _, ok := resolver.FindRelease(tags, "9.9.9"); ok {
		t.Error("missing release must not be found")
	}
}

func TestRewriteDeclaration(t *testing.T) {
	t.Parallel()
	c := resolver.Candidate{Version: "2.1.0", Tag: "v2.1.0", Commit: "deadbeef"}
	cases := []struct {
		shape          resolver.DeclarationShape
		current        string
		wantKey, wantV string
	}{
		{resolver.ShapeRangeCaret, "^1.0.0", "version", "^2.1.0"},
		{resolver.ShapeRangeCaret, "^v1.0.0", "version", "^v2.1.0"},
		{resolver.ShapeRangeTilde, "~1.0.0", "version", "~2.1.0"},
		{resolver.ShapeExactVersion, "1.0.0", "version", "2.1.0"},
		{resolver.ShapeExactVersion, "v1.0.0", "version", "v2.1.0"},
		{resolver.ShapeTag, "v1.0.0", "ref", "v2.1.0"},
		{resolver.ShapeCommit, "0123abc", "commit", "deadbeef"},
	}
	for _, tc := range cases {
		key, v, err := resolver.RewriteDeclaration(tc.shape, tc.current, c)
		if err != nil || key != tc.wantKey || v != tc.wantV {
			t.Errorf("%s %q: got %s=%q err=%v, want %s=%q", tc.shape, tc.current, key, v, err, tc.wantKey, tc.wantV)
		}
	}
	for _, shape := range []resolver.DeclarationShape{resolver.ShapeRangeOther, resolver.ShapeBranch, resolver.ShapeLocal, resolver.ShapeUnpinned} {
		if _, _, err := resolver.RewriteDeclaration(shape, "x", c); !errors.Is(err, resolver.ErrUnrewritable) {
			t.Errorf("%s: err = %v, want ErrUnrewritable", shape, err)
		}
	}
	if _, _, err := resolver.RewriteDeclaration(resolver.ShapeCommit, "abc", resolver.Candidate{Version: "1.0.0"}); !errors.Is(err, resolver.ErrUnrewritable) {
		t.Errorf("commit shape without a commit: err = %v", err)
	}
}
