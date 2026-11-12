package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/tui"
)

// TestDashboardService_InventoryMapsListAndPlan checks the adapter builds a
// DashboardSkill per installed skill, carrying the agents and a health status,
// with a repository identity for grouping (spec 033 T012, FR-001). It runs
// offline so discovery needs no network.
func TestDashboardService_InventoryMapsListAndPlan(t *testing.T) {
	t.Parallel()
	dir := installedAlphaProject(t)
	svc := &dashboardService{a: newTestApp(), root: dir, offline: true}

	skills, err := svc.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("Inventory returned no skills")
	}
	var alpha tui.DashboardSkill
	for _, got := range skills {
		if got.Name == "alpha" {
			alpha = got
		}
	}
	if alpha.Name != "alpha" {
		t.Fatalf("installed skill 'alpha' not found in inventory of %d", len(skills))
	}
	if alpha.Health == "" {
		t.Error("health (STATUS column) is empty")
	}
	if len(alpha.Agents) == 0 || alpha.Agents[0] != "claude" {
		t.Errorf("agents = %v, want [claude]", alpha.Agents)
	}
	if alpha.SourceLabel == "" {
		t.Error("source label (grouping) is empty")
	}
	if alpha.Markdown == "" {
		t.Error("SKILL.md preview was not preloaded")
	}
}

// TestDashboardService_PlanAndApplyMapToUpdateServices checks the adapter maps
// the update and version-change previews and applies onto App.Update,
// App.Upgrade, and App.ListVersions (spec 033 T019, FR-021).
func TestDashboardService_PlanAndApplyMapToUpdateServices(t *testing.T) {
	t.Parallel()
	a, root := interactiveUpdateProject(t)
	svc := &dashboardService{a: a, root: root}
	ctx := context.Background()

	vs, err := svc.Versions(ctx, "demo")
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if !slices.ContainsFunc(vs, func(v string) bool { return strings.Contains(v, "1.1.0") }) {
		t.Errorf("Versions = %v, want it to include the 1.1.0 release (App.ListVersions)", vs)
	}

	ch, err := svc.PlanUpdate(ctx, "demo")
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if ch.Kind != tui.ChangeUpdate || ch.FromRev != "1.0.0" || ch.ToRev != "1.1.0" {
		t.Errorf("PlanUpdate = %+v, want a compatible 1.0.0 → 1.1.0", ch)
	}

	vch, err := svc.PlanVersionChange(ctx, "demo", "1.1.0", false)
	if err != nil {
		t.Fatalf("PlanVersionChange: %v", err)
	}
	if vch.Kind != tui.ChangeVersion || vch.DeclAfter == "" {
		t.Errorf("PlanVersionChange = %+v, want a version change carrying the new declaration", vch)
	}
}

// TestDashboardService_ApplyUpdateRunsAppUpdate confirms ApplyUpdate applies the
// compatible update through App.Update (spec 033 T019).
func TestDashboardService_ApplyUpdateRunsAppUpdate(t *testing.T) {
	t.Parallel()
	a, root := interactiveUpdateProject(t)
	svc := &dashboardService{a: a, root: root}
	outs, err := svc.ApplyUpdate(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if len(outs) == 0 || outs[0].Name != "demo" {
		t.Errorf("ApplyUpdate outcomes = %+v, want one for demo", outs)
	}
	if got := lockedSkillVersion(t, root, "demo"); got != "1.1.0" {
		t.Errorf("after ApplyUpdate demo locked at %q, want 1.1.0", got)
	}
}

// TestDashboardService_ApplyVersionChangeMovesVersion confirms ApplyVersionChange
// runs App.Upgrade to the explicit target (spec 033 T019).
func TestDashboardService_ApplyVersionChangeMovesVersion(t *testing.T) {
	t.Parallel()
	a, root := interactiveUpdateProject(t)
	svc := &dashboardService{a: a, root: root}
	if _, err := svc.ApplyVersionChange(context.Background(), "demo", "1.1.0", false); err != nil {
		t.Fatalf("ApplyVersionChange: %v", err)
	}
	if got := lockedSkillVersion(t, root, "demo"); got != "1.1.0" {
		t.Errorf("after ApplyVersionChange demo locked at %q, want 1.1.0", got)
	}
}

// TestDashboardService_ApplyRemoveMapsToAppRemove checks a removal goes through
// App.Remove with the same declaration/lock/content effects as the CLI (T028).
func TestDashboardService_ApplyRemoveMapsToAppRemove(t *testing.T) {
	t.Parallel()
	dir := installedAlphaProject(t)
	svc := &dashboardService{a: newTestApp(), root: dir, offline: true}

	outs, err := svc.ApplyRemove(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("ApplyRemove: %v", err)
	}
	if len(outs) != 1 || outs[0].Result != "removed" {
		t.Errorf("ApplyRemove outcomes = %+v, want one removed", outs)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "skills", "alpha")); !os.IsNotExist(statErr) {
		t.Errorf("alpha agent dir not removed (stat err = %v)", statErr)
	}
}

