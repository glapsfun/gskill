// generate.go assembles one generation run into a Plan, then writes it or
// checks it for drift. Building a Plan touches nothing on disk, so tests and
// `gen-site -check` can inspect a run safely.

package docsite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Options configures a run. Every path except RepoRoot is slash-separated and
// relative to RepoRoot.
type Options struct {
	RepoRoot     string
	DocsDir      string // canonical Markdown, e.g. "docs"
	NavPath      string // navigation manifest
	OutDir       string // generated Starlight content tree
	GeneratedDir string // generated data (sidebar, install methods)
	AssetsDir    string // images copied from DocsDir
	LandingPath  string // hand-written landing page, copied to OutDir/index.mdx
	InstallPage  string // docs-relative page holding the install-methods region
	ReadmePath   string // README kept in sync with the install region
	Config       Config
}

// DefaultOptions returns the layout this repository uses, rooted at root.
func DefaultOptions(root string) Options {
	cfg := DefaultConfig()
	cfg.RepoRoot = root
	return Options{
		RepoRoot:     root,
		DocsDir:      docsRoot,
		NavPath:      docsRoot + "/nav.yaml",
		OutDir:       "website/src/content/docs",
		GeneratedDir: "website/src/generated",
		AssetsDir:    "website/public/" + assetsPath,
		LandingPath:  "website/src/landing/index.mdx",
		InstallPage:  "installation.md",
		ReadmePath:   "README.md",
		Config:       cfg,
	}
}

// Plan is everything one run produces, keyed by repository-relative slash
// path. Only tracked files (committed outputs) are compared by Check.
type Plan struct {
	Files   map[string][]byte
	Entries []NavEntry

	// tracked maps each committed output to the drift label Check reports.
	tracked map[string]string
	// managed are the gitignored directories Write clears before writing.
	managed []string
}

// Build reads the docs and the manifest and returns the Plan for them.
func Build(opts Options) (*Plan, error) {
	navSrc, err := os.ReadFile(opts.abs(opts.NavPath))
	if err != nil {
		return nil, fmt.Errorf("read nav manifest: %w", err)
	}
	nav, err := ParseNav(navSrc, opts.NavPath)
	if err != nil {
		return nil, err
	}
	_, assets, entries, errs := check(nav, opts.abs(opts.DocsDir))
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	if err := checkSlugs(paths); err != nil {
		return nil, err
	}

	cfg := opts.Config
	cfg.Excluded = nav.Excluded
	plan := &Plan{
		Files:   map[string][]byte{},
		Entries: entries,
		tracked: map[string]string{},
		managed: []string{opts.OutDir, opts.GeneratedDir, opts.AssetsDir},
	}

	for _, e := range entries {
		if err := plan.addPage(opts, cfg, e.Path); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if err := plan.addAssets(opts, nav, assets); err != nil {
		return nil, err
	}
	if err := plan.addLanding(opts); err != nil {
		return nil, err
	}
	sidebar, err := marshalJSON(Sidebar(nav, entries))
	if err != nil {
		return nil, fmt.Errorf("marshal sidebar: %w", err)
	}
	plan.Files[path.Join(opts.GeneratedDir, "sidebar.json")] = sidebar
	return plan, nil
}

// addPage transforms one listed page into the plan. The install page becomes
// MDX with the InstallTabs component, and also yields install-methods.json and
// the README's install region.
func (p *Plan) addPage(opts Options, cfg Config, rel string) error {
	srcBytes, err := os.ReadFile(opts.abs(path.Join(opts.DocsDir, rel)))
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	src := string(srcBytes)
	if rel != opts.InstallPage {
		out, err := Transform(src, rel, cfg)
		if err != nil {
			return err
		}
		p.Files[path.Join(opts.OutDir, Slug(rel)+".md")] = []byte(out)
		return nil
	}

	methods, err := ParseInstall(src)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	out, err := TransformInstallPage(src, rel, cfg)
	if err != nil {
		return err
	}
	p.Files[path.Join(opts.OutDir, Slug(rel)+".mdx")] = []byte(out)

	data, err := RenderInstallJSON(methods)
	if err != nil {
		return fmt.Errorf("render install methods: %w", err)
	}
	p.Files[path.Join(opts.GeneratedDir, "install-methods.json")] = data
	return p.syncReadme(opts, src)
}

// syncReadme plans the README with its install region copied from the install
// page. The README is the one tracked output.
func (p *Plan) syncReadme(opts Options, installSrc string) error {
	inner, err := InstallRegion(installSrc)
	if err != nil {
		return err
	}
	readme, err := os.ReadFile(opts.abs(opts.ReadmePath))
	if err != nil {
		return fmt.Errorf("read %s: %w", opts.ReadmePath, err)
	}
	synced, err := syncInstallRegion(string(readme), inner)
	if err != nil {
		return fmt.Errorf("%s: %w", opts.ReadmePath, err)
	}
	p.Files[opts.ReadmePath] = []byte(synced)
	p.tracked[opts.ReadmePath] = "install region differs"
	return nil
}

// addAssets copies every image under DocsDir that is not excluded into
// AssetsDir, where rewritten image links point.
func (p *Plan) addAssets(opts Options, nav *Nav, assets []string) error {
	for _, rel := range assets {
		if nav.Excluded(rel) {
			continue
		}
		data, err := os.ReadFile(opts.abs(path.Join(opts.DocsDir, rel)))
		if err != nil {
			return fmt.Errorf("read asset %s: %w", rel, err)
		}
		p.Files[path.Join(opts.AssetsDir, rel)] = data
	}
	return nil
}

// addLanding copies the hand-written landing page in, if it exists yet.
func (p *Plan) addLanding(opts Options) error {
	if opts.LandingPath == "" {
		return nil
	}
	landing, err := os.ReadFile(opts.abs(opts.LandingPath))
	switch {
	case err == nil:
		p.Files[path.Join(opts.OutDir, "index.mdx")] = landing
		return nil
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("read landing page: %w", err)
	}
}

// Write clears the managed directories, so pages removed from docs/ disappear,
// then writes every planned file under root. Tracked files are rewritten only
// when their content changed. It returns the paths written, sorted.
func (p *Plan) Write(root string) ([]string, error) {
	for _, dir := range p.managed {
		if !safeRel(dir) {
			return nil, fmt.Errorf("refusing to clear %q: not a path inside the repository", dir)
		}
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(dir))); err != nil {
			return nil, fmt.Errorf("clear %s: %w", dir, err)
		}
	}

	var written []string
	for _, rel := range sortedKeys(p.Files) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, ok := p.tracked[rel]; ok {
			if cur, err := os.ReadFile(abs); err == nil && bytes.Equal(cur, p.Files[rel]) { //nolint:gosec // path built from the plan's own keys under the repo root
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir for %s: %w", rel, err)
		}
		if err := os.WriteFile(abs, p.Files[rel], 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", rel, err)
		}
		written = append(written, rel)
	}
	return written, nil
}

