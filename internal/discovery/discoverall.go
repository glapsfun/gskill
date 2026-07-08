package discovery

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/metadata"
)

// Severity classifies a Diagnostic. An error-severity diagnostic marks a skill
// invalid (not installable by default); a warning does not.
type Severity string

const (
	// SeverityError marks a defect that makes a skill invalid.
	SeverityError Severity = "error"
	// SeverityWarning marks a non-fatal advisory (e.g. a name/folder mismatch).
	SeverityWarning Severity = "warning"
)

// Diagnostic is a single validation finding about a discovered skill.
type Diagnostic struct {
	Severity Severity
	Message  string
	Path     string // in-repo path the diagnostic concerns (forward-slash)
}

// DiscoveredSkill is one skill folder found by scanning a source (FR-001).
type DiscoveredSkill struct {
	ID          string // normalized, folder-derived identity (R2)
	DisplayName string // frontmatter name, else humanized folder name (FR-007)
	Description string
	RepoPath    string // skill dir relative to source root, forward-slash; "" at root
	Dir         string // absolute path to the skill folder
	SkillFile   string // absolute path to the SKILL.md
	Frontmatter metadata.Frontmatter
	Valid       bool
	Problems    []Diagnostic
}

// FirstError returns the message of the skill's first error-severity
// diagnostic, or "" when none exists. It is the single definition of "the
// reason a skill is invalid" shared by the app and TUI layers.
func (s DiscoveredSkill) FirstError() string {
	for _, p := range s.Problems {
		if p.Severity == SeverityError {
			return p.Message
		}
	}
	return ""
}

// DuplicateConflict records two or more discovered skills whose normalized ids
// collide (FR-011).
type DuplicateConflict struct {
	ID    string
	Paths []string // all participating in-repo paths, sorted
}

// Result is the deterministic outcome of scanning one source (FR-009).
type Result struct {
	Skills      []DiscoveredSkill
	Duplicates  []DuplicateConflict
	Diagnostics []Diagnostic // source-level (non-skill-specific) findings
}

// Options constrains a scan (FR-012).
type Options struct {
	MaxDepth   int             // 0 ⇒ unbounded (default)
	Include    []string        // path globs; empty ⇒ all
	Exclude    []string        // path globs, applied on top of the default ignore set
	IgnoreDirs map[string]bool // directory names to prune; nil ⇒ DefaultIgnoreDirs
	RootID     string          // identity for a root SKILL.md; "" ⇒ derived from the source base (R2/U1)
}

// DefaultIgnoreDirs are the directory names pruned during a scan so noisy or
// unsafe trees are never descended (FR-004).
func DefaultIgnoreDirs() map[string]bool {
	return map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "dist": true,
		"build": true, ".next": true, ".venv": true, "venv": true,
		"target": true, ".idea": true, ".vscode": true, "__pycache__": true,
		".gskill": true,
	}
}

// DiscoverAll recursively scans root and returns every skill folder found, with
// per-skill diagnostics and duplicate conflicts. It is pure (filesystem only):
// it returns a non-nil error only when root itself cannot be walked, never for a
// per-skill defect, which is recorded as data instead (FR-001..FR-012).
func DiscoverAll(root string, opts Options) (Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve scan root %s: %w", root, err)
	}
	ignore := opts.IgnoreDirs
	if ignore == nil {
		ignore = DefaultIgnoreDirs()
	}

	var skills []DiscoveredSkill
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch action, repoPath := classifyEntry(absRoot, p, d, ignore, opts); action {
		case actionSkipDir:
			return fs.SkipDir
		case actionCollect:
			skills = append(skills, loadDiscovered(absRoot, filepath.Dir(p), p, repoPath, opts.RootID))
		case actionNone:
		}
		return nil
	})
	if walkErr != nil {
		return Result{}, fmt.Errorf("scan %s: %w", absRoot, walkErr)
	}

	sort.Slice(skills, func(i, j int) bool {
		if skills[i].RepoPath != skills[j].RepoPath {
			return skills[i].RepoPath < skills[j].RepoPath
		}
		return skills[i].ID < skills[j].ID
	})
	return Result{Skills: skills, Duplicates: duplicates(skills)}, nil
}

// entryAction is what the walk should do with a visited filesystem entry.
type entryAction int

const (
	actionNone    entryAction = iota // descend / ignore this entry
	actionSkipDir                    // prune this directory subtree
	actionCollect                    // this is a SKILL.md to record
)

