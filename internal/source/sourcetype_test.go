package source_test

import (
	"testing"

	"github.com/glapsfun/gskill/internal/source"
)

func TestType_Valid(t *testing.T) {
	t.Parallel()

	valid := []source.Type{source.TypeGit, source.TypeLocal, source.TypeURL}
	for _, st := range valid {
		if !st.Valid() {
			t.Errorf("%q.Valid() = false, want true", st)
		}
	}

	if source.Type("archive").Valid() {
		t.Error(`Type("archive").Valid() = true, want false`)
	}
	if source.Type("").Valid() {
		t.Error(`Type("").Valid() = true, want false`)
	}
}
