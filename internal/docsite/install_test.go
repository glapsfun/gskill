package docsite

import (
	"encoding/json"
	"strings"
	"testing"
)

// installRegion is a region that satisfies every install rule.
const installRegion = "### Install script\n\n" +
	"Downloads the right archive for your platform and verifies its checksum.\n\n" +
	"```sh\ncurl -sSfL https://raw.githubusercontent.com/glapsfun/gskill/main/scripts/install.sh | sh\n```\n\n" +
	"### Homebrew\n\nmacOS and Linux.\n\n" +
	"```sh\nbrew install glapsfun/tap/gskill\n```\n\n" +
	"### Go\n\nRequires a Go toolchain.\n\n" +
	"```sh\ngo install github.com/glapsfun/gskill/cmd/gskill@latest\n```\n\n" +
	"### npm\n\nRequires Node.js 20 or newer.\n\n" +
	"```sh\nnpm install --global @glapsfun/gskill\n```\n\n" +
	"### npx\n\nRun without installing. Requires Node.js 20 or newer.\n\n" +
	"```sh\nnpx @glapsfun/gskill version\n```\n\n" +
	"```sh\nnpx @glapsfun/gskill add github.com/glapsfun/gskill/examples/skills/hello-gskill --agent claude\n```\n"

// installDoc wraps a region in a minimal installation page.
func installDoc(region string) string {
	return "# Install GSKILL\n\nPick one method.\n\n" + InstallStart + "\n" + region + InstallEnd + "\n\n## Check the install\n\nRun `gskill version`.\n"
}

func TestParseInstall(t *testing.T) {
	t.Parallel()

	methods, err := ParseInstall(installDoc(installRegion))
	if err != nil {
		t.Fatalf("ParseInstall: %v", err)
	}
	var labels, ids []string
	for _, m := range methods {
		labels = append(labels, m.Label)
		ids = append(ids, m.ID)
	}
	if got, want := strings.Join(labels, ","), strings.Join(InstallLabels, ","); got != want {
		t.Errorf("labels = %s, want %s", got, want)
	}
	if got, want := strings.Join(ids, ","), "install-script,homebrew,go,npm,npx"; got != want {
		t.Errorf("ids = %s, want %s", got, want)
	}
	if got := methods[2].Prerequisites; got != "Requires a Go toolchain." {
		t.Errorf("Go prerequisites = %q", got)
	}
	if len(methods[4].Blocks) != 2 {
		t.Fatalf("npx has %d blocks, want 2", len(methods[4].Blocks))
	}
	if got := methods[1].Blocks[0]; got != "brew install glapsfun/tap/gskill" {
		t.Errorf("Homebrew block = %q", got)
	}
}

