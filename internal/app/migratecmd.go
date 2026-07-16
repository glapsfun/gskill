package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/migrate"
)

// MigrateReport is the outcome of `gskill migrate global-store` (FR-037).
type MigrateReport struct {
	DryRun bool
	// NothingToDo reports an already-migrated project (no legacy store).
	NothingToDo bool
	Plan        migrate.Plan
	Result      migrate.Result
}

// MigrateGlobalStore converts a project from the legacy project-local store
// to the user-level global store (spec 015 US5). Dry-run reports the plan and
// changes nothing; a real run verifies, dedupes/copies, relinks, updates
// machine-local state, and removes the legacy store only after complete
// success — any earlier failure leaves the project fully usable (FR-038).
func (a *App) MigrateGlobalStore(ctx context.Context, root string, dryRun bool) (MigrateReport, error) {
	rep := MigrateReport{DryRun: dryRun}
	if !hasPopulatedProjectStore(root) {
		rep.NothingToDo = true
		return rep, nil
	}

	gs, err := a.openGlobalStore()
	if err != nil {
		return rep, err
	}

	// The project lock covers link switching, state, and legacy-store
	// removal (FR-030); the legacy-scope project handle carries the lock dir.
	p := openProject(root)
	err = a.withLock(ctx, p, func() error {
		if dryRun {
			plan, planErr := migrate.BuildPlan(root, gs)
			if planErr != nil {
				return planErr
			}
			rep.Plan = plan
			return nil
		}

		lf, lfErr := loadOrNewLock(p.lockPath)
		if lfErr != nil {
			return lfErr
		}
		skills := make([]migrate.LockedSkill, 0, len(lf.Skills))
		for _, name := range sortedKeys(lf.Skills) {
			rec := lf.Skills[name]
			skills = append(skills, migrate.LockedSkill{
				Name:        name,
				ContentHash: rec.Resolved.ContentHash,
				Origin: globalstore.Origin{
					SourceType: rec.Source.Type,
					Source:     rec.Source.URL,
					SkillPath:  rec.Source.Path,
					Version:    rec.Resolved.Version,
					Ref:        rec.Requested.Ref,
					Commit:     rec.Resolved.Commit,
				},
			})
		}

		res, runErr := migrate.Run(ctx, root, gs, skills)
		if runErr != nil {
			return runErr
		}
		rep.Plan = res.Plan
		rep.Result = res
		a.log.Info("migrate global-store",
			"localObjects", res.LocalObjects, "admitted", res.AdmittedObjects,
			"relinked", len(res.Relinked), "corrupt", len(res.Corrupt),
			"localStoreRemoved", res.LocalStoreRemoved)

		if res.LocalStoreRemoved {
			// The project now serves from the global store: record it in the
			// machine-local state and the advisory registry (FR-014, FR-027).
			gp := *p
			gp.storeScope = config.StoreScopeGlobal
			gp.global = gs
			if stErr := writeProjectState(&gp, lf); stErr != nil {
				a.log.Warn("write project state", "error", stErr)
			}
			a.registerProject(ctx, &gp, lf)
		}
		return nil
	})
	if err != nil {
		return rep, fmt.Errorf("migrate global-store: %w", err)
	}
	return rep, nil
}
