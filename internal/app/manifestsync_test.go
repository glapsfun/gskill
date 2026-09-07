package app

import (
	"reflect"
	"testing"

	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
)

func TestIntentFromDeclaration(t *testing.T) {
	t.Parallel()

	rec := skillslock.Record{
		Source:       skillslock.Source{Original: "github.com/acme/old", Path: "tools/demo"},
		Requested:    skillslock.Requested{Version: "^1.0.0"},
		Installation: skillslock.Installation{Mode: "symlink", Scope: "global", Agents: []string{"claude"}},
	}

	t.Run("declaration wins over the record", func(t *testing.T) {
		t.Parallel()
		decl := manifest.Skill{
			Name: "demo", Source: "github.com/acme/new", Skill: "skills/demo",
			Version: "^2.0.0", Mode: "copy", Agents: []string{"cursor", "claude"},
		}
		got := intentFromDeclaration(decl, rec)
		want := skillIntent{
			Source: "github.com/acme/new", Path: "skills/demo", Version: "^2.0.0",
			Mode: "copy", Scope: "global", Agents: []string{"cursor", "claude"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})

	t.Run("record fills what the declaration omits", func(t *testing.T) {
		t.Parallel()
		got := intentFromDeclaration(manifest.Skill{Name: "demo", Commit: "abc"}, rec)
		want := skillIntent{
			Source: "github.com/acme/old", Path: "tools/demo", Commit: "abc",
			Mode: "symlink", Scope: "global", Agents: []string{"claude"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}
