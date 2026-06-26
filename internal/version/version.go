// Package version exposes the gskill build version.
package version

// version is overridable at build time via -ldflags "-X ...version.version=...".
var version = "dev"

// Version returns the build version string.
func Version() string {
	return version
}

// String returns the human-readable version line, e.g. "gskill dev".
func String() string {
	return stringFor(version)
}

func stringFor(v string) string {
	return "gskill " + v
}
