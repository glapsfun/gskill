// Package version exposes the gskill build version and source commit.
package version

import "runtime/debug"

// version and commit are overridable at build time via
// -ldflags "-X github.com/glapsfun/gskill/internal/version.version=... -X ...version.commit=...".
// Releases (GoReleaser) inject both. For `go install` builds, where no ldflags are
// set, Version and Commit fall back to the binary's embedded build info.
var (
	version = "dev"
	commit  = ""
)

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
	if injected != "dev" {
		return injected
	}
	if moduleVersion != "" && moduleVersion != "(devel)" {
		return moduleVersion
	}
	return "dev"
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
