package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/docsite"
	"github.com/glapsfun/gskill/internal/metadata"
)

// The official bundles are installed from one GSKILL release tag. Every
// documented install and bootstrap command names it, so moving to another
// release is a single find-and-replace that these constants then check.
const (
	officialTag        = "v0.13.0"
	officialArchiveDir = "gskill-0.13.0"
	minimumCLI         = "0.12.0"
	guidePath          = "docs/how-to/use-gskill-from-an-agent.md"
	guideURL           = "https://glapsfun.github.io/gskill/how-to/use-gskill-from-an-agent/"
	readmeAgentHeading = "## Use GSKILL from an AI agent"
	officialSource     = "github.com/glapsfun/gskill"
)

// docFile is one Markdown text the official-skill checks read, labeled by its
// repository path (or region) for failure messages.
type docFile struct {
	label string
	text  string
}

// officialFiles returns every Markdown file under skills/, the guide, and the
// README's agent section: the texts whose commands must match the shipped CLI.
func officialFiles(t *testing.T) []docFile {
	t.Helper()
	var files []docFile
	err := filepath.WalkDir(repoPath("skills"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, p)
		if relErr != nil {
			return relErr
		}
		files = append(files, docFile{label: filepath.ToSlash(rel), text: readRepoFile(t, rel)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk skills/: %v", err)
	}
	files = append(files, docFile{label: guidePath, text: readRepoFile(t, guidePath)})
	if section := readmeAgentSection(readRepoFile(t, "README.md")); section != "" {
		files = append(files, docFile{label: "README.md agent section", text: section})
	}
	return files
}

// readmeAgentSection returns the README text from the agent heading up to the
// next level-2 heading, or "" when the section is missing.
func readmeAgentSection(readme string) string {
	start := strings.Index(readme, "\n"+readmeAgentHeading+"\n")
	if start < 0 {
		return ""
	}
	section := readme[start+1:]
	if end := strings.Index(section[len(readmeAgentHeading):], "\n## "); end >= 0 {
		section = section[:len(readmeAgentHeading)+end+1]
	}
	return section
}

// checkInvocations parses every gskill command in markdown against the shipped
// grammar and returns one message per command the CLI would reject.
func checkInvocations(label, markdown string) (problems []string, checked int) {
	for _, inv := range gskillInvocations(markdown) {
		checked++
		if err := cli.ParseArgs(inv.words); err != nil {
			problems = append(problems, label+":"+strconv.Itoa(inv.line)+": "+inv.text+": "+err.Error())
		}
	}
	return problems, checked
}

// TestDocumentedGskillCommandsParse is the stale-example guard (FR-044,
// FR-051): every gskill command an agent or reader can copy from the bundles,
// the guide, or the README agent section must parse against this CLI.
func TestDocumentedGskillCommandsParse(t *testing.T) {
	t.Parallel()

	total := 0
	for _, f := range officialFiles(t) {
		problems, checked := checkInvocations(f.label, f.text)
		total += checked
		for _, p := range problems {
			t.Error(p)
		}
	}
	if total == 0 {
		t.Fatal("no gskill commands found in the official skill files")
	}
}

var (
	refRE        = regexp.MustCompile("--ref[ =](v[0-9][^\\s'\"`|)]*)")
	archiveTagRE = regexp.MustCompile(`archive/refs/tags/(v[^/\s]+?)\.tar\.gz`)
	archiveDirRE = regexp.MustCompile(`\bgskill-[0-9]+\.[0-9]+\.[0-9]+[^/\s]*/`)
	// The repository itself or a bundle under skills/, not another path in it
	// such as the examples/ skill.
	officialSourceRE = regexp.MustCompile(regexp.QuoteMeta(officialSource) + "(/skills/\\S*)?(\\s|$)")
)

// TestOfficialPinIsConsistent keeps every install of the official bundles on
// one release tag (FR-007, FR-043). A --ref on any other source, such as a
// user's repository in an example, is not about the bundles and is skipped.
func TestOfficialPinIsConsistent(t *testing.T) {
	t.Parallel()

	pins := 0
	for _, f := range officialFiles(t) {
		for _, ll := range logicalLines(f.text) {
			line, where := ll.text, f.label+":"+strconv.Itoa(ll.row)
			if officialSourceRE.MatchString(line) {
				for _, m := range refRE.FindAllStringSubmatch(line, -1) {
					pins++
					if m[1] != officialTag {
						t.Errorf("%s: --ref %s, want %s", where, m[1], officialTag)
					}
				}
			}
			for _, m := range archiveTagRE.FindAllStringSubmatch(line, -1) {
				pins++
				if m[1] != officialTag {
					t.Errorf("%s: archive tag %s, want %s", where, m[1], officialTag)
				}
			}
			for _, m := range archiveDirRE.FindAllString(line, -1) {
				if m != officialArchiveDir+"/" {
					t.Errorf("%s: archive directory %s, want %s/", where, m, officialArchiveDir)
				}
			}
		}
	}
	if pins == 0 {
		t.Fatalf("no pinned install of %s found; documented installs must name %s", officialSource, officialTag)
	}
}

// logicalLine is one shell-style line: physical lines joined across trailing
// backslashes, numbered by the first of them.
type logicalLine struct {
	row  int
	text string
}

// logicalLines joins backslash continuations so a pin written on the line
// after its source is still checked against that source.
func logicalLines(text string) []logicalLine {
	var (
		out     []logicalLine
		pending strings.Builder
		start   int
	)
	for i, line := range strings.Split(text, "\n") {
		if pending.Len() == 0 {
			start = i + 1
		}
		if trimmed := strings.TrimRight(line, " \t"); strings.HasSuffix(trimmed, `\`) {
			pending.WriteString(strings.TrimSuffix(trimmed, `\`) + " ")
			continue
		}
		pending.WriteString(line)
		out = append(out, logicalLine{row: start, text: pending.String()})
		pending.Reset()
	}
	if pending.Len() > 0 {
		out = append(out, logicalLine{row: start, text: pending.String()})
	}
	return out
}

func TestLogicalLinesJoinsContinuations(t *testing.T) {
	t.Parallel()

	got := logicalLines("gskill add " + officialSource + "/skills/gskill \\\n  --ref v0.12.0\nnext")
	if len(got) != 2 || got[0].row != 1 || !strings.Contains(got[0].text, officialSource) || !strings.Contains(got[0].text, "--ref v0.12.0") || got[1].row != 3 {
		t.Errorf("logicalLines = %+v, want the pin joined onto line 1", got)
	}
}

var markdownLinkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// TestOfficialBundleLinks checks that links inside a bundle stay inside it, so
// they still resolve after the bundle is copied into an agent's skills folder.
func TestOfficialBundleLinks(t *testing.T) {
	t.Parallel()

	bundles, err := os.ReadDir(repoPath("skills"))
	if err != nil {
		t.Fatalf("read skills/: %v", err)
	}
	for _, b := range bundles {
		if !b.IsDir() {
			continue
		}
		root := repoPath("skills", b.Name())
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
				return err
			}
			data, readErr := os.ReadFile(p) //nolint:gosec // repository file
			if readErr != nil {
				return readErr
			}
			for _, problem := range linkProblems(root, p, string(data)) {
				t.Error(problem)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// linkProblems reports each relative link in the bundle file p that leaves the
// bundle root or names a missing file.
func linkProblems(root, p, markdown string) []string {
	var problems []string
	for _, m := range markdownLinkRE.FindAllStringSubmatch(markdown, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		resolved := filepath.Join(filepath.Dir(p), filepath.FromSlash(target))
		if rel, err := filepath.Rel(root, resolved); err != nil || strings.HasPrefix(rel, "..") {
			problems = append(problems, p+": link "+m[1]+" leaves the bundle")
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			problems = append(problems, p+": link "+m[1]+" does not resolve")
		}
	}
	return problems
}

// validateBundle returns every way the bundle in dir breaks the official
// bundle contract (specs 034 contracts/bundles.md) other than its requires
// block, which differs per bundle. It returns the parsed document too.
func validateBundle(dir string) (metadata.Document, []string) {
	var problems []string
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md")) //nolint:gosec // repository file
	if err != nil {
		return metadata.Document{}, []string{err.Error()}
	}
	doc, err := metadata.Parse(data)
	if err != nil {
		return doc, []string{"SKILL.md: " + err.Error()}
	}
	problems = append(problems, frontmatterProblems(filepath.Base(dir), doc)...)
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o111 != 0 {
			problems = append(problems, p+" is executable")
		}
		return nil
	})
	if walkErr != nil {
		problems = append(problems, walkErr.Error())
	}
	return doc, problems
}

// frontmatterProblems checks the SKILL.md fields every official bundle shares.
func frontmatterProblems(name string, doc metadata.Document) []string {
	var problems []string
	fm := doc.Frontmatter
	if fm.Name != name {
		problems = append(problems, "name "+strconv.Quote(fm.Name)+" does not match directory "+strconv.Quote(name))
	}
	if strings.TrimSpace(fm.Description) == "" {
		problems = append(problems, "description is empty")
	}
	if fm.License != "MIT" {
		problems = append(problems, "license "+strconv.Quote(fm.License)+", want MIT")
	}
	if compat, ok := fm.Compatibility.(string); !ok || !strings.Contains(compat, minimumCLI) {
		problems = append(problems, "compatibility must be a string naming GSKILL "+minimumCLI)
	}
	if lines := strings.Count(strings.TrimSpace(string(doc.Body)), "\n") + 1; lines > maxBodyLines {
		problems = append(problems, "SKILL.md body is "+strconv.Itoa(lines)+" lines, want at most "+strconv.Itoa(maxBodyLines))
	}
	return problems
}

// maxBodyLines keeps SKILL.md to the procedure; details go in references/
// (FR-003).
const maxBodyLines = 150

func TestOfficialBundlesMetadata(t *testing.T) {
	t.Parallel()

	t.Run("gskill-install", func(t *testing.T) {
		t.Parallel()
		doc, problems := validateBundle(repoPath("skills", "gskill-install"))
		for _, p := range problems {
			t.Error(p)
		}
		if !strings.Contains(strings.ToLower(doc.Frontmatter.Description), "install") {
			t.Error("description does not say it installs GSKILL")
		}
		// The setup skill must load before GSKILL exists (FR-005).
		if r := doc.Frontmatter.Requires; len(r.Commands)+len(r.Skills)+len(r.Environment)+len(r.MCP) != 0 {
			t.Errorf("requires = %+v, want nothing", r)
		}
	})

	t.Run("gskill", func(t *testing.T) {
		t.Parallel()
		doc, problems := validateBundle(repoPath("skills", "gskill"))
		for _, p := range problems {
			t.Error(p)
		}
		// Recorded and checked by `gskill doctor`, never installed (FR-004).
		if got := doc.Frontmatter.Requires.Commands; len(got) != 1 || got[0] != "gskill" {
			t.Errorf("requires.commands = %q, want [gskill]", got)
		}
	})

	t.Run("only official bundles", func(t *testing.T) {
		t.Parallel()
		entries, err := os.ReadDir(repoPath("skills"))
		if err != nil {
			t.Fatal(err)
		}
		var dirs []string
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
		if strings.Join(dirs, ",") != "gskill,gskill-install" {
			t.Errorf("skills/ holds %v, want exactly [gskill gskill-install]", dirs)
		}
	})
}

var exitCodeRowRE = regexp.MustCompile(`(?m)^\| ([0-9]+) \|`)

// TestResultsCoverEveryExitCode keeps the usage skill's result table in step
// with the generated exit-code reference, so an agent can classify every code
// the CLI returns (FR-027).
func TestResultsCoverEveryExitCode(t *testing.T) {
	t.Parallel()

	codes := exitCodeRowRE.FindAllStringSubmatch(readRepoFile(t, "docs", "reference", "exit-codes.md"), -1)
	if len(codes) == 0 {
		t.Fatal("no exit codes found in docs/reference/exit-codes.md")
	}
	covered := map[string]bool{}
	for _, m := range exitCodeRowRE.FindAllStringSubmatch(readRepoFile(t, "skills", "gskill", "references", "results.md"), -1) {
		covered[m[1]] = true
	}
	for _, m := range codes {
		if !covered[m[1]] {
			t.Errorf("skills/gskill/references/results.md has no row for exit code %s", m[1])
		}
	}
}

// TestReadmeAgentSection checks the README shows the agent setup with
// copy-paste examples (FR-040, FR-045).
func TestReadmeAgentSection(t *testing.T) {
	t.Parallel()

	readme := readRepoFile(t, "README.md")
	section := readmeAgentSection(readme)
	if section == "" {
		t.Fatalf("README.md has no %q section", readmeAgentHeading)
	}
	quick, agent, core := strings.Index(readme, "\n## Quick start\n"), strings.Index(readme, "\n"+readmeAgentHeading+"\n"), strings.Index(readme, "\n## Core workflow\n")
	if quick < 0 || core < 0 || quick >= agent || agent >= core {
		t.Errorf("%q must sit between ## Quick start and ## Core workflow", readmeAgentHeading)
	}
	if region, err := docsite.InstallRegion(readme); err == nil && strings.Contains(region, readmeAgentHeading) {
		t.Errorf("%q is inside the generated install region", readmeAgentHeading)
	}
	for _, want := range []string{
		"https://github.com/glapsfun/gskill/archive/refs/tags/" + officialTag + ".tar.gz",
		"gskill add github.com/glapsfun/gskill/skills/gskill --ref " + officialTag,
		"--global",
		guideURL,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("README agent section does not contain %q", want)
		}
	}
	requests := 0
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "> ") {
			requests++
		}
	}
	if requests < 3 {
		t.Errorf("README agent section has %d example requests (> lines), want at least 3", requests)
	}
}

// TestValidateBundleRejectsNameMismatch proves the metadata guard catches a
// bundle whose name no longer matches its directory (FR-002).
func TestValidateBundleRejectsNameMismatch(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "gskill")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: gskill-usage\ndescription: d\nlicense: MIT\ncompatibility: GSKILL " + minimumCLI + "\n---\n# x\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, problems := validateBundle(dir)
	if len(problems) != 1 || !strings.Contains(problems[0], `does not match directory "gskill"`) {
		t.Errorf("problems = %q, want one name mismatch", problems)
	}
}
