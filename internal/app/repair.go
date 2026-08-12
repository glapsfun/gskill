package app

import (
	"context"
)

// RepairResult reports a repair run.
type RepairResult struct {
	Repaired []string
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
		lf, err := loadOrNewLock(p.lockPath)
		if err != nil {
			return err
		}
		names := sortedKeys(lf.Skills)
		for k, name := range names {
			locked := lf.Skills[name]
			h, hErr := a.evaluateSkill(p, name, locked, true)
			if hErr != nil {
				return hErr
			}
			if h.Healthy() {
				continue
			}
			// Re-materialize the broken rungs (committed copy → agent targets)
			// from the locked revision, never re-resolving. A hash mismatch fails closed
			// on the content-hash check (exit 6).
			ireq, reqErr := a.frozenRequest(p, name, locked, InstallRequest{Root: root})
			if reqErr != nil {
				return reqErr
			}
			// Repair's contract is restoring lock-true content, including a
			// drifted repo-owned active entry (spec 022 §7 repair path).
			ireq.ReplaceActive = true
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
