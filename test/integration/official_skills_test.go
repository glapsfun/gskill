package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/metadata"
	"github.com/glapsfun/gskill/internal/testutil"
)

// The documented bootstrap and install commands for the official bundles
// (docs/how-to/use-gskill-from-an-agent.md). test/docs checks that the docs
// keep naming this tag.
const (
	officialTag        = "v0.13.0"
	officialArchiveDir = "gskill-0.13.0"
	officialRepoURL    = "https://github.com/glapsfun/gskill.git"
)

// bootstrapTarArgs are the documented tar arguments after `-C <dest>`.
var bootstrapTarArgs = []string{"--strip-components=2", officialArchiveDir + "/skills/gskill-install"}

// officialSkillsDir is the working tree's skills/ directory, the content the
// next release ships.
func officialSkillsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gskill-install", "SKILL.md")); err != nil {
		t.Fatalf("official bundles missing: %v", err)
	}
	return dir
}

// copyTree copies the regular files under src into dst, keeping their layout.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(p) //nolint:gosec // repository file
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// decoyPaths are skills that live in the GSKILL repository but are not
// official bundles: development skills, docs examples, and test fixtures.
var decoyPaths = []string{".agents/skills/decoy-dev", "examples/skills/decoy-example", "internal/x/testdata/decoy-fixture"}

func writeDecoys(t *testing.T, root string) {
	t.Helper()
	for _, p := range decoyPaths {
		dir := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(validSkill(filepath.Base(p))), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// officialRepoFixture builds a git repository shaped like the GSKILL
// repository: the working tree's official bundles under skills/ plus decoy
// skills elsewhere, committed and tagged.
func officialRepoFixture(t *testing.T, tag string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	copyTree(t, officialSkillsDir(t), filepath.Join(repo, "skills"))
	writeDecoys(t, repo)
	testutil.GitRun(t, repo, "init", "--quiet", "-b", "main")
	testutil.GitRun(t, repo, "add", ".")
	testutil.GitRun(t, repo, "commit", "--quiet", "-m", "release")
	testutil.GitRun(t, repo, "tag", tag)
	return repo
}

// rewriteGitHub points every git operation on the GSKILL repository URL at
// fixture, so the documented github.com source runs with no network. gskill
// keeps GIT_CONFIG_* in the environment of the git it runs. Callers must not
// be parallel.
func rewriteGitHub(t *testing.T, fixture string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+fixture+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", officialRepoURL)
}

// bootstrapArchive builds a .tar.gz laid out like GitHub's source archive of
// the release tag: everything under gskill-<version>/, with directory entries.
func bootstrapArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte, dir bool) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if dir {
			hdr = &tar.Header{Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add(officialArchiveDir, nil, true)
	add(officialArchiveDir+"/README.md", []byte("# gskill\n"), false)
	src := officialSkillsDir(t)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(src), p)
		if err != nil {
			return err
		}
		name := officialArchiveDir + "/" + filepath.ToSlash(rel)
		if d.IsDir() {
			add(name, nil, true)
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // repository file
		if err != nil {
			return err
		}
		add(name, data, false)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	decoy := officialArchiveDir + "/examples/skills/decoy-example"
	add(officialArchiveDir+"/examples", nil, true)
	add(officialArchiveDir+"/examples/skills", nil, true)
	add(decoy, nil, true)
	add(decoy+"/SKILL.md", []byte(validSkill("decoy-example")), false)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// extractSetupBundle runs the documented bootstrap tar command against a local
// copy of the release archive, as `curl ... | tar -xzf - -C dest ...` does.
func extractSetupBundle(t *testing.T, dest string) {
	t.Helper()
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-xzf", "-", "-C", dest}, bootstrapTarArgs...)
	cmd := exec.CommandContext(t.Context(), "tar", args...)
	cmd.Stdin = bytes.NewReader(bootstrapArchive(t))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar %v: %v\n%s", args, err, out)
	}
}

// listedFiles returns the regular files under root, slash-separated and sorted.
func listedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func TestOfficialBundlesBootstrap(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "skills")
	extractSetupBundle(t, dest)

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "gskill-install" {
		t.Fatalf("bootstrap wrote %v, want only gskill-install/", entries)
	}
	want := listedFiles(t, filepath.Join(officialSkillsDir(t), "gskill-install"))
	if got := listedFiles(t, filepath.Join(dest, "gskill-install")); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("bootstrap files = %v, want %v", got, want)
	}
	doc, err := metadata.Parse(readFile(t, filepath.Join(dest, "gskill-install", "SKILL.md")))
	if err != nil {
		t.Fatalf("bootstrapped SKILL.md: %v", err)
	}
	if doc.Frontmatter.Name != "gskill-install" {
		t.Errorf("name = %q, want gskill-install", doc.Frontmatter.Name)
	}
}

