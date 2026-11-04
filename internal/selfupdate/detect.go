package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/version"
)

const (
	koEntrypoint   = "/ko-app/gskill"
	gskillModule   = "github.com/glapsfun/gskill"
	maxPackageJSON = 1 << 20
)

// systemDirs are the binary directories an OS package manager owns.
var systemDirs = map[string]bool{"/usr/bin": true, "/usr/sbin": true, "/bin": true, "/sbin": true}

// npmBinary matches the native binary inside an npm platform package
// (npm/README.md): <root>/node_modules/@glapsfun/gskill-<os>-<cpu>/bin/gskill.
var npmBinary = regexp.MustCompile(`^(.*)/node_modules/@glapsfun/gskill-[^/]+/bin/gskill$`)

// DetectInput is what channel detection reads. Detection never runs a package
// manager and never touches the network.
type DetectInput struct {
	Executable string
	InvokedAs  string
	Build      BuildInfo
	Getenv     func(string) string
	ReadFile   func(string) ([]byte, error)
}

// Detect classifies the installation from positive evidence only. The rules
// run in order and the first match wins; anything unproven is unknown, and
// only ChannelStandalone is ever replaced.
func Detect(in DetectInput) Installation {
	inst := Installation{
		Executable: in.Executable,
		InvokedAs:  in.InvokedAs,
		Version:    in.Build.Version,
		Kind:       in.Build.Kind,
	}
	inst.Channel, inst.Evidence, inst.NPMProjectDir = classify(in)
	if inst.Channel == ChannelGo && in.Getenv != nil {
		inst.GoInstallDir = goInstallDir(in.Getenv)
	}
	return inst
}

// goInstallDir mirrors where the go command installs binaries.
func goInstallDir(getenv func(string) string) string {
	if bin := getenv("GOBIN"); bin != "" {
		return bin
	}
	if gopath := getenv("GOPATH"); gopath != "" {
		first, _, _ := strings.Cut(gopath, string(os.PathListSeparator))
		return filepath.Join(first, "bin")
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, "go", "bin")
	}
	return ""
}

func classify(in DetectInput) (Channel, string, string) {
	p := in.Executable
	// Rule 1: a source build is never replaced, wherever it sits.
	if in.Build.Kind == version.KindDevelopment {
		return ChannelDevelopment, fmt.Sprintf("development build (%s)", in.Build.Version), ""
	}
	// Rule 2: ko puts every image's binary at the same entrypoint.
	if p == koEntrypoint {
		return ChannelContainer, "container image entrypoint " + koEntrypoint, ""
	}
	// Rule 3: the cask (and a formula, should one exist) stages under the
	// Homebrew prefix; symlinks in bin/ are already resolved.
	for _, marker := range []string{"/Caskroom/gskill/", "/Cellar/gskill/"} {
		if strings.Contains(p, marker) {
			return ChannelHomebrew, "path contains " + marker, ""
		}
	}
	// Rule 4: the npm launcher runs a binary from a platform package.
	if m := npmBinary.FindStringSubmatch(p); m != nil {
		return classifyNPM(in, p, m[1])
	}
	// Rule 5: go install builds from a module zip, so it has a module version
	// and no VCS data (version.Kind).
	if in.Build.Kind == version.KindModule && in.Build.ModulePath == gskillModule {
		return ChannelGo, fmt.Sprintf("go install build (%s)", in.Build.Version), ""
	}
	// Rule 6: tool managers such as mise, asdf, and aqua install into
	// directories named after the version; replacing a file there would leave
	// the manager's records wrong.
	if dir := managedDir(p, in.Build.Version); dir != "" {
		return ChannelUnknown, fmt.Sprintf("path is managed by another tool (%s)", dir), ""
	}
	// The install script never writes to the system directories, so a binary
	// there was put in place by the OS package manager.
	if dir := filepath.Dir(p); systemDirs[dir] {
		return ChannelUnknown, fmt.Sprintf("path is in a system directory owned by the OS package manager (%s)", dir), ""
	}
	// Rule 7: only the release pipeline injects a version.
	if in.Build.Kind == version.KindRelease {
		return ChannelStandalone, "release build outside any package-managed location", ""
	}
	return ChannelUnknown, "no evidence of how this gskill was installed", ""
}

