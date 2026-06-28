package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/app"
)

func scanApp() *app.App {
	return app.New(app.Options{
		Agents: agent.NewDefaultRegistry(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// sourceTree builds a local source with skills at the given subpaths. A subpath
// whose leaf is "broken" gets a SKILL.md missing its description (invalid).
func sourceTree(t *testing.T, subpaths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, sp := range subpaths {
		dir := filepath.Join(root, filepath.FromSlash(sp))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(sp)
		body := "---\nname: " + name + "\ndescription: a skill\n---\n# " + name + "\n"
		if name == "broken" {
			body = "---\nname: broken\n---\n# broken\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSourceList_EnumeratesAll(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review", "skills/writing")
	res, err := scanApp().SourceList(context.Background(), src, app.ScanOptions{})
	if err != nil {
		t.Fatalf("SourceList: %v", err)
	}
	if len(res.Skills) != 2 {
		t.Errorf("got %d skills, want 2", len(res.Skills))
	}
}

func TestSourceList_ReadOnly(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review")
	before, _ := os.ReadDir(src)
	if _, err := scanApp().SourceList(context.Background(), src, app.ScanOptions{}); err != nil {
		t.Fatalf("SourceList: %v", err)
	}
	after, _ := os.ReadDir(src)
	if len(before) != len(after) {
		t.Error("SourceList must not modify the source tree")
	}
}

func TestSourceInspect_ShowsSkill(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review", "skills/writing")
	insp, err := scanApp().SourceInspect(context.Background(), src, "code-review", t.TempDir(), app.ScanOptions{})
	if err != nil {
		t.Fatalf("SourceInspect: %v", err)
	}
	if insp.Skill.ID != "code-review" {
		t.Errorf("inspected %q, want code-review", insp.Skill.ID)
	}
	if insp.Skill.RepoPath != "skills/code-review" {
		t.Errorf("repo path = %q", insp.Skill.RepoPath)
	}
}

func TestSourceCheck_ReportsProblems(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/ok", "skills/broken", "skills/a/dup", "skills/b/dup")
	report, err := scanApp().SourceCheck(context.Background(), src, app.ScanOptions{})
	if err != nil {
		t.Fatalf("SourceCheck: %v", err)
	}
	if !report.HasProblems() {
		t.Fatal("expected problems (invalid + duplicate)")
	}
	if len(report.Invalid) != 1 || report.Invalid[0].ID != "broken" {
		t.Errorf("invalid = %v, want [broken]", report.Invalid)
	}
	if len(report.Duplicates) != 1 || report.Duplicates[0].ID != "dup" {
		t.Errorf("duplicates = %v, want [dup]", report.Duplicates)
	}
}

func TestSourceCheck_CleanSource(t *testing.T) {
	t.Parallel()
	src := sourceTree(t, "skills/code-review", "skills/writing")
	report, err := scanApp().SourceCheck(context.Background(), src, app.ScanOptions{})
	if err != nil {
		t.Fatalf("SourceCheck: %v", err)
	}
	if report.HasProblems() {
		t.Errorf("clean source should have no problems: %+v", report)
	}
}