// lockedSkills reads skills-lock.json and returns each skill's resolved
// version, keyed by name. A tag pin records its ref instead of a version.
func lockedSkills(t *testing.T, root string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "skills-lock.json")) //nolint:gosec // test project
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Skills map[string]struct {
			Gskill struct {
				Version string `json:"version"`
				Ref     string `json:"ref"`
			} `json:"gskill"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("skills-lock.json: %v", err)
	}
	out := map[string]string{}
	for name, rec := range lock.Skills {
		out[name] = rec.Gskill.Version
		if out[name] == "" {
			out[name] = rec.Gskill.Ref
		}
	}
	return out
}

func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Not parallel: rewrites the GitHub URL and, for the global case, HOME.
func TestOfficialBundleSelection(t *testing.T) {
	rewriteGitHub(t, officialRepoFixture(t, officialTag))

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"usage-project", []string{"add", "github.com/glapsfun/gskill/skills/gskill", "--ref", officialTag, "--agent", "claude"}, []string{"gskill"}},
		{"setup-project", []string{"add", "github.com/glapsfun/gskill/skills/gskill-install", "--ref", officialTag, "--agent", "claude"}, []string{"gskill-install"}},
		{"both", []string{"add", "github.com/glapsfun/gskill", "--include", "skills/*", "--all", "--ref", officialTag, "--agent", "claude"}, []string{"gskill", "gskill-install"}},
	}
	//nolint:paralleltest // the parent rewrites the GitHub URL with t.Setenv
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := newProject(t)
			stdout, stderr, code := runGskill(t, proj, append([]string{"--no-interactive"}, tc.args...)...)
			if code != 0 {
				t.Fatalf("gskill %v: exit %d\nstdout: %s\nstderr: %s", tc.args, code, stdout, stderr)
			}
			locked := lockedSkills(t, proj)
			if got := sortedNames(locked); strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("installed %v, want %v", got, tc.want)
			}
			for _, name := range tc.want {
				if locked[name] != officialTag {
					t.Errorf("%s locked at %q, want %s", name, locked[name], officialTag)
				}
				if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", name, "SKILL.md")); err != nil {
					t.Errorf("%s not linked for claude: %v", name, err)
				}
			}
		})
	}

	t.Run("usage-global", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		proj := newProject(t)
		args := []string{"--no-interactive", "add", "github.com/glapsfun/gskill/skills/gskill", "--ref", officialTag, "--agent", "claude", "--global"}
		if stdout, stderr, code := runGskill(t, proj, args...); code != 0 {
			t.Fatalf("gskill %v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "gskill", "SKILL.md")); err != nil {
			t.Errorf("global install missing: %v", err)
		}
		// The project records the global install but holds none of its content.
		if v := lockedSkills(t, proj)["gskill"]; v != officialTag {
			t.Errorf("global install locked at %q, want %s", v, officialTag)
		}
		if _, err := os.Stat(filepath.Join(proj, ".agents", "skills", "gskill")); err == nil {
			t.Error("global install wrote skill content into the project")
		}
	})
}

// gskillJSON runs a gskill command expected to print one JSON object and
// returns it decoded, failing with the full output otherwise.
func gskillJSON(t *testing.T, proj string, wantCode int, args ...string) map[string]any {
	t.Helper()
	stdout, stderr, code := runGskill(t, proj, args...)
	if code != wantCode {
		t.Fatalf("gskill %v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, wantCode, stdout, stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("gskill %v: stdout is not one JSON object: %v\n%s", args, err, stdout)
	}
	return out
}

func gskillOK(t *testing.T, proj string, args ...string) {
	t.Helper()
	if stdout, stderr, code := runGskill(t, proj, args...); code != 0 {
		t.Fatalf("gskill %v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
}

// declaredVersion returns the version a skill declares in skills.toml, or ""
// when it declares none. Removing the last skill deletes skills.toml.
func declaredVersion(t *testing.T, proj, name string) string {
	t.Helper()
	m, err := manifest.Load(filepath.Join(proj, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		return ""
	}
	return m.Skills[name].Version
}

// TestUsageSkillLifecycle runs the bootstrap and then the usage skill's
// documented lifecycle (skills/gskill/references/workflows.md) in an isolated
// project, the flow FR-053 asks the gate to cover. Not parallel: sets HOME.
func TestUsageSkillLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	extractSetupBundle(t, filepath.Join(home, ".claude", "skills"))
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "gskill-install", "SKILL.md")); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	repo := testutil.InitSkillRepo(t, "demo", testutil.SkillBody("demo", "v1.0.0"), "v1.0.0")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v1.1.0"), "v1.1.0")
	testutil.PublishVersion(t, repo, "demo", testutil.SkillBody("demo", "v2.0.0"), "v2.0.0")
	proj := newProject(t)

	gskillOK(t, proj, "--no-interactive", "add", repo, "--version", "^1.0.0", "--agent", "claude")
	if v := lockedSkills(t, proj)["demo"]; v != "1.1.0" {
		t.Fatalf("add locked demo at %q, want 1.1.0", v)
	}
	declared := declaredVersion(t, proj, "demo")
	if declared == "" {
		t.Fatal("add did not declare demo's version in skills.toml")
	}

	gskillJSON(t, proj, 0, "--no-interactive", "--json", "list")
	gskillJSON(t, proj, 0, "--no-interactive", "--json", "info", "demo")
	gskillJSON(t, proj, 0, "--no-interactive", "--json", "update", "--list")

	gskillOK(t, proj, "--no-interactive", "update")
	if v := lockedSkills(t, proj)["demo"]; v != "1.1.0" {
		t.Errorf("update moved demo to %q; ^1.0.0 allows nothing past 1.1.0", v)
	}
	if got := declaredVersion(t, proj, "demo"); got != declared {
		t.Errorf("update changed the declared version %q -> %q", declared, got)
	}

	gskillJSON(t, proj, 0, "--no-interactive", "--json", "upgrade", "demo", "--to", "2.0.0")
	if v := lockedSkills(t, proj)["demo"]; v != "2.0.0" {
		t.Errorf("upgrade locked demo at %q, want 2.0.0", v)
	}
	if got := declaredVersion(t, proj, "demo"); got == declared {
		t.Errorf("upgrade left the declared version at %q", got)
	}

	gskillJSON(t, proj, 0, "--no-interactive", "--json", "project", "verify")

	gskillOK(t, proj, "--no-interactive", "remove", "demo", "--force")
	if _, ok := lockedSkills(t, proj)["demo"]; ok {
		t.Error("remove left demo in skills-lock.json")
	}
	if got := declaredVersion(t, proj, "demo"); got != "" {
		t.Errorf("remove left demo declared in skills.toml (version %q)", got)
	}
}
