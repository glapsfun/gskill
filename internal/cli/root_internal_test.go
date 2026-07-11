package cli

import "testing"

func TestStyledUsageError_IdentityWithoutColor(t *testing.T) {
	t.Parallel()

	errLine, hintLine := styledUsageError(false, "unexpected argument --foo")
	if errLine != "unexpected argument --foo" {
		t.Errorf("errLine = %q, want unchanged message", errLine)
	}
	if hintLine != "Run 'gskill --help' for usage." {
		t.Errorf("hintLine = %q, want unchanged hint", hintLine)
	}
}

// TestNoInteractiveRequested locks the fix for a real bug: kong never
// applies flag values to root on a failed parse (Apply only runs after a
// fully successful trace), so root.NoInteractive stays false on a usage
// error regardless of whether --no-interactive was typed, and regardless of
// where in argv it appeared relative to the offending token.
func TestNoInteractiveRequested(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"badcmd", "--no-interactive"}, true},
		{[]string{"--no-interactive", "badcmd"}, true},
		{[]string{"badcmd"}, false},
		{[]string{}, false},
		{[]string{"--no-interactive"}, true},
	}
	for _, c := range cases {
		if got := noInteractiveRequested(c.args); got != c.want {
			t.Errorf("noInteractiveRequested(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
