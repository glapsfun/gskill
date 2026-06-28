package discovery

import "testing"

func TestNormalizeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"code-review", "code-review"},
		{"Convex Best Practices", "convex-best-practices"},
		{"code_review", "code-review"},
		{"Frontend  Design", "frontend-design"},
		{"--weird--", "weird"},
		{"MixedCASE", "mixedcase"},
		{"a.b.c", "a-b-c"},
	}
	for _, c := range cases {
		if got := normalizeID(c.in); got != c.want {
			t.Errorf("normalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeID_SpacedNameMatchesFolderID(t *testing.T) {
	t.Parallel()

	// A quoted display name with spaces must normalize to the same id as its
	// kebab-case folder, so selection by either resolves the same skill.
	if normalizeID("Convex Best Practices") != normalizeID("convex-best-practices") {
		t.Error("spaced display name and folder id must normalize equal")
	}
}

func TestHumanizeName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"code-review", "Code Review"},
		{"frontend_design", "Frontend Design"},
		{"writing", "Writing"},
	}
	for _, c := range cases {
		if got := humanizeName(c.in); got != c.want {
			t.Errorf("humanizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
