package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/errs"
)

// repairHint explains a failed restore whose real cause is a moved override
// declaration. Repair reproduces the *locked* content, but the locked hash is
// the output of the declaration as it stood at install time; once an override
// input changes, no restore can reproduce it and the bare "content does not
// match locked" mismatch sends the user nowhere. `install` is the command that
// re-applies a changed declaration, so say so.
func repairHint(name string, h SkillHealth, err error) error {
	if h.OverrideDrift == "" {
		return err
	}
	return errs.WithHint(
		fmt.Errorf("skill %q: %w", name, err),
		"the override declaration changed since install, so the locked content cannot be reproduced; run 'gskill install' to re-apply it")
}

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
			if h.WithoutOverrideDrift().Healthy() {
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
				return repairHint(name, h, instErr)
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
