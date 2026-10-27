// install.go parses the install-methods region of docs/installation.md, the one
// place install commands are written. From it come the site's install tabs (via
// install-methods.json) and the README's install section (a byte-identical
// copy of the region).

package docsite

import (
	"errors"
	"fmt"
	"strings"
)

// Region markers. The same pair delimits the region in docs/installation.md
// and in README.md.
const (
	InstallStart = "<!-- install-methods:start -->"
	InstallEnd   = "<!-- install-methods:end -->"
)

// ExampleSkillSource is the skill every runnable example adds. It lives in
// this repository under examples/skills/hello-gskill.
const ExampleSkillSource = "github.com/glapsfun/gskill/examples/skills/hello-gskill"

// installTabsImport is placed after the installation page's frontmatter. The
// path is relative to website/src/content/docs, where the page is built.
const installTabsImport = "import InstallTabs from '../../components/InstallTabs.astro';"

// InstallLabels are the install methods, in tab order. The first is the
// default tab.
var InstallLabels = []string{"Install script", "Homebrew", "Go", "npm", "npx"}

// placeholders must never appear in a command a visitor will paste.
var placeholders = []string{"<", ">", "owner/repo", "example/skills"}

// InstallMethod is one way to install or run GSKILL. Each block is copied as
// a unit by the site's copy button.
type InstallMethod struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Prerequisites string   `json:"prerequisites"`
	Blocks        []string `json:"blocks"`
}

// InstallRegion returns the text between the install markers in src.
func InstallRegion(src string) (string, error) {
	inner, _, _, err := findRegion(src)
	return inner, err
}

