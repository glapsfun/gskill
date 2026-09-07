package skillslock_test

import (
	"bytes"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestDeclarationKindRoundTripsAndIsOptional: the shape survives the JSON
// bridge, and an entry written without it re-serializes byte-identically.
func TestDeclarationKindRoundTripsAndIsOptional(t *testing.T) {
	t.Parallel()

	with := fullLegacy()
	with.Requested.Kind = "range-caret"
	l := skillslock.New()
	l.SetEntry("deploy-to-vercel", skillslock.FromRecord(with))
	data, err := skillslock.Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"declarationKind": "range-caret"`)) {
		t.Fatalf("declarationKind missing from:\n%s", data)
	}
	l2, err := skillslock.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e, _ := l2.Entry("deploy-to-vercel")
	if got := skillslock.ToRecord("deploy-to-vercel", e).Requested.Kind; got != "range-caret" {
		t.Errorf("Kind = %q after round trip", got)
	}

	without := fullLegacy()
	l3 := skillslock.New()
	l3.SetEntry("deploy-to-vercel", skillslock.FromRecord(without))
	data3, err := skillslock.Marshal(l3)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(data3, []byte("declarationKind")) {
		t.Errorf("an entry without a kind must not gain the field:\n%s", data3)
	}
}
