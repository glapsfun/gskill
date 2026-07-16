package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/installer"
)

// RepairResult reports a repair run.
type RepairResult struct {
	Repaired       []string
	StagingCleaned int
}

// Repair re-materializes broken or modified installs from the store/cache
// without changing the lockfile, and cleans up orphaned staging left by an
// interrupted install (FR-024, SC-007).
func (a *App) Repair(ctx context.Context, root string) (RepairResult, error) {
	p, err := a.openProjectScoped(root)
	if err != nil {
		return RepairResult{}, err
	}

	var out RepairResult
	err = a.withLock(ctx, p, func() error {
		// Staging cleanup only touches roots this project's lock covers. For
		// scope=global, p.cache is the SHARED home cache where other projects
		// stage concurrent fetches (only the per-project lock is held here),
		// so it is left to the age-thresholded sweep in store GC (FR-032).
		stagingRoots := []string{p.store.Root()}
		if p.storeScope != config.StoreScopeGlobal {
			stagingRoots = append(stagingRoots, p.cache.Root())
		}
		cleaned, cleanErr := installer.CleanupStaging(stagingRoots...)
		if cleanErr != nil {
			return cleanErr
		}
		out.StagingCleaned = cleaned

		lf, err := loadOrNewLock(p.lockPath)
		if err != nil {
			return err
		}
		storeRoot, err := filepath.Abs(p.contentRoot())
		if err != nil {
			return fmt.Errorf("resolve store root: %w", err)
		}
		names := sortedKeys(lf.Skills)
		for k, name := range names {
			locked := lf.Skills[name]
			h, hErr := a.evaluateSkill(p, name, locked, storeRoot, true)
			if hErr != nil {
				return hErr
			}
			if h.Healthy() {
				continue
			}
			// Re-materialize the broken rungs (store → active → agent targets) from
			// the locked revision, never re-resolving. A corrupt store fails closed
			// on the content-hash check (exit 6).
			ireq, reqErr := a.frozenRequest(p, name, locked, InstallRequest{Root: root})
			if reqErr != nil {
				return reqErr
			}
			sctx := stampSkill(ctx, name, k+1, len(names))
			if _, instErr := a.installerForScope(p, string(ireq.Scope)).Install(sctx, ireq); instErr != nil {
				return instErr
			}
			out.Repaired = append(out.Repaired, name)
		}
		a.recordProjectState(ctx, p, lf)
		return nil
	})
	if err != nil {
		return RepairResult{}, err
	}
	return out, nil
}
