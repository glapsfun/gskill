package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/tui"
)

// storeCmd groups global-store management subcommands (spec 015 FR-035).
type storeCmd struct {
	Status  storeStatusCmd  `cmd:"" help:"Summarize the global store."`
	List    storeListCmd    `cmd:"" help:"List stored objects and their users."`
	Inspect storeInspectCmd `cmd:"" help:"Show one object's integrity, origins, and users."`
	Verify  storeVerifyCmd  `cmd:"" help:"Verify every global store object's integrity."`
	Repair  storeRepairCmd  `cmd:"" help:"Restore a corrupted object from its recorded origin."`
	Gc      storeGCCmd      `cmd:"" name:"gc" help:"Report (default) or delete unused store objects."`
	Pin     storePinCmd     `cmd:"" help:"Exempt an object from garbage collection."`
	Unpin   storeUnpinCmd   `cmd:"" help:"Remove an object's GC exemption."`
	Pins    storePinsCmd    `cmd:"" help:"List pinned objects."`
}

// Help returns the detailed help shown by `gskill store --help`.
func (storeCmd) Help() string {
	return examplesHelp("gskill store status", "gskill store gc --apply", "gskill store repair sha256:abc123…")
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

type storeStatusCmd struct{}

// Help returns the detailed help shown by `gskill store status --help`.
func (storeStatusCmd) Help() string {
	return examplesHelp("gskill store status", "gskill store status --json")
}

// Run summarizes the store (contracts §2).
func (storeStatusCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	rep, err := a.StoreStatus(ctx)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("Global gskill store\n")
	fmt.Fprintf(&b, "  Path:      %s\n", tui.Sanitize(rep.Path))
	fmt.Fprintf(&b, "  Objects:   %d\n", rep.Objects)
	fmt.Fprintf(&b, "  Size:      %d bytes\n", rep.SizeBytes)
	fmt.Fprintf(&b, "  Projects:  %d\n", rep.Projects)
	fmt.Fprintf(&b, "  Unused:    %d\n", rep.Unused)
	fmt.Fprintf(&b, "  Corrupted: %d", rep.Corrupted)
	return out.Result(b.String(), map[string]any{
		"path": rep.Path, "objects": rep.Objects, "sizeBytes": rep.SizeBytes,
		"projects": rep.Projects, "unused": rep.Unused, "corrupted": rep.Corrupted,
	})
}

type storeListCmd struct{}

// Help returns the detailed help shown by `gskill store list --help`.
func (storeListCmd) Help() string {
	return examplesHelp("gskill store list", "gskill store list --json")
}

// Run lists stored objects. Origin-derived strings are sanitized (FR-034).
func (storeListCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	items, err := a.StoreList(ctx)
	if err != nil {
		return err
	}
	var b strings.Builder
	rows := make([]map[string]any, 0, len(items))
	fmt.Fprintf(&b, "%-22s %-24s %-10s %-10s %s\n", "Hash", "Skill", "Version", "Size", "Projects")
	for _, it := range items {
		fmt.Fprintf(&b, "%-22s %-24s %-10s %-10d %d\n",
			tui.Sanitize(shortHash(it.Key)), tui.OrDash(tui.Sanitize(it.Skill)),
			tui.OrDash(tui.Sanitize(it.Version)), it.SizeBytes, it.Projects)
		rows = append(rows, map[string]any{
			"hash": it.Key, "skill": it.Skill, "version": it.Version,
			"sizeBytes": it.SizeBytes, "projects": it.Projects,
		})
	}
	if len(items) == 0 {
		b.Reset()
		b.WriteString("the global store is empty")
	}
	return out.Result(strings.TrimRight(b.String(), "\n"), map[string]any{"objects": rows})
}

// shortHash abbreviates a content key for table display.
func shortHash(key string) string {
	const maxLen = 19
	if len(key) <= maxLen {
		return key
	}
	return key[:maxLen] + "…"
}

type storeInspectCmd struct {
	Hash string `arg:"" help:"Content key of the object (sha256:<hex>)."`
}

// Help returns the detailed help shown by `gskill store inspect --help`.
func (storeInspectCmd) Help() string {
	return examplesHelp("gskill store inspect sha256:abc123…")
}

// Run verifies and describes one object.
func (c storeInspectCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	rep, err := a.StoreInspect(ctx, c.Hash)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Store object %s\n", tui.Sanitize(rep.Key))
	fmt.Fprintf(&b, "  Integrity: %s\n", tui.Sanitize(rep.Integrity))
	fmt.Fprintf(&b, "  Size:      %d bytes\n", rep.SizeBytes)
	fmt.Fprintf(&b, "  Pinned:    %t", rep.Pinned)
	for _, o := range rep.Origins {
		fmt.Fprintf(&b, "\n  origin: %s %s %s (commit %s)",
			tui.OrDash(tui.Sanitize(o.Source)), tui.OrDash(tui.Sanitize(o.SkillPath)),
			tui.OrDash(tui.Sanitize(o.Version)), tui.OrDash(tui.Sanitize(o.Commit)))
	}
	for _, p := range rep.UsedBy {
		fmt.Fprintf(&b, "\n  used by: %s", tui.Sanitize(p))
	}
	origins := make([]map[string]any, 0, len(rep.Origins))
	for _, o := range rep.Origins {
		origins = append(origins, map[string]any{
			"sourceType": o.SourceType, "source": o.Source, "skillPath": o.SkillPath,
			"version": o.Version, "ref": o.Ref, "commit": o.Commit,
		})
	}
	return out.Result(b.String(), map[string]any{
		"hash": rep.Key, "integrity": rep.Integrity, "sizeBytes": rep.SizeBytes,
		"pinned": rep.Pinned, "origins": origins, "usedBy": nonNilStrings(rep.UsedBy),
	})
}

type storeGCCmd struct {
	Apply     bool   `help:"Delete the unused objects (default is a dry-run report)."`
	OlderThan string `name:"older-than" help:"Override the grace period for this run (e.g. 90d)."`
}

// Help returns the detailed help shown by `gskill store gc --help`.
func (storeGCCmd) Help() string {
	return examplesHelp("gskill store gc", "gskill store gc --apply", "gskill store gc --apply --older-than 90d")
}

// Run garbage-collects: dry-run by default (FR-025).
func (c storeGCCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	var olderThan time.Duration
	if c.OlderThan != "" {
		d, err := config.ParseFlexDuration(c.OlderThan)
		if err != nil {
			return errs.Wrap(errs.CodeUsage, "--older-than", err)
		}
		if d == 0 {
			d = time.Nanosecond // explicit "0d": no grace period at all
		}
		olderThan = d
	}
	rep, err := a.StoreGC(ctx, c.Apply, olderThan)
	if err != nil {
		return err
	}

	var b strings.Builder
	rows := make([]map[string]any, 0, len(rep.Candidates))
	if len(rep.Candidates) == 0 {
		b.WriteString("no unused global store objects")
	} else {
		b.WriteString("Unused global store objects\n")
		fmt.Fprintf(&b, "%-22s %-24s %-10s %s\n", "Hash", "Skill", "Version", "Size")
		for _, cand := range rep.Candidates {
			fmt.Fprintf(&b, "%-22s %-24s %-10s %d\n",
				tui.Sanitize(shortHash(cand.Key)), tui.OrDash(tui.Sanitize(cand.Skill)),
				tui.OrDash(tui.Sanitize(cand.Version)), cand.SizeBytes)
			rows = append(rows, map[string]any{
				"hash": cand.Key, "skill": cand.Skill, "version": cand.Version, "sizeBytes": cand.SizeBytes,
			})
		}
		fmt.Fprintf(&b, "Reclaimable: %d bytes", rep.ReclaimableBytes)
	}
	if rep.Degraded {
		b.WriteString("\nwarning: no project registry available — reference marking is degraded")
	}
	switch {
	case !rep.Applied && len(rep.Candidates) > 0:
		b.WriteString("\n\nrun: gskill store gc --apply")
	case rep.Applied:
		fmt.Fprintf(&b, "\n\ndeleted %d objects", len(rep.Deleted))
		for _, key := range rep.Skipped {
			fmt.Fprintf(&b, "\nskipped (in use): %s", tui.Sanitize(key))
		}
	}
	return out.Result(strings.TrimRight(b.String(), "\n"), map[string]any{
		"applied": rep.Applied, "candidates": rows,
		"reclaimableBytes": rep.ReclaimableBytes,
		"deleted":          nonNilStrings(rep.Deleted),
		"skipped":          nonNilStrings(rep.Skipped),
		"degraded":         rep.Degraded,
	})
}

type storePinCmd struct {
	Hash string `arg:"" help:"Content key to pin."`
}

// Help returns the detailed help shown by `gskill store pin --help`.
func (storePinCmd) Help() string {
	return examplesHelp("gskill store pin sha256:abc123…")
}

// Run pins an object (FR-026).
func (c storePinCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	if err := a.StorePin(ctx, c.Hash); err != nil {
		return err
	}
	return out.Result("pinned "+tui.Sanitize(c.Hash), map[string]any{"pinned": c.Hash})
}

type storeUnpinCmd struct {
	Hash string `arg:"" help:"Content key to unpin."`
}

// Help returns the detailed help shown by `gskill store unpin --help`.
func (storeUnpinCmd) Help() string {
	return examplesHelp("gskill store unpin sha256:abc123…")
}

// Run unpins an object.
func (c storeUnpinCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	if err := a.StoreUnpin(ctx, c.Hash); err != nil {
		return err
	}
	return out.Result("unpinned "+tui.Sanitize(c.Hash), map[string]any{"unpinned": c.Hash})
}

type storePinsCmd struct{}

// Help returns the detailed help shown by `gskill store pins --help`.
func (storePinsCmd) Help() string {
	return examplesHelp("gskill store pins")
}

// Run lists pinned objects.
func (storePinsCmd) Run(ctx context.Context, out *Output, a *app.App) error {
	pins, err := a.StorePins(ctx)
	if err != nil {
		return err
	}
	human := "no pinned objects"
	if len(pins) > 0 {
		var sanitized []string
		for _, p := range pins {
			sanitized = append(sanitized, tui.Sanitize(p))
		}
		human = strings.Join(sanitized, "\n")
	}
	return out.Result(human, map[string]any{"pins": nonNilStrings(pins)})
}