// classifyEntry decides what to do with one walked entry: prune a directory
// (ignored, excluded, or a symlink), collect a SKILL.md that passes the depth
// and include filters, or do nothing. It keeps DiscoverAll's walk callback flat.
func classifyEntry(root, p string, d fs.DirEntry, ignore map[string]bool, opts Options) (entryAction, string) {
	// Never follow symlinks (defense against traversal/loops, R1).
	if d.Type()&fs.ModeSymlink != 0 {
		if d.IsDir() {
			return actionSkipDir, ""
		}
		return actionNone, ""
	}
	if d.IsDir() {
		if p != root && (ignore[d.Name()] || excluded(relSlash(root, p), opts.Exclude)) {
			return actionSkipDir, ""
		}
		return actionNone, ""
	}
	if d.Name() != integrity.SkillFileName {
		return actionNone, ""
	}
	repoPath := relSlash(root, filepath.Dir(p))
	if !withinDepth(repoPath, opts.MaxDepth) || !included(repoPath, opts.Include) {
		return actionNone, ""
	}
	return actionCollect, repoPath
}

// loadDiscovered builds one DiscoveredSkill from its SKILL.md, recording defects
// as diagnostics rather than failing.
func loadDiscovered(root, dir, skillFile, repoPath, rootID string) DiscoveredSkill {
	id := skillID(root, dir, repoPath, rootID)
	folderBase := identityFolder(root, dir, repoPath)

	s := DiscoveredSkill{
		ID:        id,
		RepoPath:  repoPath,
		Dir:       dir,
		SkillFile: skillFile,
	}

	content, readErr := os.ReadFile(skillFile) //nolint:gosec // scanning within a caller-provided root
	if readErr != nil {
		s.DisplayName = humanizeName(folderBase)
		s.Problems = append(s.Problems, Diagnostic{SeverityError, fmt.Sprintf("read SKILL.md: %v", readErr), repoPath})
		return s
	}

	doc, parseErr := metadata.ParseLenient(content)
	if parseErr != nil {
		s.DisplayName = humanizeName(folderBase)
		s.Problems = append(s.Problems, Diagnostic{SeverityError, parseErr.Error(), repoPath})
		return s
	}

	s.Frontmatter = doc.Frontmatter
	s.Description = doc.Frontmatter.Description
	s.Valid = true
	if doc.Frontmatter.Name != "" {
		s.DisplayName = doc.Frontmatter.Name
		if normalizeID(doc.Frontmatter.Name) != id {
			s.Problems = append(s.Problems, Diagnostic{
				SeverityWarning,
				fmt.Sprintf("frontmatter name %q does not match folder identity %q", doc.Frontmatter.Name, id),
				repoPath,
			})
		}
	} else {
		s.DisplayName = humanizeName(folderBase)
	}
	for _, w := range doc.Warnings {
		s.Problems = append(s.Problems, Diagnostic{SeverityWarning, w, repoPath})
	}
	return s
}

// skillID derives a skill's normalized identity: the folder name, except a root
// SKILL.md which takes the explicit RootID or the source base (R2/U1).
func skillID(root, dir, repoPath, rootID string) string {
	if repoPath == "" {
		if rootID != "" {
			return rootID
		}
		return normalizeID(filepath.Base(root))
	}
	return normalizeID(filepath.Base(dir))
}

// identityFolder returns the folder name used to humanize a missing display name.
func identityFolder(root, dir, repoPath string) string {
	if repoPath == "" {
		return filepath.Base(root)
	}
	return filepath.Base(dir)
}

// duplicates groups skills by normalized id and reports any id shared by more
// than one skill, with all participating paths sorted.
func duplicates(skills []DiscoveredSkill) []DuplicateConflict {
	byID := make(map[string][]string)
	order := make([]string, 0)
	for _, s := range skills {
		if _, seen := byID[s.ID]; !seen {
			order = append(order, s.ID)
		}
		byID[s.ID] = append(byID[s.ID], s.RepoPath)
	}
	var out []DuplicateConflict
	for _, id := range order {
		paths := byID[id]
		if len(paths) > 1 {
			sorted := append([]string(nil), paths...)
			sort.Strings(sorted)
			out = append(out, DuplicateConflict{ID: id, Paths: sorted})
		}
	}
	return out
}

// relSlash returns p relative to root in forward-slash form, "" for root itself.
func relSlash(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// withinDepth reports whether a skill at repoPath is within maxDepth (0 ⇒ any).
func withinDepth(repoPath string, maxDepth int) bool {
	if maxDepth <= 0 {
		return true
	}
	if repoPath == "" {
		return true
	}
	return strings.Count(repoPath, "/")+1 <= maxDepth
}

// included reports whether repoPath matches any include glob (empty ⇒ all).
func included(repoPath string, include []string) bool {
	if len(include) == 0 {
		return true
	}
	return matchesAny(repoPath, include)
}

// excluded reports whether repoPath matches any exclude glob.
func excluded(repoPath string, exclude []string) bool {
	return matchesAny(repoPath, exclude)
}

// matchesAny reports whether repoPath matches any of the path globs.
func matchesAny(repoPath string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := path.Match(g, repoPath); ok {
			return true
		}
	}
	return false
}
