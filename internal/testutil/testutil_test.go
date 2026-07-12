package testutil_test

import (
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/testutil"
)

func TestGitEnv_StripsAmbientGitVars(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/somewhere")
	t.Setenv("GIT_INDEX_FILE", "/somewhere/.git/index")
	t.Setenv("PROBE_KEEP_ME", "1")

	env := testutil.GitEnv("GIT_AUTHOR_NAME=t")

	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") && kv != "GIT_AUTHOR_NAME=t" {
			t.Errorf("GitEnv leaked ambient var: %q", kv)
		}
	}
	if !contains(env, "GIT_AUTHOR_NAME=t") {
		t.Errorf("GitEnv dropped its own extra: %v", env)
	}
	if !contains(env, "PROBE_KEEP_ME=1") {
		t.Errorf("GitEnv dropped a non-GIT_ ambient var: %v", env)
	}
}

func TestGitEnv_NoExtrasStillStrips(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/.git")

	env := testutil.GitEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") {
			t.Errorf("GitEnv() leaked ambient var: %q", kv)
		}
	}
}

func contains(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
