package app

import (
	"context"

	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
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
	p := openProject(root)

	var out RepairResult
	err := a.withLock(ctx, p, func() error {
		cleaned, cleanErr := installer.CleanupStaging(p.store.Root(), p.cache.Root())
		if cleanErr != nil {
			return cleanErr
		}
		out.StagingCleaned = cleaned

		lf, err := loadOrNewLock(p.lockPath)
		if err != nil {
			return err
		}
		for _, name := range sortedKeys(lf.Skills) {
			locked := lf.Skills[name]
			if skillHealthy(root, name, locked) {
				continue
			}
			ireq, reqErr := a.frozenRequest(p, name, locked, InstallRequest{Root: root})
			if reqErr != nil {
				return reqErr
			}
			if _, instErr := a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq); instErr != nil {
				return instErr
			}
			out.Repaired = append(out.Repaired, name)
		}
		return nil
	})
	if err != nil {
		return RepairResult{}, err
	}
	return out, nil
}

// skillHealthy reports whether every target of a locked skill verifies.
func skillHealthy(root, name string, locked lockfile.LockedSkill) bool {
	return verifySkill(root, name, locked).OK
}
