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
		t.Fatalf("VerifyDir clean = ok:%v actual:%s err:%v", ok, actual, err)
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
