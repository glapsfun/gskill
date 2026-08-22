package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectConfig_TakesEffect is spec 023 FR-002: a [config] table in
// skills.toml supplies project-scoped configuration, above the user file and
// below environment and flags. It travels with the repository, so a teammate
// cloning it gets the same effective settings with no local setup.
func TestProjectConfig_TakesEffect(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	if err := os.WriteFile(filepath.Join(proj, "skills.toml"),
		[]byte("[config]\nlog_level = \"debug\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runGskill(t, proj, "config", "list")
	if code != 0 {
		t.Fatalf("config list: exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "debug") {
		t.Errorf("project [config] had no effect on the effective configuration:\n%s", stdout)
	}
}

// TestProjectConfig_FlagsStillWin pins the precedence boundary: project
// configuration must not override an explicit flag.
func TestProjectConfig_FlagsStillWin(t *testing.T) {
	t.Parallel()

	proj := newProject(t)
	initProject(t, proj)
	if err := os.WriteFile(filepath.Join(proj, "skills.toml"),
		[]byte("[config]\nlog_level = \"debug\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// An unknown key alongside a known one must not fail the run either: the
	// manifest is committed and shared, so a key a newer gskill understands
	// cannot break an older one.
	if err := os.WriteFile(filepath.Join(proj, "skills.toml"),
		[]byte("[config]\nlog_level = \"debug\"\nnot_a_real_key = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runGskill(t, proj, "config", "list"); code != 0 {
		t.Errorf("unknown project config key must not fail the run: exit %d: %s", code, stderr)
	}
}

// TestProjectConfig_WarningsReachTheUser: the manifest's advisories (V3
// unknown config key, V9 ref and commit both declared) are computed during
// parsing, so they must actually be shown. A warning nobody sees is the same
// as no warning at all.
func TestProjectConfig_WarningsReachTheUser(t *testing.T) {
	t.Parallel()

	repo := gitRepo(t, validSkill("demo"), "v1.0.0")
	proj := newProject(t)
	initProject(t, proj)
	if _, stderr, code := runGskill(t, proj, "add", repo); code != 0 {
		t.Fatalf("add: %s", stderr)
	}

	// Declare an unknown configuration key alongside the generated entry.
	data, err := os.ReadFile(filepath.Join(proj, "skills.toml")) //nolint:gosec // test reads its own temp project
	if err != nil {
		t.Fatal(err)
	}
	out := "[config]\nnot_a_real_key = 1\n\n" + string(data)
	if err := os.WriteFile(filepath.Join(proj, "skills.toml"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runGskill(t, proj, "install")
	if code != 0 {
		t.Fatalf("install: exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "not_a_real_key") {
		t.Errorf("manifest warning never reached the user:\n%s", stderr)
	}
}
