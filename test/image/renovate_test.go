package image

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

type renovateManager struct {
	ManagerFilePatterns []string `json:"managerFilePatterns"`
	MatchStrings        []string `json:"matchStrings"`
	DatasourceTemplate  string   `json:"datasourceTemplate"`
}

// managerFor returns the custom manager whose file pattern matches path.
// Renovate writes the patterns as /regex/.
func managerFor(t *testing.T, path string) renovateManager {
	t.Helper()
	var cfg struct {
		CustomManagers []renovateManager `json:"customManagers"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, "renovate.json")), &cfg); err != nil {
		t.Fatalf("parse renovate.json: %v", err)
	}
	for _, m := range cfg.CustomManagers {
		for _, p := range m.ManagerFilePatterns {
			if regexp.MustCompile(strings.Trim(p, "/")).MatchString(path) {
				return m
			}
		}
	}
	t.Fatalf("renovate.json has no custom manager for %s", path)
	return renovateManager{}
}

// captures returns the named groups of every match of the manager's
// matchStrings in content.
func captures(t *testing.T, m renovateManager, content string) []map[string]string {
	t.Helper()
	var all []map[string]string
	for _, s := range m.MatchStrings {
		re := regexp.MustCompile(s)
		for _, match := range re.FindAllStringSubmatch(content, -1) {
			groups := map[string]string{}
			for i, name := range re.SubexpNames() {
				if name != "" {
					groups[name] = match[i]
				}
			}
			all = append(all, groups)
		}
	}
	return all
}

func TestRenovateTracksPins(t *testing.T) {
	t.Parallel()

	t.Run("ko base image digest", func(t *testing.T) {
		t.Parallel()

		m := managerFor(t, ".ko.yaml")
		if m.DatasourceTemplate != "docker" {
			t.Errorf("datasourceTemplate = %q, want docker", m.DatasourceTemplate)
		}
		_, digest, _ := strings.Cut(readKoConfig(t).DefaultBaseImage, "@")
		got := captures(t, m, readRepoFile(t, ".ko.yaml"))
		if len(got) != 1 {
			t.Fatalf("matches in .ko.yaml = %v, want exactly one", got)
		}
		want := map[string]string{"depName": "cgr.dev/chainguard/git", "currentValue": "latest", "currentDigest": digest}
		for name, value := range want {
			if got[0][name] != value {
				t.Errorf("%s = %q, want %q", name, got[0][name], value)
			}
		}
	})

	// Every NAME=version pin, ko and crane included, must carry an annotation
	// the manager captures, or Renovate never updates it.
	t.Run("every tool pin", func(t *testing.T) {
		t.Parallel()

		content := readRepoFile(t, ".config", "tool-versions")
		captured := map[string]bool{}
		for _, g := range captures(t, managerFor(t, ".config/tool-versions"), content) {
			captured[g["currentValue"]] = true
		}
		for _, line := range strings.Split(content, "\n") {
			name, version, ok := strings.Cut(line, "=")
			if !ok || strings.HasPrefix(name, "#") {
				continue
			}
			if !captured[version] {
				t.Errorf("Renovate does not capture the %s pin (%s)", name, version)
			}
		}
	})
}