// Check compares the tracked outputs with the files under root and returns one
// line per drifted file. Gitignored outputs are not compared: they are rebuilt
// on every run and never committed.
func (p *Plan) Check(root string) ([]string, error) {
	var diff []string
	for _, rel := range sortedKeys(p.Files) {
		label, ok := p.tracked[rel]
		if !ok {
			continue
		}
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // path built from the plan's own keys under the repo root
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		if !bytes.Equal(got, p.Files[rel]) {
			diff = append(diff, fmt.Sprintf("%s (%s)", rel, label))
		}
	}
	return diff, nil
}

// FindRepoRoot walks up from start to the directory holding go.mod.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", start, err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", start)
		}
		dir = parent
	}
}

// abs maps a repository-relative slash path to an absolute path.
func (o Options) abs(rel string) string {
	return filepath.Join(o.RepoRoot, filepath.FromSlash(rel))
}

// check scans docsDir and resolves the manifest against it, returning the
// pages, assets, resolved entries, and every coverage problem.
func check(nav *Nav, docsDir string) (pages, assets []string, entries []NavEntry, errs []error) {
	pages, assets, err := scanDocs(docsDir)
	if err != nil {
		return nil, nil, nil, []error{err}
	}
	entries, errs = resolve(nav, pages)

	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Path] = true
	}
	prefix := filepath.ToSlash(filepath.Base(docsDir))
	for _, rel := range pages {
		if !listed[rel] && !nav.Excluded(rel) {
			errs = append(errs, fmt.Errorf("%s/%s is in neither %s nor its exclude list", prefix, rel, nav.name))
		}
	}
	return pages, assets, entries, errs
}

// safeRel reports whether rel is a non-empty relative path that stays inside
// its root.
func safeRel(rel string) bool {
	clean := path.Clean(rel)
	return rel != "" && clean != "." && !path.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, "../")
}

// marshalJSON renders v as indented JSON with a trailing newline, without HTML
// escaping.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sortedKeys returns m's keys in order, for deterministic output.
func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