// TestDashboardService_ApplyRemoveSurfacesOwnershipRefusal checks that an
// ownership refusal (locally modified, non-gskill-owned content) comes back as
// a failed outcome with nothing deleted (T028, FR-015).
func TestDashboardService_ApplyRemoveSurfacesOwnershipRefusal(t *testing.T) {
	t.Parallel()
	// A copy-mode install owns real content, so a local edit fails the
	// ownership check (a symlink install has nothing to tamper with).
	dir := lockOnlyProject(t)
	if _, stderr, code := runCLI(t, newTestApp(), "-C", dir, "install", "--agent", "claude", "--copy"); code != 0 {
		t.Fatalf("copy-mode install: code %d, stderr %q", code, stderr)
	}
	skillMD := filepath.Join(dir, ".claude", "skills", "alpha", "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("locally tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	svc := &dashboardService{a: newTestApp(), root: dir, offline: true}

	outs, err := svc.ApplyRemove(context.Background(), "alpha")
	if err == nil {
		t.Fatal("ApplyRemove on tampered content: want an ownership refusal, got nil")
	}
	if !errors.Is(err, errs.ErrInvalidLock) {
		t.Errorf("error = %v, want ErrInvalidLock", err)
	}
	if len(outs) == 0 || outs[0].Result != "failed" {
		t.Errorf("outcomes = %+v, want a failed outcome", outs)
	}
	if _, statErr := os.Stat(skillMD); statErr != nil {
		t.Errorf("content deleted on a refused removal: %v", statErr)
	}
}

// TestDashboardService_UpdateEquivalentToCLI is the equivalence spot-check: a
// dashboard update and `gskill update <skill>` leave skills.toml,
// skills-lock.json, and the installed content identical, and the refreshed
// dashboard reports the same per-skill status as App.List (T039, SC-003, SC-006).
func TestDashboardService_UpdateEquivalentToCLI(t *testing.T) {
	t.Parallel()
	a, root := interactiveUpdateProject(t)
	copyRoot := filepath.Join(t.TempDir(), "copy")
	if err := os.CopyFS(copyRoot, os.DirFS(root)); err != nil {
		t.Fatalf("copy project: %v", err)
	}

	svc := &dashboardService{a: a, root: root}
	if _, err := svc.ApplyUpdate(context.Background(), "demo"); err != nil {
		t.Fatalf("dashboard update: %v", err)
	}
	if _, stderr, code := runCLI(t, a, "-C", copyRoot, "update", "demo"); code != 0 {
		t.Fatalf("cli update: code %d, stderr %q", code, stderr)
	}

	if da, db := readProjectFile(t, root, "skills.toml"), readProjectFile(t, copyRoot, "skills.toml"); da != db {
		t.Errorf("skills.toml differs:\n dashboard:\n%s\n cli:\n%s", da, db)
	}
	if da, db := readLockNormalized(t, root), readLockNormalized(t, copyRoot); da != db {
		t.Errorf("skills-lock.json differs (timestamps normalized):\n dashboard:\n%s\n cli:\n%s", da, db)
	}
	dh := treeHash(t, filepath.Join(root, ".claude", "skills", "demo"))
	ch := treeHash(t, filepath.Join(copyRoot, ".claude", "skills", "demo"))
	if dh != ch {
		t.Errorf("installed content differs: dashboard %s, cli %s", dh, ch)
	}

	inv, err := svc.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	listed, err := a.List(context.Background(), root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	status := make(map[string]string, len(listed))
	for _, l := range listed {
		status[l.Name] = l.Status
	}
	for _, s := range inv {
		if s.Health != status[s.Name] {
			t.Errorf("%s: dashboard status %q, App.List status %q (SC-006)", s.Name, s.Health, status[s.Name])
		}
	}
}

// TestDashboardCmd_RequiresTTYAndKeepsTuiAlias confirms the non-interactive
// usage guard and the `gskill tui` alias are unchanged (T038, FR-022).
func TestDashboardCmd_RequiresTTYAndKeepsTuiAlias(t *testing.T) {
	t.Parallel()
	dir := initedProject(t)
	for _, name := range []string{"dashboard", "tui"} {
		_, stderr, code := runCLI(t, newTestApp(), "-C", dir, name)
		if code != int(errs.CodeUsage) {
			t.Errorf("%s: exit code = %d, want %d", name, code, errs.CodeUsage)
		}
		if !strings.Contains(stderr, "requires a TTY") || !strings.Contains(stderr, "gskill list") {
			t.Errorf("%s: stderr = %q, want the TTY usage guidance", name, stderr)
		}
	}
}

// readProjectFile returns a project file's contents.
func readProjectFile(t *testing.T, root, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

// lockTimestamps blanks the per-skill install/update timestamps so two lockfiles
// written at different moments compare on their substantive content.
var lockTimestamps = regexp.MustCompile(`"(installedAt|updatedAt)": "[^"]*"`)

// readLockNormalized returns skills-lock.json with its timestamps blanked.
func readLockNormalized(t *testing.T, root string) string {
	t.Helper()
	return lockTimestamps.ReplaceAllString(readProjectFile(t, root, "skills-lock.json"), `"$1": ""`)
}

// treeHash hashes a directory tree's relative paths and file contents so two
// installed trees can be compared byte-for-byte regardless of location.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	// A symlink-mode install points the agent dir at the active layer; resolve
	// it so the comparison hashes the real installed files.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	h := sha256.New()
	walkErr := filepath.WalkDir(resolved, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(resolved, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := os.Stat(path) // follows any inner symlink
		if infoErr != nil {
			return infoErr
		}
		if info.IsDir() {
			h.Write([]byte("d:" + rel + "\n"))
			return nil
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if readErr != nil {
			return readErr
		}
		h.Write([]byte("f:" + rel + ":"))
		h.Write(data)
		h.Write([]byte("\n"))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("hash tree %s: %v", dir, walkErr)
	}
	return hex.EncodeToString(h.Sum(nil))
}
