package app

import (
	"reflect"
	"testing"

	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// backfillRequested maps a resolved revision onto an empty tracking intent by
// ref-kind, and never overwrites explicit values.
func TestBackfillRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   skillslock.Requested
		rev  resolver.Revision
		want skillslock.Requested
	}{
		{
			name: "semver resolution records a floating caret range",
			in:   skillslock.Requested{},
			rev:  resolver.Revision{RefKind: resolver.RefKindSemver, Version: "0.1.0"},
			want: skillslock.Requested{Version: "^0.1.0"},
		},
		{
			name: "tag resolution records the tag as ref",
			in:   skillslock.Requested{},
			rev:  resolver.Revision{RefKind: resolver.RefKindTag, Tag: "release-1"},
			want: skillslock.Requested{Ref: "release-1"},
		},
		{
			name: "branch resolution records the branch as ref",
			in:   skillslock.Requested{},
			rev:  resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main"},
			want: skillslock.Requested{Ref: "main"},
		},
		{
			name: "commit resolution records the commit",
			in:   skillslock.Requested{},
			rev:  resolver.Revision{RefKind: resolver.RefKindCommit, Commit: "abc123"},
			want: skillslock.Requested{Commit: "abc123"},
		},
		{
			name: "local source stays unpinned",
			in:   skillslock.Requested{},
			rev:  resolver.Revision{RefKind: resolver.RefKindLocal},
			want: skillslock.Requested{},
		},
		{
			name: "explicit version is never overwritten",
			in:   skillslock.Requested{Version: "^2.0.0"},
			rev:  resolver.Revision{RefKind: resolver.RefKindSemver, Version: "0.1.0"},
			want: skillslock.Requested{Version: "^2.0.0"},
		},
		{
			name: "explicit ref is never overwritten",
			in:   skillslock.Requested{Ref: "develop"},
			rev:  resolver.Revision{RefKind: resolver.RefKindBranch, Branch: "main"},
			want: skillslock.Requested{Ref: "develop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := backfillRequested(tt.in, tt.rev)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("backfillRequested() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
