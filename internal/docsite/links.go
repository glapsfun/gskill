// links.go rewrites docs-relative Markdown link targets into site URLs, or into
// GitHub blob URLs for anything the site does not host, and rejects targets
// that do not exist.

package docsite

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Production addresses. astro.config.mjs mirrors Site and BasePath; a test in
// test/docs keeps the two in step.
const (
	Site     = "https://glapsfun.github.io"
	BasePath = "/gskill"
	BlobBase = "https://github.com/glapsfun/gskill/blob/main"
	EditBase = "https://github.com/glapsfun/gskill/edit/main/docs"
)

// docsRoot is the documentation directory, relative to the repository root.
const docsRoot = "docs"

// assetsPath is where images from docs/ are served, under the base path. It
// matches Options.AssetsDir inside website/public.
const assetsPath = "docs-assets"

// Config carries everything link rewriting and frontmatter depend on.
type Config struct {
	Site     string
	BasePath string
	BlobBase string
	EditBase string
	// RepoRoot is the absolute repository root used to check that relative
	// targets exist. Empty disables the check.
	RepoRoot string
	// Excluded reports whether a docs-relative page is unpublished. Links to
	// such pages go to GitHub, since a site URL would 404.
	Excluded func(rel string) bool
}

// DefaultConfig returns the production addresses with no existence checks and
// no exclusions.
func DefaultConfig() Config {
	return Config{Site: Site, BasePath: BasePath, BlobBase: BlobBase, EditBase: EditBase}
}

var (
	// inlineLinkRe matches an inline link or image and captures its target and
	// optional title, so both survive the rewrite.
	inlineLinkRe = regexp.MustCompile(`(!?\[[^\]]*\]\()([^)\s]+)(\s*(?:"[^"]*")?\))`)
	// refDefRe matches a reference-style link definition.
	refDefRe = regexp.MustCompile(`^(\s{0,3}\[[^\]]+\]:\s+)(\S+)(.*)$`)
)

// RewriteLinks rewrites every link, image, and reference definition target in
// src. fromPath is the docs-relative path of the page, used to resolve
// relative targets. Fenced code blocks and inline code spans are left alone.
func RewriteLinks(src, fromPath string, cfg Config) (string, error) {
	var errs []error
	rewrite := func(target string) string {
		out, err := rewriteTarget(target, fromPath, cfg)
		if err != nil {
			errs = append(errs, err)
			return target
		}
		return out
	}

	lines := strings.Split(src, "\n")
	var f fence
	for i, line := range lines {
		if f.step(line) || f.open() {
			continue
		}
		lines[i] = rewriteLine(line, rewrite)
	}
	return strings.Join(lines, "\n"), errors.Join(errs...)
}

// rewriteLine applies rewrite to every link target on one line that is not
// inside an inline code span. Matching runs on a copy where code spans are
// blanked out, so a link whose text contains code still matches while a link
// written inside code does not.
func rewriteLine(line string, rewrite func(string) string) string {
	masked := maskCodeSpans(line)
	var b strings.Builder
	last := 0
	for _, m := range inlineLinkRe.FindAllStringSubmatchIndex(masked, -1) {
		start, end := m[4], m[5]
		b.WriteString(line[last:start])
		b.WriteString(rewrite(line[start:end]))
		last = end
	}
	b.WriteString(line[last:])
	out := b.String()

	if m := refDefRe.FindStringSubmatchIndex(maskCodeSpans(out)); m != nil {
		out = out[:m[4]] + rewrite(out[m[4]:m[5]]) + out[m[5]:]
	}
	return out
}

// maskCodeSpans returns line with the contents of every inline code span
// replaced by 'x', byte for byte, so match offsets apply to the original.
func maskCodeSpans(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := runLen(b, i, '`')
		closeAt := findRun(b, i+n, '`', n)
		if closeAt < 0 {
			i += n
			continue
		}
		for j := i + n; j < closeAt; j++ {
			b[j] = 'x'
		}
		i = closeAt + n
	}
	return string(b)
}

// runLen counts consecutive c bytes starting at i.
func runLen(b []byte, i int, c byte) int {
	n := 0
	for i+n < len(b) && b[i+n] == c {
		n++
	}
	return n
}

// findRun returns the index of the next run of exactly n c bytes at or after
// from, or -1.
func findRun(b []byte, from int, c byte, n int) int {
	for i := from; i < len(b); {
		if b[i] != c {
			i++
			continue
		}
		run := runLen(b, i, c)
		if run == n {
			return i
		}
		i += run
	}
	return -1
}

// rewriteTarget maps one link target per the rewriting table in the
// content-transform contract.
func rewriteTarget(target, fromPath string, cfg Config) (string, error) {
	body, suffix := splitLinkSuffix(target)
	if passThrough(target) || body == "" {
		return target, nil
	}

	repoRel := path.Join(docsRoot, path.Dir(fromPath), body)
	if repoRel == ".." || strings.HasPrefix(repoRel, "../") {
		return "", fmt.Errorf("%s: link target %s escapes the repository", fromPath, target)
	}
	if cfg.RepoRoot != "" {
		if _, err := os.Stat(filepath.Join(cfg.RepoRoot, filepath.FromSlash(repoRel))); err != nil {
			return "", fmt.Errorf("%s: link target %s does not exist", fromPath, target)
		}
	}

	docsRel, insideDocs := strings.CutPrefix(repoRel, docsRoot+"/")
	ext := strings.ToLower(path.Ext(docsRel))
	switch {
	case !insideDocs, cfg.Excluded != nil && cfg.Excluded(docsRel):
		return cfg.BlobBase + "/" + repoRel + suffix, nil
	case imageExts[ext]:
		return cfg.BasePath + "/" + assetsPath + "/" + docsRel + suffix, nil
	case ext != ".md":
		return cfg.BlobBase + "/" + repoRel + suffix, nil
	}
	return siteURL(cfg.BasePath, Slug(docsRel)) + suffix, nil
}

// passThrough reports whether a target is already absolute (a URL, a mailto,
// a site path) or an in-page anchor, and so is never rewritten.
func passThrough(target string) bool {
	return target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") ||
		strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
}

// siteURL is the trailing-slash URL of a slug under base.
func siteURL(base, slug string) string {
	if slug == "" {
		return base + "/"
	}
	return base + "/" + slug + "/"
}

// splitLinkSuffix separates a target from its #anchor or ?query suffix, which
// is carried through verbatim.
func splitLinkSuffix(target string) (body, suffix string) {
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		return target[:i], target[i:]
	}
	return target, ""
}

// fence tracks fenced code blocks line by line. A block opens with three or
// more backticks or tildes and closes with at least as many of the same
// character and nothing else on the line.
type fence struct {
	char byte
	n    int
}

// open reports whether the scanner is inside a fenced block.
func (f *fence) open() bool { return f.n > 0 }

// step consumes one line and reports whether it opened or closed a block.
func (f *fence) step(line string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || t == "" || (t[0] != '`' && t[0] != '~') {
		return false
	}
	n := runLen([]byte(t), 0, t[0])
	if n < 3 {
		return false
	}
	if !f.open() {
		f.char, f.n = t[0], n
		return true
	}
	if t[0] == f.char && n >= f.n && strings.TrimSpace(t[n:]) == "" {
		f.char, f.n = 0, 0
		return true
	}
	return false
}