func classifyNPM(in DetectInput, p, root string) (Channel, string, string) {
	switch {
	case strings.Contains(p, "/_npx/"):
		return ChannelNPX, "npx cache", ""
	case strings.Contains(p, "/lib/node_modules/@glapsfun/gskill/node_modules/"):
		return ChannelNPMGlobal, "global npm package", ""
	case declaresGskill(in.ReadFile, filepath.Join(root, "package.json")):
		return ChannelNPMProject, "npm dependency of " + root, root
	default:
		return ChannelNPM, "npm package (kind unknown)", ""
	}
}

// declaresGskill reports whether the package.json at path lists the gskill
// package. A missing, oversized, or unparsable file is no evidence.
func declaresGskill(readFile func(string) ([]byte, error), path string) bool {
	if readFile == nil {
		return false
	}
	data, err := readFile(path)
	if err != nil || len(data) > maxPackageJSON {
		return false
	}
	var pkg map[string]json.RawMessage
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	for _, field := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		var deps map[string]any
		if raw, ok := pkg[field]; ok && json.Unmarshal(raw, &deps) == nil {
			if _, ok := deps["@glapsfun/gskill"]; ok {
				return true
			}
		}
	}
	return false
}

// managedDir returns the directory that marks p as owned by another tool, or
// "". Only whole directory names are compared, so /opt/gskill-0.9.0 is not a
// match.
func managedDir(p, ver string) string {
	if rest, ok := strings.CutPrefix(p, "/nix/store/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		return "/nix/store/" + first
	}
	bare := strings.TrimPrefix(ver, "v")
	if bare == "" {
		return ""
	}
	for dir := filepath.Dir(p); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if name := filepath.Base(dir); name == bare || name == "v"+bare {
			return dir
		}
	}
	return ""
}

// ResolveExecutable returns the running executable with symlinks resolved, and
// the path it was invoked through when that is a different (symlinked) path.
func ResolveExecutable(exe func() (string, error), args0 string, lookPath func(string) (string, error)) (executable, invokedAs string, err error) {
	p, err := exe()
	if err != nil {
		return "", "", errs.Wrap(errs.CodeGeneric, "cannot locate the running executable", err)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", "", errs.Wrap(errs.CodeGeneric, "cannot locate the running executable", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", errs.Wrap(errs.CodeGeneric, "cannot locate the running executable", err)
	}

	return resolved, invokedThrough(args0, resolved, lookPath), nil
}

// invokedThrough returns the path args0 names when it is a symlink to resolved,
// or "" when it is resolved itself or points somewhere else.
func invokedThrough(args0, resolved string, lookPath func(string) (string, error)) string {
	candidate := args0
	if candidate != "" && !strings.Contains(candidate, "/") && lookPath != nil {
		found, err := lookPath(candidate)
		if err != nil {
			return ""
		}
		candidate = found
	}
	if candidate == "" {
		return ""
	}
	abs, err := filepath.Abs(candidate)
	if err != nil || abs == resolved {
		return ""
	}
	if target, err := filepath.EvalSymlinks(abs); err == nil && target == resolved {
		return abs
	}
	return ""
}

// Shadow returns the gskill that PATH finds first when it is a different
// executable from the one running, so the user knows `gskill version` there
// will not change. It is never modified.
func Shadow(ctx context.Context, env Env, executable string) *ShadowExecutable {
	if env.LookPath == nil {
		return nil
	}
	found, err := env.LookPath("gskill")
	if err != nil {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(found)
	if err != nil {
		return nil
	}
	other, err := os.Stat(resolved)
	if err != nil {
		return nil
	}
	self, err := os.Stat(executable)
	if err != nil || os.SameFile(other, self) {
		return nil
	}
	shadow := &ShadowExecutable{Path: found, Version: "unknown"}
	if env.Probe != nil {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if v, err := env.Probe(probeCtx, resolved); err == nil {
			shadow.Version = v
		}
	}
	return shadow
}
