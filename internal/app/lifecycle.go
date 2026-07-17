package app

import (
	"context"
	"fmt"
	"os"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/git"

	"github.com/glapsfun/gskill/internal/resolver"
)

// OutdatedSkill reports one skill's update availability.
type OutdatedSkill struct {
	Name      string
	Current   string
	Latest    string
	Available bool
}

// OutdatedReport aggregates an outdated run.
type OutdatedReport struct {
	Skills       []OutdatedSkill
	AnyAvailable bool
}

// Outdated reports available updates per locked skill (FR-009).
func (a *App) Outdated(ctx context.Context, root string) (OutdatedReport, error) {
	ctx = git.WithMemo(ctx)
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return OutdatedReport{}, err
	}

	var report OutdatedReport
	for _, name := range sortedKeys(lf.Skills) {
		locked := lf.Skills[name]
		res, err := resolver.Outdated(ctx, a.git, refFromLock(locked.Source),
			resolver.Requested{
				Version: locked.Requested.Version,
				Ref:     locked.Requested.Ref,
				Commit:  locked.Requested.Commit,
			},
			revFromLock(locked.Resolved))
		if err != nil {
			return OutdatedReport{}, err
		}
		report.Skills = append(report.Skills, OutdatedSkill{
			Name: name, Current: res.Current, Latest: res.Latest, Available: res.Available,
		})
		if res.Available {
			report.AnyAvailable = true
		}
	}
	return report, nil
}

// Update re-resolves the named skills (or all when names is empty) to the newest
// version within their constraints and rewrites the lock (FR-009).
func (a *App) Update(ctx context.Context, root string, names []string) (InstallResult, error) {
	ctx = git.WithMemo(ctx)
	p, err := a.openProjectScoped(root)
	if err != nil {
		return InstallResult{}, err
	}
	if !fileExists(p.lockPath) {
		return InstallResult{}, errNoLock()
	}
	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		targets := names
		if len(targets) == 0 {
			targets = sortedKeys(lf.Skills)
		}
		if len(targets) == 0 {
			return nil // nothing declared: never create a lock as a side effect
		}
		for k, name := range targets {
			r, ok := lf.Skills[name]
			if !ok {
				return errs.WithHint(
					fmt.Errorf("%w: skill %q is not declared", errs.ErrInvalidLock, name),
					"run 'gskill list' to see installed skills",
				)
			}
			sctx := stampSkill(ctx, name, k+1, len(targets))
			change, applyErr := a.installOne(sctx, p, lf, name, intentFromRecord(r), InstallRequest{Root: root})
			if applyErr != nil {
				return applyErr
			}
			out.Skills = append(out.Skills, change)
			out.Changed = out.Changed || change.Changed
		}
		if saveErr := saveLock(p.lockPath, lf); saveErr != nil {
			return saveErr
		}
		a.recordProjectState(ctx, p, lf)
		return nil
	})
	if err != nil {
		return InstallResult{}, err
	}
	return out, nil
}

// RemoveResult reports a remove run.
type RemoveResult struct {
	Removed    []string
	StoreGCed  int
	NotPresent []string
}

// Remove uninstalls the named skills from the lock and every agent directory,
// then garbage-collects unreferenced store entries.
func (a *App) Remove(ctx context.Context, root string, names []string) (RemoveResult, error) {
	p, err := a.openProjectScoped(root)
	if err != nil {
		return RemoveResult{}, err
	}
	if !fileExists(p.lockPath) {
		return RemoveResult{}, errNoLock()
	}
	var out RemoveResult
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		if rmErr := a.removeSkills(p, lf, names, &out); rmErr != nil {
			return rmErr
		}
		if len(out.Removed) == 0 {
			return nil // nothing removed: never create or rewrite the lock
		}

		if saveErr := saveLock(p.lockPath, lf); saveErr != nil {
			return saveErr
		}

		// GC is strictly the PROJECT-LOCAL store's: removing a skill from one
		// project must never delete shared global content — global objects are
		// deletable only through `gskill store gc` (spec 015 FR-009/FR-024).
		// For scope=global p.store is empty, so this is a no-op by design.
		gced, gcErr := p.store.GC(referencedHashes(lf))
		if gcErr != nil {
			return gcErr
		}
		out.StoreGCed = gced

		// Drop the removed skills' machine-local bookkeeping (FR-014) and let
		// the registry stop marking the removed objects (FR-027).
		a.recordProjectState(ctx, p, lf)
		return nil
	})
	if err != nil {
		return RemoveResult{}, err
	}
	return out, nil
}

// removeSkills deletes the named skills from the lock, agent directories
// (confined), and the active layer, recording the outcome in out.
func (a *App) removeSkills(p *project, lf *skillslock.State, names []string, out *RemoveResult) error {
	for _, name := range names {
		locked, inLock := lf.Skills[name]
		if !inLock {
			out.NotPresent = append(out.NotPresent, name)
			continue
		}
		scope := locked.Installation.Scope
		for _, id := range sortedKeys(locked.Installation.Targets) {
			// Ownership-checked (not the bare confined-path deletion): a
			// copy-mode target whose content no longer matches what gskill
			// installed fails closed here too, matching the guarantee
			// unlink and install --agent narrowing already enforce.
			target, safe, chkErr := a.checkSafeTargetRemoval(p, scope, id, name, locked.Installation.Targets[id], locked.Resolved.ContentHash)
			if chkErr != nil {
				return fmt.Errorf("remove target for %q: %w", name, chkErr)
			}
			if safe {
				if rmErr := os.RemoveAll(target); rmErr != nil {
					return fmt.Errorf("remove target for %q: %w", name, rmErr)
				}
			}
		}
		if err := active.Remove(p.root, name); err != nil {
			return fmt.Errorf("remove active for %q: %w", name, err)
		}
		delete(lf.Skills, name)
		out.Removed = append(out.Removed, name)
	}
	return nil
}

// referencedHashes collects the content hashes still referenced by the lockfile.
func referencedHashes(lf *skillslock.State) map[string]bool {
	refs := make(map[string]bool, len(lf.Skills))
	for _, s := range lf.Skills {
		if s.Resolved.ContentHash != "" {
			refs[s.Resolved.ContentHash] = true
		}
	}
	return refs
}
