package image

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	versionVar = "github.com/glapsfun/gskill/internal/version.version"
	commitVar  = "github.com/glapsfun/gskill/internal/version.commit"
)

// goBuild is the subset of a build entry that .ko.yaml and .goreleaser.yaml share.
type goBuild struct {
	ID      string   `json:"id"`
	Main    string   `json:"main"`
	Env     []string `json:"env"`
	Flags   []string `json:"flags"`
	Ldflags []string `json:"ldflags"`
}

type koConfig struct {
	DefaultBaseImage string    `json:"defaultBaseImage"`
	DefaultPlatforms []string  `json:"defaultPlatforms"`
	Builds           []goBuild `json:"builds"`
}

func readKoConfig(t *testing.T) koConfig {
	t.Helper()
	var cfg koConfig
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".ko.yaml")), &cfg); err != nil {
		t.Fatalf("parse .ko.yaml: %v", err)
	}
	return cfg
}

// gskillBuild returns the build with id gskill from a ko or GoReleaser config.
func gskillBuild(t *testing.T, file string) goBuild {
	t.Helper()
	var cfg struct {
		Builds []goBuild `json:"builds"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, file)), &cfg); err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, b := range cfg.Builds {
		if b.ID == "gskill" {
			return b
		}
	}
	t.Fatalf("%s has no build with id gskill", file)
	return goBuild{}
}

// xFlags maps each -X variable in the ldflags tokens to its value.
func xFlags(tokens []string) map[string]string {
	vars := map[string]string{}
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i] != "-X" {
			continue
		}
		name, value, _ := strings.Cut(tokens[i+1], "=")
		vars[name] = value
	}
	return vars
}

func TestKoConfigMatchesGoReleaser(t *testing.T) {
	t.Parallel()

	for _, file := range []string{".ko.yaml", ".goreleaser.yaml"} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			b := gskillBuild(t, file)
			if b.Main != "./cmd/gskill" {
				t.Errorf("main = %q, want ./cmd/gskill", b.Main)
			}
			if !slices.Contains(b.Env, "CGO_ENABLED=0") {
				t.Errorf("env = %v, want CGO_ENABLED=0", b.Env)
			}
			if !slices.Contains(b.Flags, "-trimpath") {
				t.Errorf("flags = %v, want -trimpath", b.Flags)
			}
			// GoReleaser keeps its ldflags in one folded entry, ko in one entry per flag.
			tokens := strings.Fields(strings.Join(b.Ldflags, " "))
			for _, f := range []string{"-s", "-w"} {
				if !slices.Contains(tokens, f) {
					t.Errorf("ldflags = %v, want %s", b.Ldflags, f)
				}
			}
			vars := xFlags(tokens)
			for _, v := range []string{versionVar, commitVar} {
				if _, ok := vars[v]; !ok {
					t.Errorf("ldflags = %v, want -X %s=...", b.Ldflags, v)
				}
			}
		})
	}

	t.Run("ko version metadata", func(t *testing.T) {
		t.Parallel()

		b := gskillBuild(t, ".ko.yaml")
		vars := xFlags(strings.Fields(strings.Join(b.Ldflags, " ")))
		want := map[string]string{
			versionVar: "{{.Env.GSKILL_VERSION}}",
			commitVar:  "{{.Git.ShortCommit}}",
		}
		for name, value := range want {
			if vars[name] != value {
				t.Errorf("-X %s = %q, want %q", name, vars[name], value)
			}
		}
	})
}

func TestKoConfigPinsBase(t *testing.T) {
	t.Parallel()

	t.Run("base pinned by digest", func(t *testing.T) {
		t.Parallel()

		pinned := regexp.MustCompile(`^cgr\.dev/chainguard/git:latest@sha256:[0-9a-f]{64}$`)
		if got := readKoConfig(t).DefaultBaseImage; !pinned.MatchString(got) {
			t.Errorf("defaultBaseImage = %q, want %s", got, pinned)
		}
	})

	t.Run("platforms", func(t *testing.T) {
		t.Parallel()

		want := []string{"linux/amd64", "linux/arm64"}
		if got := readKoConfig(t).DefaultPlatforms; !slices.Equal(got, want) {
			t.Errorf("defaultPlatforms = %v, want %v", got, want)
		}
	})
}
