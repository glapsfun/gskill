package resolver_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/resolver"
)

func TestRefKind_Valid(t *testing.T) {
	t.Parallel()

	for _, k := range []resolver.RefKind{
		resolver.RefKindSemver, resolver.RefKindTag, resolver.RefKindBranch,
		resolver.RefKindCommit, resolver.RefKindLocal,
	} {
		if !k.Valid() {
			t.Errorf("%q.Valid() = false, want true", k)
		}
	}
	if resolver.RefKind("head").Valid() {
		t.Error(`RefKind("head").Valid() = true, want false`)
	}
}

func TestRefKind_Mutable(t *testing.T) {
	t.Parallel()

	mutable := map[resolver.RefKind]bool{
		resolver.RefKindSemver: false,
		resolver.RefKindTag:    false,
		resolver.RefKindCommit: false,
		resolver.RefKindBranch: true,
		resolver.RefKindLocal:  true,
	}
	for k, want := range mutable {
		if got := k.Mutable(); got != want {
			t.Errorf("%q.Mutable() = %v, want %v", k, got, want)
		}
	}
}
