package cli

import "testing"

func TestExamplesHelp_FormatsPreformattedBlock(t *testing.T) {
	t.Parallel()

	got := examplesHelp(
		"gskill add github.com/owner/repo --agent claude",
		"gskill add ./local/path --all",
	)
	want := "Examples:\n" +
		"\n\tgskill add github.com/owner/repo --agent claude" +
		"\n\tgskill add ./local/path --all"
	if got != want {
		t.Errorf("examplesHelp() = %q, want %q", got, want)
	}
}
