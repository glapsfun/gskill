package installer_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/installer"
)

func TestScope_Valid(t *testing.T) {
	t.Parallel()

	if !installer.ScopeProject.Valid() || !installer.ScopeGlobal.Valid() {
		t.Error("expected project and global scopes to be valid")
	}
	if installer.Scope("system").Valid() {
		t.Error(`Scope("system").Valid() = true, want false`)
	}
}

func TestMode_Valid(t *testing.T) {
	t.Parallel()

	if !installer.ModeSymlink.Valid() || !installer.ModeCopy.Valid() {
		t.Error("expected symlink and copy modes to be valid")
	}
	if installer.Mode("auto").Valid() {
		t.Error(`Mode("auto").Valid() = true, want false (auto is a preference, never recorded)`)
	}
}
