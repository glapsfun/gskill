package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/skillslock"
)

// TestCheckPlanFresh: PlanUpgrade runs before the project mutation lock is
// taken, so a competing process can rewrite a declaration while this run waits
// for the lock. Applying the plan blindly would write the older declaration
// back and silently undo the newer tracking choice, so the apply step compares
// the declaration on disk against the one it planned from.
func TestCheckPlanFresh(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, decl string) string {
		t.Helper()
		root := t.TempDir()
		body := "[skills.demo]\nsource = \"github.com/acme/demo\"\n" + decl + "\n"
		if err := os.WriteFile(filepath.Join(root, "skills.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return root
	}
	rec := skillslock.Record{Source: skillslock.Source{Original: "github.com/acme/demo"}}

	t.Run("unchanged declaration applies", func(t *testing.T) {
		t.Parallel()
		a := New(Options{})
		p := openProject(write(t, `version = "^1.2.0"`))
		it := UpgradePlanItem{Name: "demo", CurrentDecl: `version = "^1.2.0"`}
		if err := a.checkPlanFresh(p, it, rec); err != nil {
			t.Errorf("fresh plan refused: %v", err)
		}
	})

	t.Run("declaration changed since planning is refused", func(t *testing.T) {
		t.Parallel()
		a := New(Options{})
		// The plan was built from a caret range; another process has since
		// pinned the skill exactly.
		p := openProject(write(t, `version = "1.3.0"`))
		it := UpgradePlanItem{Name: "demo", CurrentDecl: `version = "^1.2.0"`}
		err := a.checkPlanFresh(p, it, rec)
		if err == nil {
			t.Fatal("a stale plan was applied over a newer declaration")
		}
		for _, want := range []string{"demo", "^1.2.0", "1.3.0"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})
}

// TestCheckPlanFresh_DefeatsManifestCache is the case the comparison test
// cannot reach. loadManifest memoizes on existence, size, and mtime, so a
// competing writer that changes a declaration to a value of the same length
// within one mtime tick is invisible to a cached read — exactly the race this
// check exists to catch. The check must therefore read past its own cache.
func TestCheckPlanFresh_DefeatsManifestCache(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "skills.toml")
	const planned = `version = "^1.2.0"`
	// Same byte length as planned, so only the content differs.
	const concurrent = `version = "^1.3.0"`
	write := func(t *testing.T, decl string) {
		t.Helper()
		body := "[skills.demo]\nsource = \"github.com/acme/demo\"\n" + decl + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a := New(Options{})
	p := openProject(root)
	rec := skillslock.Record{Source: skillslock.Source{Original: "github.com/acme/demo"}}
	it := UpgradePlanItem{Name: "demo", CurrentDecl: planned}

	write(t, planned)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Plan against this declaration, which also warms the cache.
	if err := a.checkPlanFresh(p, it, rec); err != nil {
		t.Fatalf("fresh plan refused: %v", err)
	}

	// Another process rewrites the declaration; the file keeps its length, and
	// its mtime lands in the same tick.
	write(t, concurrent)
	stamp := stat.ModTime()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if after, sErr := os.Stat(path); sErr != nil {
		t.Fatal(sErr)
	} else if after.Size() != stat.Size() || !after.ModTime().Equal(stamp) {
		t.Fatalf("setup failed to hide the edit: size %d->%d, mtime %v->%v",
			stat.Size(), after.Size(), stamp, after.ModTime())
	}

	err = a.checkPlanFresh(p, it, rec)
	if err == nil {
		t.Fatal("a concurrent same-length edit went undetected — the stale plan would be applied")
	}
	if !strings.Contains(err.Error(), "^1.3.0") {
		t.Errorf("error %q does not report the declaration now on disk", err)
	}
}
