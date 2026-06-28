package selection_test

import (
	"errors"
	"testing"

	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/selection"
)

func sk(id, path string, valid bool) discovery.DiscoveredSkill {
	return discovery.DiscoveredSkill{ID: id, DisplayName: id, RepoPath: path, Valid: valid}
}

func result(skills ...discovery.DiscoveredSkill) discovery.Result {
	r := discovery.Result{Skills: skills}
	byID := map[string][]string{}
	for _, s := range skills {
		byID[s.ID] = append(byID[s.ID], s.RepoPath)
	}
	for id, paths := range byID {
		if len(paths) > 1 {
			r.Duplicates = append(r.Duplicates, discovery.DuplicateConflict{ID: id, Paths: paths})
		}
	}
	return r
}

func selectedIDs(skills []discovery.DiscoveredSkill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.ID
	}
	return out
}

func TestParse_Variants(t *testing.T) {
	t.Parallel()

	sels, err := selection.Parse([]string{"code-review", "shared@skills/a"}, false, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(sels) != 2 {
		t.Fatalf("got %d selectors, want 2", len(sels))
	}

	all, err := selection.Parse([]string{"*"}, false, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("wildcard parse: %v %v", all, err)
	}
	allFlag, err := selection.Parse(nil, true, "")
	if err != nil || len(allFlag) != 1 {
		t.Fatalf("--all parse: %v %v", allFlag, err)
	}
}

func TestResolve_OneByName(t *testing.T) {
	t.Parallel()

	res := result(sk("code-review", "skills/code-review", true), sk("writing", "skills/writing", true))
	sels, _ := selection.Parse([]string{"code-review"}, false, "")
	got, err := selection.Resolve(res, sels, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ids := selectedIDs(got); len(ids) != 1 || ids[0] != "code-review" {
		t.Errorf("got %v, want [code-review]", ids)
	}
}

func TestResolve_MultipleByName(t *testing.T) {
	t.Parallel()

	res := result(sk("code-review", "skills/code-review", true), sk("writing", "skills/writing", true))
	sels, _ := selection.Parse([]string{"code-review", "writing"}, false, "")
	got, err := selection.Resolve(res, sels, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d, want 2", len(got))
	}
}

func TestResolve_AllValid(t *testing.T) {
	t.Parallel()

	res := result(sk("a", "skills/a", true), sk("b", "skills/b", true), sk("bad", "skills/bad", false))
	sels, _ := selection.Parse(nil, true, "")
	got, err := selection.Resolve(res, sels, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ids := selectedIDs(got); len(ids) != 2 {
		t.Errorf("all should select only valid skills, got %v", ids)
	}
}

func TestResolve_AllRefusesOnDuplicate(t *testing.T) {
	t.Parallel()

	res := result(sk("shared", "skills/a/shared", true), sk("shared", "skills/b/shared", true))
	sels, _ := selection.Parse([]string{"*"}, false, "")
	if _, err := selection.Resolve(res, sels, false); !errors.Is(err, selection.ErrAmbiguousSelection) {
		t.Errorf("wildcard with duplicates should be ambiguous, got %v", err)
	}
}

func TestResolve_BareNameOnDuplicateFails(t *testing.T) {
	t.Parallel()

	res := result(sk("shared", "skills/a/shared", true), sk("shared", "skills/b/shared", true))
	sels, _ := selection.Parse([]string{"shared"}, false, "")
	if _, err := selection.Resolve(res, sels, false); !errors.Is(err, selection.ErrAmbiguousSelection) {
		t.Errorf("bare name on duplicate should be ambiguous, got %v", err)
	}
}

func TestResolve_PathQualifiedDuplicate(t *testing.T) {
	t.Parallel()

	res := result(sk("shared", "skills/a/shared", true), sk("shared", "skills/b/shared", true))
	sels, _ := selection.Parse([]string{"shared@skills/a/shared"}, false, "")
	got, err := selection.Resolve(res, sels, false)
	if err != nil {
		t.Fatalf("Resolve path-qualified: %v", err)
	}
	if len(got) != 1 || got[0].RepoPath != "skills/a/shared" {
		t.Errorf("got %v, want the skills/a/shared variant", selectedIDs(got))
	}
}

func TestResolve_NoMatchSuggests(t *testing.T) {
	t.Parallel()

	res := result(sk("code-review", "skills/code-review", true))
	sels, _ := selection.Parse([]string{"code-revoew"}, false, "")
	_, err := selection.Resolve(res, sels, false)
	var nm *selection.NoMatchError
	if !errors.As(err, &nm) {
		t.Fatalf("want NoMatchError, got %v", err)
	}
	if len(nm.Suggestions) == 0 || nm.Suggestions[0] != "code-review" {
		t.Errorf("suggestions = %v, want code-review", nm.Suggestions)
	}
}

func TestResolve_InvalidExplicitFails(t *testing.T) {
	t.Parallel()

	res := result(sk("broken", "skills/broken", false))
	sels, _ := selection.Parse([]string{"broken"}, false, "")
	if _, err := selection.Resolve(res, sels, false); !errors.Is(err, selection.ErrInvalidSelection) {
		t.Errorf("explicit invalid selection should fail, got %v", err)
	}
}

func TestResolve_DeduplicatesOverlap(t *testing.T) {
	t.Parallel()

	res := result(sk("code-review", "skills/code-review", true))
	sels, _ := selection.Parse([]string{"code-review", "code-review"}, false, "")
	got, err := selection.Resolve(res, sels, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("overlapping selectors should install once, got %d", len(got))
	}
}
