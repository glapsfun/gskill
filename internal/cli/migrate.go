package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/tui"
)

// migrateCmd groups migration subcommands (spec 015 US5).
type migrateCmd struct {
	GlobalStore migrateGlobalStoreCmd `cmd:"" name:"global-store" help:"Move this project's skill content into the shared global store."`
}

// Help returns the detailed help shown by `gskill migrate --help`.
func (migrateCmd) Help() string {
	return examplesHelp("gskill migrate global-store --dry-run", "gskill migrate global-store")
}

type migrateGlobalStoreCmd struct{}

// Help returns the detailed help shown by `gskill migrate global-store --help`.
func (migrateGlobalStoreCmd) Help() string {
	return examplesHelp("gskill migrate global-store --dry-run", "gskill migrate global-store")
}

// Run migrates the project from its legacy project-local store to the global
// store (FR-037): verified dedupe/copy, atomic relink, delete-local-last,
// with rollback-by-construction on any failure (FR-038).
func (migrateGlobalStoreCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot, g Globals) error {
	rep, err := a.MigrateGlobalStore(ctx, string(root), g.DryRun)
	if err != nil {
		return err
	}

	doc := map[string]any{
		"dryRun":        rep.DryRun,
		"nothingToDo":   rep.NothingToDo,
		"localObjects":  rep.Plan.LocalObjects,
		"alreadyGlobal": rep.Plan.AlreadyGlobal,
		"toCopy":        rep.Plan.ToCopy,
		"savingsBytes":  rep.Plan.SavingsBytes,
	}
	if len(rep.Plan.Corrupt) > 0 {
		doc["corrupt"] = rep.Plan.Corrupt
	}
	if !rep.DryRun {
		doc["admittedObjects"] = rep.Result.AdmittedObjects
		doc["relinked"] = rep.Result.Relinked
		doc["localStoreRemoved"] = rep.Result.LocalStoreRemoved
		if len(rep.Result.BlockedLinks) > 0 {
			doc["blockedLinks"] = rep.Result.BlockedLinks
		}
	}
	return out.Result(humanMigrate(rep, string(root)), doc)
}

// humanMigrate renders the migration plan or result.
func humanMigrate(rep app.MigrateReport, root string) string {
	if rep.NothingToDo {
		return "nothing to migrate: this project already uses the global store"
	}
	var b strings.Builder
	if rep.DryRun {
		b.WriteString("Migration plan\n")
	} else {
		b.WriteString("Migration result\n")
	}
	fmt.Fprintf(&b, "  Project:          %s\n", tui.Sanitize(root))
	fmt.Fprintf(&b, "  Local objects:    %d\n", rep.Plan.LocalObjects)
	fmt.Fprintf(&b, "  Already global:   %d\n", rep.Plan.AlreadyGlobal)
	fmt.Fprintf(&b, "  Objects to copy:  %d\n", rep.Plan.ToCopy)
	fmt.Fprintf(&b, "  Disk savings:     %d bytes", rep.Plan.SavingsBytes)
	for _, key := range rep.Plan.Corrupt {
		fmt.Fprintf(&b, "\n  corrupt (skipped): %s", tui.Sanitize(key))
	}
	if rep.DryRun {
		b.WriteString("\n\nno files were changed (--dry-run)")
		return b.String()
	}
	fmt.Fprintf(&b, "\n  Relinked skills:  %d", len(rep.Result.Relinked))
	if rep.Result.LocalStoreRemoved {
		b.WriteString("\n\nlegacy project-local store removed; this project now shares the global store")
		return b.String()
	}
	for _, name := range rep.Result.BlockedLinks {
		fmt.Fprintf(&b, "\n  blocked: active link %q (managed by another tool) still resolves into the legacy store", tui.Sanitize(name))
	}
	switch {
	case len(rep.Result.BlockedLinks) > 0:
		b.WriteString("\n\nlegacy store preserved (the links above still depend on it); the project remains fully usable")
	case len(rep.Plan.Corrupt) > 0:
		b.WriteString("\n\nlegacy store preserved (corrupt objects were skipped); the project remains fully usable")
	default:
		b.WriteString("\n\nlegacy store preserved (not every skill could be migrated); the project remains fully usable")
	}
	return b.String()
}
