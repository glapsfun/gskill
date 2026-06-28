package version

import "testing"

func TestVersion_DefaultsToDev(t *testing.T) {
	t.Parallel()

	if got := Version(); got != "dev" {
		t.Errorf("Version() = %q, want %q", got, "dev")
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "default", version: "dev", want: "gskill dev"},
		{name: "semver", version: "v1.2.3", want: "gskill v1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stringFor(tt.version); got != tt.want {
				t.Errorf("stringFor(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		injected      string
		moduleVersion string
		want          string
	}{
		{name: "injected release wins", injected: "v1.2.3", moduleVersion: "", want: "v1.2.3"},
		{name: "injected wins over build info", injected: "v1.2.3", moduleVersion: "v9.9.9", want: "v1.2.3"},
		{name: "dev plus empty module version stays dev", injected: "dev", moduleVersion: "", want: "dev"},
		{name: "dev plus devel module version stays dev", injected: "dev", moduleVersion: "(devel)", want: "dev"},
		{name: "dev plus real module version uses it", injected: "dev", moduleVersion: "v0.3.0", want: "v0.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveVersion(tt.injected, tt.moduleVersion); got != tt.want {
				t.Errorf("resolveVersion(%q, %q) = %q, want %q", tt.injected, tt.moduleVersion, got, tt.want)
			}
		})
	}
}

func TestResolveCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		injected string
		revision string
		modified bool
		want     string
	}{
		{name: "injected wins", injected: "abc1234", revision: "deadbeef", modified: false, want: "abc1234"},
		{name: "revision when not injected", injected: "", revision: "deadbeef", modified: false, want: "deadbeef"},
		{name: "revision marked dirty", injected: "", revision: "deadbeef", modified: true, want: "deadbeef+dirty"},
		{name: "empty when nothing available", injected: "", revision: "", modified: false, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveCommit(tt.injected, tt.revision, tt.modified); got != tt.want {
				t.Errorf("resolveCommit(%q, %q, %v) = %q, want %q", tt.injected, tt.revision, tt.modified, got, tt.want)
			}
		})
	}
}
