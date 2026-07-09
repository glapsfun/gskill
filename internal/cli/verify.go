package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
)

// verifyCmd re-hashes installed content against the lockfile.
type verifyCmd struct{}

// Help returns the detailed help shown by `gskill project verify --help`.
func (verifyCmd) Help() string {
	return examplesHelp(
		"gskill project verify",
	)
}

// Run executes `gskill project verify` (alias: `gskill verify`).
func (verifyCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	report, err := a.Verify(ctx, string(root))
	// Render the report regardless, then propagate the integrity error's code.
	skills := make([]map[string]any, 0, len(report.Skills))
	for _, s := range report.Skills {
		skills = append(skills, map[string]any{
			"name":   s.Name,
			"ok":     s.OK,
			"issue":  s.Issue,
			"actual": s.Actual,
		})
		if !s.OK {
			out.Diag("verify: %s %s (expected %s)", s.Name, s.Issue, s.Expected)
		}
	}

	human := out.summary(fmt.Sprintf("Verified %d skill(s): all OK", len(report.Skills)))
	if !report.OK {
		human = out.errSummary("Integrity verification FAILED")
	}
	if rErr := out.Result(human, map[string]any{"ok": report.OK, "skills": skills}); rErr != nil {
		return rErr
	}
	if err != nil && errors.Is(err, errs.ErrIntegrity) {
		return errs.WithHint(err, "run 'gskill project repair' to re-materialize broken installs")
	}
	return err
}
