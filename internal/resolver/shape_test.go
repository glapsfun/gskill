package resolver_test

import (
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/resolver"
)

func TestClassifyDeclaration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		decl resolver.Declaration
		hint resolver.RefKind
		want resolver.DeclarationShape
	}{
		{"caret", resolver.Declaration{Version: "^1.2.0"}, "", resolver.ShapeRangeCaret},
		{"caret v-prefixed", resolver.Declaration{Version: "^v1.2.0"}, "", resolver.ShapeRangeCaret},
		{"tilde", resolver.Declaration{Version: "~1.2.0"}, "", resolver.ShapeRangeTilde},
		{"hyphen range", resolver.Declaration{Version: ">=1.0.0 <2.0.0"}, "", resolver.ShapeRangeOther},
		{"or range", resolver.Declaration{Version: "1.x || 2.x"}, "", resolver.ShapeRangeOther},
		{"partial caret", resolver.Declaration{Version: "^1.2"}, "", resolver.ShapeRangeOther},
		{"exact", resolver.Declaration{Version: "1.2.0"}, "", resolver.ShapeExactVersion},
		{"exact v-prefixed", resolver.Declaration{Version: "v1.2.0"}, "", resolver.ShapeExactVersion},
		{"exact equals", resolver.Declaration{Version: "=1.2.0"}, "", resolver.ShapeExactVersion},
		{"exact prerelease", resolver.Declaration{Version: "1.2.0-rc.1"}, "", resolver.ShapeExactVersion},
		{"tag by hint", resolver.Declaration{Ref: "v1.0.0"}, resolver.RefKindTag, resolver.ShapeTag},
		{"branch by hint", resolver.Declaration{Ref: "main"}, resolver.RefKindBranch, resolver.ShapeBranch},
		{"ref without hint is mutable", resolver.Declaration{Ref: "main"}, "", resolver.ShapeBranch},
		{"commit wins over version", resolver.Declaration{Version: "^1.0.0", Commit: "abc123"}, "", resolver.ShapeCommit},
		{"version wins over ref", resolver.Declaration{Version: "^1.0.0", Ref: "v9"}, resolver.RefKindTag, resolver.ShapeRangeCaret},
		{"local", resolver.Declaration{Version: "^1.0.0", Local: true}, "", resolver.ShapeLocal},
		{"unpinned", resolver.Declaration{}, "", resolver.ShapeUnpinned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolver.ClassifyDeclaration(tc.decl, tc.hint)
			if err != nil {
				t.Fatalf("ClassifyDeclaration: %v", err)
			}
			if got != tc.want {
				t.Errorf("shape = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyDeclaration_UnparseableVersion(t *testing.T) {
	t.Parallel()
	if _, err := resolver.ClassifyDeclaration(resolver.Declaration{Version: "not a version"}, ""); err == nil {
		t.Fatal("want an error for an unparseable constraint")
	}
}

func TestDeclarationShape_Pinned(t *testing.T) {
	t.Parallel()
	pinned := map[resolver.DeclarationShape]bool{
		resolver.ShapeExactVersion: true, resolver.ShapeTag: true, resolver.ShapeCommit: true,
		resolver.ShapeRangeCaret: false, resolver.ShapeRangeTilde: false, resolver.ShapeRangeOther: false,
		resolver.ShapeBranch: false, resolver.ShapeLocal: false, resolver.ShapeUnpinned: false,
	}
	for shape, want := range pinned {
		if got := shape.Pinned(); got != want {
			t.Errorf("%s.Pinned() = %v, want %v", shape, got, want)
		}
	}
}

// TestRewriteDeclaration_EmptyVersionRefused: FindRelease returns a candidate
// with an empty Version for a tag that is not semver. Rewriting a
// version-shaped declaration from such a candidate would write `version = ""`,
// which erases the pin and silently downgrades the next install to "latest".
// Every version shape must refuse it, as the tag and commit shapes already do.
func TestRewriteDeclaration_EmptyVersionRefused(t *testing.T) {
	t.Parallel()
	shapes := map[resolver.DeclarationShape]string{
		resolver.ShapeExactVersion: "1.0.0",
		resolver.ShapeRangeCaret:   "^1.0.0",
		resolver.ShapeRangeTilde:   "~1.0.0",
	}
	for shape, current := range shapes {
		key, value, err := resolver.RewriteDeclaration(shape, current, resolver.Candidate{Tag: "release-x", Commit: "abc123"})
		if err == nil {
			t.Errorf("%s: RewriteDeclaration(%q) = (%q, %q), want an error", shape, current, key, value)
			continue
		}
		if !errors.Is(err, resolver.ErrUnrewritable) {
			t.Errorf("%s: error = %v, want ErrUnrewritable", shape, err)
		}
	}
}
