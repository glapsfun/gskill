package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/testutil"
)

// versionedRepo builds a git repo whose skills/gamma has two tagged versions,
// returning the repo path and each version's compat hash.
func versionedRepo(t *testing.T) (repo, hashV1, hashV2 string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	dir := filepath.Join(repo, "skills", "gamma")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(body string) {
		t.Helper()
		md := "---\nname: gamma\ndescription: The gamma skill\n---\n" + body
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "-b", "main")
	write("# gamma v1 " + t.Name() + "\n")
	var err error
	if hashV1, err = integrity.CompatHash(dir); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "v1")
	run("tag", "v1.0.0")

	write("# gamma v2 " + t.Name() + "\n")
	if hashV2, err = integrity.CompatHash(dir); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "v2")
	run("tag", "v2.0.0")
	return repo, hashV1, hashV2
}

// writeVersionLock writes a lockfile pinning skills/gamma at tag.
func writeVersionLock(t *testing.T, root, repo, tag, hash string) {
	t.Helper()
	lock := `{
  "version": 1,
  "skills": {
    "gamma": {
      "source": ` + jsonStr(repo) + `,
      "sourceType": "local",
      "skillPath": "skills/gamma/SKILL.md",
      "ref": "` + tag + `",
      "computedHash": "` + hash + `"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(root, skillslock.FileName), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
}

// digestTree hashes every file (path + bytes) under root, skipping .gskill.
func digestTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == ".gskill" && d.IsDir() {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		h.Write([]byte(rel))
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Write([]byte(target))
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if err != nil {
			return err
		}
		h.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestVersionIndependence_TwoProjectsTwoVersions is spec 015 US2 scenarios
// 1–3 and quickstart S2: different versions coexist as distinct global
// objects; updating one project leaves the other byte-identical; removing a
// skill from one project deletes no global content and leaves the other
// healthy.
func TestVersionIndependence_TwoProjectsTwoVersions(t *testing.T) {
	t.Parallel()

	h, a := globalHome(t)
	repo, hv1, hv2 := versionedRepo(t)

	repo1, repo2 := t.TempDir(), t.TempDir()
	writeVersionLock(t, repo1, repo, "v1.0.0", hv1)
	writeVersionLock(t, repo2, repo, "v2.0.0", hv2)

	for _, root := range []string{repo1, repo2} {
		if _, err := installLock(t, a, root, false); err != nil {
			t.Fatalf("install %s: %v", root, err)
		}
	}

	// (a) Two distinct objects; each project resolves its own version.
	objects := listStoreObjects(t, h)
	if len(objects) != 2 {
		t.Fatalf("store objects = %v, want 2 distinct versions", objects)
	}
	readActive := func(root string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "gamma", "SKILL.md")) //nolint:gosec // test-controlled temp path
		if err != nil {
			t.Fatalf("%s active content: %v", root, err)
		}
		return string(data)
	}
	if readActive(repo1) == readActive(repo2) {
		t.Error("repo1 and repo2 resolve identical content, want different versions")
	}

	// (b) Update repo1 to v2: repo2 stays byte-identical, and the old object
	// is not deleted (FR-010).
	repo2Before := digestTree(t, repo2)
	writeVersionLock(t, repo1, repo, "v2.0.0", hv2)
	if _, err := installLock(t, a, repo1, false); err != nil {
		t.Fatalf("update repo1: %v", err)
	}
	if got := digestTree(t, repo2); got != repo2Before {
		t.Error("updating repo1 changed repo2's tree")
	}
	if got := listStoreObjects(t, h); len(got) != 2 {
		t.Errorf("store objects after update = %v, want both versions retained", got)
	}
	if readActive(repo1) != readActive(repo2) {
		t.Error("after update both projects should resolve v2 content")
	}

	assertRemoveKeepsGlobalContent(t, a, h, repo1, repo2)
}

// assertRemoveKeepsGlobalContent removes gamma from repo1 and checks the
// global objects survive, repo2 stays healthy, and repo1's state entry is
// dropped (FR-009, FR-014, US2 scenario 3).
func assertRemoveKeepsGlobalContent(t *testing.T, a *app.App, h, repo1, repo2 string) {
	t.Helper()
	if _, err := a.Remove(context.Background(), repo1, []string{"gamma"}); err != nil {
		t.Fatalf("remove in repo1: %v", err)
	}
	if got := listStoreObjects(t, h); len(got) != 2 {
		t.Errorf("store objects after remove = %v, want global content untouched", got)
	}
	if _, err := os.Lstat(filepath.Join(repo1, ".agents", "skills", "gamma")); !os.IsNotExist(err) {
		t.Error("repo1 active link still present after remove")
	}
	if _, err := installLock(t, a, repo2, true); err != nil {
		t.Errorf("repo2 offline reinstall after repo1 remove: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repo1, ".gskill", "state.json")) //nolint:gosec // test-controlled temp path
	if err == nil && len(data) > 0 {
		var st struct {
			Skills map[string]any `json:"skills"`
		}
		if jsonErr := json.Unmarshal(data, &st); jsonErr == nil {
			if _, ok := st.Skills["gamma"]; ok {
				t.Error("repo1 state.json still records the removed skill")
			}
		}
	}
}