func TestParseInstall_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, old, new, want string
	}{
		{
			name: "methods out of order",
			old:  "### Homebrew", new: "### Brew",
			want: "install region: methods must be exactly: Install script, Homebrew, Go, npm, npx",
		},
		{
			name: "comment inside a block",
			old:  "brew install glapsfun/tap/gskill", new: "# macOS\nbrew install glapsfun/tap/gskill",
			want: `install method "Homebrew": line "# macOS" starts with # or $`,
		},
		{
			name: "prompt inside a block",
			old:  "brew install glapsfun/tap/gskill", new: "$ brew install glapsfun/tap/gskill",
			want: `install method "Homebrew": line "$ brew install glapsfun/tap/gskill" starts with # or $`,
		},
		{
			name: "empty block",
			old:  "```sh\nbrew install glapsfun/tap/gskill\n```", new: "```sh\n\n```",
			want: `install method "Homebrew": empty code block`,
		},
		{
			name: "placeholder token",
			old:  "npx @glapsfun/gskill version", new: "npx @glapsfun/gskill add github.com/owner/repo",
			want: `install method "npx": placeholder "owner/repo"`,
		},
		{
			name: "angle bracket placeholder",
			old:  "brew install glapsfun/tap/gskill", new: "brew install <tap>",
			want: `install method "Homebrew": placeholder "<"`,
		},
		{
			name: "add without the example skill",
			old:  "github.com/glapsfun/gskill/examples/skills/hello-gskill", new: "github.com/someone/skills",
			want: `install method "npx": add command must use github.com/glapsfun/gskill/examples/skills/hello-gskill`,
		},
		{
			name: "text after a code block",
			old:  "```sh\ngo install github.com/glapsfun/gskill/cmd/gskill@latest\n```\n", new: "```sh\ngo install github.com/glapsfun/gskill/cmd/gskill@latest\n```\n\nThen add it to PATH.\n",
			want: `install method "Go": text after a code block`,
		},
		{
			name: "two prerequisite paragraphs",
			old:  "macOS and Linux.\n", new: "macOS and Linux.\n\nAlso Linuxbrew.\n",
			want: `install method "Homebrew": more than one prerequisite paragraph`,
		},
		{
			name: "wrong fence language",
			old:  "```sh\nbrew install", new: "```bash\nbrew install",
			want: "install method \"Homebrew\": code block must be fenced as ```sh",
		},
		{
			name: "text before the first method",
			old:  "### Install script\n", new: "Choose one:\n\n### Install script\n",
			want: "install region: unexpected text before the first ### heading",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			region := strings.Replace(installRegion, tt.old, tt.new, 1)
			if region == installRegion {
				t.Fatalf("replacement %q did not apply", tt.old)
			}
			_, err := ParseInstall(installDoc(region))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ParseInstall error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestInstallRegion(t *testing.T) {
	t.Parallel()

	got, err := InstallRegion(installDoc(installRegion))
	if err != nil {
		t.Fatalf("InstallRegion: %v", err)
	}
	if got != installRegion {
		t.Errorf("InstallRegion =\n%q\nwant\n%q", got, installRegion)
	}

	tests := []struct {
		name, src, want string
	}{
		{"no markers", "# T\n\nText.\n", "no install-methods region"},
		{"start only", InstallStart + "\nx\n", "no install-methods region"},
		{"end before start", InstallEnd + "\n" + InstallStart + "\n", "no install-methods region"},
		{"two regions", installDoc(installRegion) + InstallStart + "\n" + InstallEnd + "\n", "more than one install-methods region"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := InstallRegion(tt.src); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("InstallRegion error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestSyncInstallRegion(t *testing.T) {
	t.Parallel()

	readme := "# GSKILL\n\n## Quick start\n\n" + InstallStart + "\nold commands\n" + InstallEnd + "\n\nVerify with `gskill version`.\n"
	got, err := syncInstallRegion(readme, installRegion)
	if err != nil {
		t.Fatalf("syncInstallRegion: %v", err)
	}
	region, err := InstallRegion(got)
	if err != nil {
		t.Fatalf("InstallRegion(synced): %v", err)
	}
	if region != installRegion {
		t.Errorf("README region is not byte-identical to the source region:\n%q", region)
	}
	if !strings.HasSuffix(got, InstallEnd+"\n\nVerify with `gskill version`.\n") || !strings.HasPrefix(got, "# GSKILL\n") {
		t.Errorf("text outside the region changed:\n%s", got)
	}

	if _, err := syncInstallRegion("# GSKILL\n", installRegion); err == nil {
		t.Error("syncInstallRegion succeeded on a README without markers")
	}
}

func TestTransformInstallPage(t *testing.T) {
	t.Parallel()

	out, err := TransformInstallPage(installDoc(installRegion), "installation.md", testConfig(""))
	if err != nil {
		t.Fatalf("TransformInstallPage: %v", err)
	}
	wantImport := "---\n\n" + installTabsImport + "\n\n"
	if !strings.Contains(out, wantImport) {
		t.Errorf("import is not directly after the frontmatter:\n%s", out)
	}
	if !strings.Contains(out, "Pick one method.\n\n<InstallTabs />\n\n## Check the install") {
		t.Errorf("region was not replaced by <InstallTabs />:\n%s", out)
	}
	for _, gone := range []string{InstallStart, InstallEnd, "### Homebrew", "brew install"} {
		if strings.Contains(out, gone) {
			t.Errorf("output still contains %q", gone)
		}
	}
}

func TestRenderInstallJSON(t *testing.T) {
	t.Parallel()

	methods, err := ParseInstall(installDoc(installRegion))
	if err != nil {
		t.Fatalf("ParseInstall: %v", err)
	}
	data, err := RenderInstallJSON(methods)
	if err != nil {
		t.Fatalf("RenderInstallJSON: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d methods, want 5", len(got))
	}
	first := got[0]
	for _, key := range []string{"id", "label", "prerequisites", "blocks"} {
		if _, ok := first[key]; !ok {
			t.Errorf("method JSON missing %q: %v", key, first)
		}
	}
	if strings.Contains(string(data), `<`) || strings.Contains(string(data), `&`) {
		t.Errorf("JSON is HTML-escaped:\n%s", data)
	}
}
