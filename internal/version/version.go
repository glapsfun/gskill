// Package version exposes the gskill build version and source commit.
package version

import (
	"runtime/debug"
	"strings"
)

// version and commit are overridable at build time via
// -ldflags "-X github.com/glapsfun/gskill/internal/version.version=... -X ...version.commit=...".
// Releases (GoReleaser) inject both. For `go install` builds, where no ldflags are
// set, Version and Commit fall back to the binary's embedded build info.
var (
	version = devVersion
	commit  = ""
)

// devVersion is the placeholder version of a build without ldflags.
const devVersion = "dev"

// Version returns the resolved build version: the injected value when present,
// otherwise the module version embedded in the binary's build info, otherwise "dev".
func Version() string {
	return resolveVersion(version, moduleVersion())
}

// Commit returns the resolved source commit: the injected value when present,
// otherwise the VCS revision from the binary's build info (suffixed "+dirty" when the
// working tree was modified), otherwise an empty string.
func Commit() string {
	rev, modified := vcsRevision()
	return resolveCommit(commit, rev, modified)
}

// BuildKind says where the running binary's version came from.
type BuildKind string

const (
	// KindRelease is a binary built by the release pipeline, which injects the
	// version through ldflags.
	KindRelease BuildKind = "release"
	// KindModule is a `go install pkg@version` build: the version comes from the
	// module, and there is no VCS data because it was built from a module zip.
	KindModule BuildKind = "module"
	// KindDevelopment is any other build, such as a local `go build` in a checkout.
	KindDevelopment BuildKind = "development"
)

// Kind reports how the binary was built. A local `go build` in a checkout has
// stamped a module version from VCS since Go 1.24, so only the presence of VCS
// settings tells it apart from `go install`.
func Kind() BuildKind {
	return kindFor(version, moduleVersion(), HasVCS())
}

// ModulePath returns the main module path from the build info, or "".
func ModulePath() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Path
}

// HasVCS reports whether the build info carries any vcs.* setting.
func HasVCS() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range info.Settings {
		if strings.HasPrefix(s.Key, "vcs.") {
			return true
		}
	}
	return false
}

func kindFor(injected, moduleVersion string, hasVCS bool) BuildKind {
	switch {
	case injected != devVersion:
		return KindRelease
	case hasVCS:
		return KindDevelopment
	case moduleVersion != "" && moduleVersion != "(devel)":
		return KindModule
	default:
		return KindDevelopment
	}
}

// String returns the human-readable version line, e.g. "gskill dev".
func String() string {
	return stringFor(Version())
}

func stringFor(v string) string {
	return "gskill " + v
}

// resolveVersion picks the injected version when it is not the "dev" default, else
// the module version when it is a real release (not empty and not the "(devel)"
// placeholder), else "dev".
func resolveVersion(injected, moduleVersion string) string {
	if injected != devVersion {
		return injected
	}
	if moduleVersion != "" && moduleVersion != "(devel)" {
		return moduleVersion
	}
	return devVersion
}

// resolveCommit prefers the injected commit, else the VCS revision (suffixed
// "+dirty" when the tree was modified), else an empty string.
func resolveCommit(injected, revision string, modified bool) string {
	if injected != "" {
		return injected
	}
	if revision == "" {
		return ""
	}
	if modified {
		return revision + "+dirty"
	}
	return revision
}

// moduleVersion reports the main module version from the embedded build info, if any.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

// vcsRevision reports the VCS revision and dirty flag from the embedded build info.
func vcsRevision() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return revision, modified
}
