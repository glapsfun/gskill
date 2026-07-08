package cli

import (
	"testing"

	"github.com/glapsfun/gskill/internal/tui"
)

func TestSkillRowCarriesLockfileColumns(t *testing.T) {
	t.Parallel()
	r := tui.SkillRow{Name: "a", Version: "1.2.0", Source: "acme/skills", Status: "installed"}
	if r.Version != "1.2.0" || r.Source != "acme/skills" {
		t.Fatalf("row = %+v", r)
	}
}
