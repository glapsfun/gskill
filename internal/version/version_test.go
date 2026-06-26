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
