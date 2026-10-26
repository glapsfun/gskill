package integrity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

func TestVerifyDir_MatchTamperAndSymlink(t *testing.T) {
	t.Parallel()

	content := t.TempDir()
	writeFile(t, content, "SKILL.md", []byte("# skill\n"), 0o600)

	hashes, err := integrity.HashDir(content)
	if err != nil {
		t.Fatal(err)
	}

	// Direct directory matches.
	ok, actual, err := integrity.VerifyDir(content, hashes.ContentHash)
	if err != nil || !ok {
		t.Fatalf("VerifyDir clean = ok:%v actual:%s err:%v", ok, actual.ContentHash, err)
	}

	// A symlink to the content resolves and still matches.
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(content, link); err != nil {
		t.Fatal(err)
	}
	ok, _, err = integrity.VerifyDir(link, hashes.ContentHash)
	if err != nil || !ok {
		t.Errorf("VerifyDir through symlink = ok:%v err:%v", ok, err)
	}

	// A single-byte tamper is detected.
	if err := os.WriteFile(filepath.Join(content, "SKILL.md"), []byte("# skill!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, _, err = integrity.VerifyDir(content, hashes.ContentHash)
	if err != nil {
		t.Fatalf("VerifyDir after tamper err: %v", err)
	}
	if ok {
		t.Error("VerifyDir did not detect tampered content")
	}
}

func TestHashes_Matches(t *testing.T) {
	t.Parallel()

	h := integrity.Hashes{ContentHash: "sha256:new", LegacyContentHash: "sha256:old"}
	cases := []struct {
		name     string
		hashes   integrity.Hashes
		recorded string
		want     bool
	}{
		{"empty recorded never matches", h, "", false},
		{"empty recorded against empty hashes", integrity.Hashes{}, "", false},
		{"current identity", h, "sha256:new", true},
		{"pre-028 identity", h, "sha256:old", true},
		{"anything else", h, "sha256:other", false},
	}
	for _, tc := range cases {
		if got := tc.hashes.Matches(tc.recorded); got != tc.want {
			t.Errorf("%s: Matches(%q) = %v, want %v", tc.name, tc.recorded, got, tc.want)
		}
	}
}
