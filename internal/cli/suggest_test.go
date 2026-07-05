package cli

import (
	"strings"
	"testing"
)

func TestUnknownCommand_Suggestions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		arg         string
		wantSuggest string // empty means: no suggestion at all
	}{
		{"typo of canonical", "instal", "install"},
		{"transposed canonical", "serach", "search"},
		{"typo of rename alias resolves to canonical", "fnd", "search"},
		{"typo of tui alias resolves to canonical", "tuii", "dashboard"},
		{"typo of regrouped alias resolves to project form", "sinc", "project sync"},
		{"garbage gets no suggestion", "zzzzqqq", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, code := runCLI(t, nil, tt.arg)
			if code != 2 {
				t.Fatalf("gskill %s: exit code = %d, want 2 (usage)", tt.arg, code)
			}
			if stdout != "" {
				t.Errorf("gskill %s: stdout = %q, want empty (diagnostics belong on stderr)", tt.arg, stdout)
			}
			if tt.wantSuggest == "" {
				if strings.Contains(stderr, "did you mean") {
					t.Errorf("gskill %s: unexpected suggestion in %q", tt.arg, stderr)
				}
			} else {
				want := `did you mean "` + tt.wantSuggest + `"?`
				if !strings.Contains(stderr, want) {
					t.Errorf("gskill %s: stderr = %q, want it to contain %q", tt.arg, stderr, want)
				}
			}
			if !strings.Contains(stderr, "gskill --help") {
				t.Errorf("gskill %s: stderr = %q, want pointer to 'gskill --help'", tt.arg, stderr)
			}
		})
	}
}

func TestTrailingArgument_GetsNoTopLevelSuggestion(t *testing.T) {
	t.Parallel()

	// A stray argument after a valid command is not a top-level typo: the
	// alias-aware suggester must stay out of the way and leave kong's
	// context-aware message untouched.
	for _, args := range [][]string{
		{"version", "check"},
		{"doctor", "list"},
	} {
		stdout, stderr, code := runCLI(t, nil, args...)
		if code != 2 {
			t.Errorf("gskill %v: exit code = %d, want 2", args, code)
		}
		if stdout != "" {
			t.Errorf("gskill %v: stdout = %q, want empty", args, stdout)
		}
		if strings.Contains(stderr, `did you mean "project`) {
			t.Errorf("gskill %v: stderr = %q, wrongly suggests a project command for a trailing argument", args, stderr)
		}
	}
}

func TestBareGroup_JSONModeStaysStrict(t *testing.T) {
	t.Parallel()

	// Machine consumers need the usage error and a clean stdout: the bare-
	// group help courtesy must not fire under --json (FR-011/FR-035).
	for _, args := range [][]string{
		{"--json", "project"},
		{"--json", "cache"},
	} {
		stdout, stderr, code := runCLI(t, nil, args...)
		if code != 2 {
			t.Errorf("gskill %v: exit code = %d, want 2 (strict usage error in --json mode)", args, code)
		}
		if stdout != "" {
			t.Errorf("gskill %v: stdout = %q, want empty (no human help on --json stdout)", args, stdout)
		}
		if stderr == "" {
			t.Errorf("gskill %v: stderr empty, want the usage error", args)
		}
	}
}

func TestFlagsOnlyInvocation_IsAUsageError(t *testing.T) {
	t.Parallel()

	// Flags without a command never selected a group node, so the help
	// courtesy must not fire; this preserves the pre-existing exit-2 contract.
	stdout, _, code := runCLI(t, nil, "--offline")
	if code != 2 {
		t.Errorf("gskill --offline: exit code = %d, want 2", code)
	}
	if strings.Contains(stdout, "Usage:") {
		t.Errorf("gskill --offline: stdout = %q, must not print help on a usage error", stdout)
	}
}

func TestUnknownCommand_SuggestionsAreDeterministic(t *testing.T) {
	t.Parallel()

	var first string
	for i := range 5 {
		_, stderr, _ := runCLI(t, nil, "instal")
		if i == 0 {
			first = stderr
			continue
		}
		if stderr != first {
			t.Fatalf("suggestion output changed between runs:\nfirst: %q\nrun %d: %q", first, i+1, stderr)
		}
	}
}
