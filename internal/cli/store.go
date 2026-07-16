package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/tui"
)

// storeCmd groups global-store management subcommands (spec 015 FR-035).
type storeCmd struct {
	Verify storeVerifyCmd `cmd:"" help:"Verify every global store object's integrity."`
	Repair storeRepairCmd `cmd:"" help:"Restore a corrupted object from its recorded origin."`
}

// Help returns the detailed help shown by `gskill store --help`.
func (storeCmd) Help() string {
	return examplesHelp("gskill store verify", "gskill store repair sha256:abc123…")
}

type storeVerifyCmd struct{}

// Help returns the detailed help shown by `gskill store verify --help`.
func (storeVerifyCmd) Help() string {
	return examplesHelp("gskill store verify", "gskill store verify --json")
}

// Run scans the global store and reports per-object health (FR-022). Any
// corrupted or malformed finding exits non-zero.
func (storeVerifyCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	res, err := a.StoreVerify(ctx)
	if err != nil {
		return err
	}

	findings := make([]map[string]any, 0, len(res.Findings))
	for _, f := range res.Findings {
		entry := map[string]any{
			"kind":   string(f.Kind),
			"detail": f.Detail,
		}
		if f.Key != "" {
			entry["object"] = f.Key
		}
		if f.Path != "" {
			entry["path"] = f.Path
		}
		if f.Expected != "" {
			entry["expected"] = f.Expected
			entry["actual"] = f.Actual
		}
		if len(f.UsedBy) > 0 {
			entry["usedBy"] = f.UsedBy
		}
		findings = append(findings, entry)
	}
	doc := map[string]any{
		"path":     res.Path,
		"checked":  res.Checked,
		"healthy":  res.Healthy,
		"findings": findings,
	}
	if err := out.Result(humanStoreVerify(res), doc); err != nil {
		return err
	}
	if res.Failed() {
		return errs.New(errs.CodeIntegrity, "global store verification found problems")
	}
	return nil
}

// humanStoreVerify renders the scan report. All store-derived strings are
// sanitized before display (FR-034).
func humanStoreVerify(res app.StoreVerifyResult) string {
	var b strings.Builder
	b.WriteString("Global store verification\n")
	fmt.Fprintf(&b, "  Path:    %s\n", tui.Sanitize(res.Path))
	fmt.Fprintf(&b, "  Checked: %d\n", res.Checked)
	fmt.Fprintf(&b, "  Healthy: %d", res.Healthy)
	for _, f := range res.Findings {
		fmt.Fprintf(&b, "\n\n%s", strings.ToUpper(string(f.Kind)))
		if f.Key != "" {
			fmt.Fprintf(&b, "  %s", tui.Sanitize(f.Key))
		}
		fmt.Fprintf(&b, "\n  %s", tui.Sanitize(f.Detail))
		if f.Expected != "" {
			fmt.Fprintf(&b, "\n  expected: %s\n  actual:   %s", tui.Sanitize(f.Expected), tui.Sanitize(f.Actual))
		}
		for _, p := range f.UsedBy {
			fmt.Fprintf(&b, "\n  used by: %s", tui.Sanitize(p))
		}
		if f.Kind == globalstore.FindingCorrupted && f.Key != "" {
			fmt.Fprintf(&b, "\n  run: gskill store repair %s", tui.Sanitize(f.Key))
		}
	}
	if len(res.Findings) == 0 {
		b.WriteString("\n\nall objects healthy")
	}
	return b.String()
}

type storeRepairCmd struct {
	Hash string `arg:"" help:"Content key of the object to repair (sha256:<hex>)."`
}

// Help returns the detailed help shown by `gskill store repair --help`.
func (storeRepairCmd) Help() string {
	return examplesHelp("gskill store repair sha256:abc123…")
}

// Run re-fetches the object's recorded exact origin and atomically replaces
// it (FR-023). It never substitutes different content.
func (c storeRepairCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	if err := a.StoreRepair(ctx, c.Hash); err != nil {
		return err
	}
	return out.Result(
		fmt.Sprintf("repaired %s from its recorded origin", tui.Sanitize(c.Hash)),
		map[string]any{"repaired": c.Hash},
	)
}