// ParseInstall parses and validates the install region of src.
func ParseInstall(src string) ([]InstallMethod, error) {
	inner, err := InstallRegion(src)
	if err != nil {
		return nil, err
	}
	methods, err := parseMethods(inner)
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(methods))
	for _, m := range methods {
		labels = append(labels, m.Label)
	}
	if strings.Join(labels, "\n") != strings.Join(InstallLabels, "\n") {
		return nil, fmt.Errorf("install region: methods must be exactly: %s", strings.Join(InstallLabels, ", "))
	}
	var errs []error
	for _, m := range methods {
		errs = append(errs, validateMethod(m)...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return methods, nil
}

// RenderInstallJSON renders the methods the InstallTabs component reads.
func RenderInstallJSON(methods []InstallMethod) ([]byte, error) {
	return marshalJSON(methods)
}

// TransformInstallPage transforms docs/installation.md like any page, except
// that the install region becomes <InstallTabs /> and the component's import is
// placed directly after the frontmatter.
func TransformInstallPage(src, rel string, cfg Config) (string, error) {
	_, start, end, err := findRegion(src)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	src = src[:start] + "<InstallTabs />" + src[end:]

	page, err := ParsePage(src, rel, cfg)
	if err != nil {
		return "", err
	}
	body, err := RewriteLinks(page.Body, rel, cfg)
	if err != nil {
		return "", err
	}
	return page.Frontmatter() + "\n" + installTabsImport + "\n\n" + body, nil
}

// syncInstallRegion returns dst with the text between its install markers
// replaced by inner.
func syncInstallRegion(dst, inner string) (string, error) {
	_, start, end, err := findRegion(dst)
	if err != nil {
		return "", err
	}
	return dst[:start] + InstallStart + "\n" + inner + InstallEnd + dst[end:], nil
}

// findRegion locates the single install region in src. It returns the text
// between the marker lines and the byte offsets of the whole region, markers
// included.
func findRegion(src string) (inner string, start, end int, err error) {
	start = strings.Index(src, InstallStart)
	endAt := strings.Index(src, InstallEnd)
	if start < 0 || endAt < start {
		return "", 0, 0, errors.New("no install-methods region (" + InstallStart + " ... " + InstallEnd + ")")
	}
	if strings.Count(src, InstallStart) > 1 || strings.Count(src, InstallEnd) > 1 {
		return "", 0, 0, errors.New("more than one install-methods region")
	}
	inner = strings.TrimPrefix(src[start+len(InstallStart):endAt], "\n")
	return inner, start, endAt + len(InstallEnd), nil
}

// methodParser walks the region line by line. Each method is a ### heading, at
// most one prerequisite paragraph, then one or more ```sh blocks.
type methodParser struct {
	methods []InstallMethod
	block   []string
	inBlock bool // inside a ```sh block
	blocks  bool // the current method already has a block
	prereq  bool // the current method's prerequisite paragraph has started
	ended   bool // ...and a blank line has ended it
}

// parseMethods splits the region into methods.
func parseMethods(inner string) ([]InstallMethod, error) {
	p := &methodParser{}
	for _, line := range strings.Split(inner, "\n") {
		if err := p.line(line); err != nil {
			return nil, err
		}
	}
	if p.inBlock {
		return nil, fmt.Errorf("install method %q: unclosed code block", p.cur().Label)
	}
	return p.methods, nil
}

// cur is the method being parsed.
func (p *methodParser) cur() *InstallMethod { return &p.methods[len(p.methods)-1] }

// line consumes one line of the region.
func (p *methodParser) line(line string) error {
	switch {
	case p.inBlock && line == "```":
		p.cur().Blocks = append(p.cur().Blocks, strings.Join(p.block, "\n"))
		p.block, p.inBlock, p.blocks = nil, false, true
	case p.inBlock:
		p.block = append(p.block, line)
	case strings.HasPrefix(line, "### "):
		label := strings.TrimSpace(strings.TrimPrefix(line, "### "))
		p.methods = append(p.methods, InstallMethod{ID: methodID(label), Label: label, Blocks: []string{}})
		p.blocks, p.prereq, p.ended = false, false, false
	case strings.TrimSpace(line) == "":
		p.ended = p.prereq
	case len(p.methods) == 0:
		return errors.New("install region: unexpected text before the first ### heading")
	case strings.HasPrefix(line, "```"):
		if line != "```sh" {
			return fmt.Errorf("install method %q: code block must be fenced as ```sh", p.cur().Label)
		}
		p.inBlock = true
	default:
		return p.prose(strings.TrimSpace(line))
	}
	return nil
}

// prose adds a line of text to the current method's prerequisite paragraph.
func (p *methodParser) prose(text string) error {
	m := p.cur()
	switch {
	case p.blocks:
		return fmt.Errorf("install method %q: text after a code block", m.Label)
	case p.ended:
		return fmt.Errorf("install method %q: more than one prerequisite paragraph", m.Label)
	case p.prereq:
		m.Prerequisites += " " + text
	default:
		m.Prerequisites, p.prereq = text, true
	}
	return nil
}

// validateMethod applies the copy-safety rules to one method's blocks.
func validateMethod(m InstallMethod) []error {
	var errs []error
	if len(m.Blocks) == 0 {
		errs = append(errs, fmt.Errorf("install method %q: no code block", m.Label))
	}
	for _, b := range m.Blocks {
		if strings.TrimSpace(b) == "" {
			errs = append(errs, fmt.Errorf("install method %q: empty code block", m.Label))
			continue
		}
		for _, line := range strings.Split(b, "\n") {
			errs = append(errs, validateCommand(m.Label, line)...)
		}
	}
	return errs
}

// validateCommand checks one command line from an install block.
func validateCommand(label, line string) []error {
	var errs []error
	t := strings.TrimSpace(line)
	if t == "" {
		return []error{fmt.Errorf("install method %q: blank line inside a code block", label)}
	}
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "$") {
		errs = append(errs, fmt.Errorf("install method %q: line %q starts with # or $", label, t))
	}
	for _, p := range placeholders {
		if strings.Contains(t, p) {
			errs = append(errs, fmt.Errorf("install method %q: placeholder %q in %q", label, p, t))
		}
	}
	if isAddCommand(t) && !strings.Contains(t, ExampleSkillSource) {
		errs = append(errs, fmt.Errorf("install method %q: add command must use %s", label, ExampleSkillSource))
	}
	return errs
}

// isAddCommand reports whether a command runs `gskill add`, directly or via
// npx.
func isAddCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	for i := 1; i < len(fields); i++ {
		prev := fields[i-1]
		if fields[i] == "add" && (prev == "gskill" || strings.HasSuffix(prev, "/gskill")) {
			return true
		}
	}
	return false
}

// methodID derives a stable id from a label, e.g. "Install script" becomes
// "install-script".
func methodID(label string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(label)), " ", "-")
}
