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

func TestKindFor(t *testing.T) {
	t.Parallel()

	pseudo := "v0.9.1-0.20261103120000-abcdef123456"
	tests := []struct {
		name          string
		injected      string
		moduleVersion string
		hasVCS        bool
		want          BuildKind
	}{
		{name: "injected version is a release", injected: "0.9.0", want: KindRelease},
		{name: "go install tag build", injected: "dev", moduleVersion: "v0.9.0", want: KindModule},
		{name: "go install pseudo-version build", injected: "dev", moduleVersion: pseudo, want: KindModule},
		{name: "local checkout build", injected: "dev", moduleVersion: pseudo + "+dirty", hasVCS: true, want: KindDevelopment},
		{name: "devel placeholder", injected: "dev", moduleVersion: "(devel)", want: KindDevelopment},
		{name: "no build info", injected: "dev", want: KindDevelopment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := kindFor(tt.injected, tt.moduleVersion, tt.hasVCS); got != tt.want {
				t.Errorf("kindFor(%q, %q, %v) = %q, want %q", tt.injected, tt.moduleVersion, tt.hasVCS, got, tt.want)
			}
		})
	}
}

func TestKind_TestBinaryIsNotRelease(t *testing.T) {
	t.Parallel()

	if got := Kind(); got == KindRelease {
		t.Errorf("Kind() = %q under go test, want a non-release kind", got)
	}
}
