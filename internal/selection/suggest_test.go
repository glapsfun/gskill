package selection_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/selection"
)

func TestClosest(t *testing.T) {
	t.Parallel()

	cands := []string{"code-review", "writing", "kubernetes-ops"}
	got := selection.Closest("code-revoew", cands, 3)
	if len(got) == 0 || got[0] != "code-review" {
		t.Errorf("Closest(code-revoew) = %v, want code-review first", got)
	}
}

func TestClosest_NoneWithinThreshold(t *testing.T) {
	t.Parallel()

	got := selection.Closest("zzzzzzzz", []string{"code-review", "writing"}, 3)
	if len(got) != 0 {
		t.Errorf("Closest for a far-off target = %v, want none", got)
	}
}

func TestClosest_Deterministic(t *testing.T) {
	t.Parallel()

	cands := []string{"api", "apt", "abi"}
	a := selection.Closest("apl", cands, 3)
	b := selection.Closest("apl", cands, 3)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("non-deterministic order: %v vs %v", a, b)
		}
	}
}
