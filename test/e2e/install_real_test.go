//go:build e2e

package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestE2E_InstallAllSkills covers scenario 1: installing every skill the repo
// offers. The expected set is derived from discovery (not hardcoded), so the
// suite survives upstream additions (FR-010/FR-012).
func TestE2E_InstallAllSkills(t *testing.T) {
	requireE2E(t)

	proj := newProject(t)

	// Discover what the repo offers.
	stdout, stderr, code := runGskill(t, proj, "add", repoURL, "--skill", "*", "--max-depth", "0", "--list", "--json")
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, stderr)
	}
	var listed []struct {
		ID    string `json:"id"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("list json: %v\n%s", err, stdout)
	}
	want := 0
	for _, s := range listed {
		if s.Valid {
			want++
		}
	}
	if want == 0 {
		t.Fatal("discovery found no valid skills")
	}

	// Install all of them.
	if _, stderr, code := runGskill(t, proj, "add", repoURL, "--skill", "*", "--max-depth", "0"); code != 0 {
		t.Fatalf("add all exit %d: %s", code, stderr)
	}

	if got := countStoreEntries(t, proj); got != want {
		t.Errorf("store entries = %d, want one per discovered skill (%d)", got, want)
	}
	// Every known skill present in the repo must be installed for claude.
	for skill := range knownSkills {
		assertChain(t, proj, skill, "claude")
	}
}

// TestE2E_OneSkillOneAgent covers scenario 2: a single named skill for a single
// agent, with the manifest recording the agent set and version pin.
func TestE2E_OneSkillOneAgent(t *testing.T) {
	requireE2E(t)

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repoURL, "--skill", "argocd", "--agent", "claude"); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	assertChain(t, proj, "argocd", "claude")
	if got := countStoreEntries(t, proj); got != 1 {
		t.Errorf("store entries = %d, want 1", got)
	}
	entry := section(readManifest(t, proj), "[skills.argocd]")
	if !strings.Contains(entry, "agents = ['claude']") {
		t.Errorf("argocd entry missing agents = ['claude']:\n%s", entry)
	}
}

// TestE2E_TwoSkillsMultipleAgents covers scenario 3: two named skills for two
// agents, each skill stored exactly once (SC-002).
func TestE2E_TwoSkillsMultipleAgents(t *testing.T) {
	requireE2E(t)

	proj := newProject(t)
	if _, stderr, code := runGskill(t, proj, "add", repoURL,
		"--skill", "argocd", "--skill", "fluxcd",
		"--agent", "claude", "--agent", "codex"); code != 0 {
		t.Fatalf("add exit %d: %s", code, stderr)
	}

	assertChain(t, proj, "argocd", "claude", "codex")
	assertChain(t, proj, "fluxcd", "claude", "codex")
	if got := countStoreEntries(t, proj); got != 2 {
		t.Errorf("store entries = %d, want 2 (one per skill, shared across agents)", got)
	}
}
