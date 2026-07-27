package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/resolver"
)

// UpdateRequest configures an update execution run (spec 018).
type UpdateRequest struct {
	Root string
	// Names selects the skills to update: command arguments or a confirmed
	// interactive selection. Empty means every actionable skill.
	Names   []string
	Offline bool
	NoCache bool
	// DryRun reports the would-be transitions and writes nothing (FR-014).
	DryRun bool
}

// UpdateOutcome is one skill's update execution outcome.
type UpdateOutcome string

// Update outcomes. UpdateOutcomeNoChange is an honest no-op (up to date,
// pinned, or no longer actionable), never a disguised failure.
const (
	UpdateOutcomeUpdated  UpdateOutcome = "updated"
	UpdateOutcomeNoChange UpdateOutcome = "no-change"
	UpdateOutcomeWould    UpdateOutcome = "would-update"
	UpdateOutcomeFailed   UpdateOutcome = "failed"
)

// UpdateSkillResult reports one skill's update: the revision it was on, the
// revision it is on now, and what happened (FR-015).
type UpdateSkillResult struct {
	Name        string
	From        string
	To          string
	Outcome     UpdateOutcome
	Status      UpdateStatus
	Reason      string
	ContentHash string
	Err         error
}

// UpdateResult aggregates an update run, name-sorted.
type UpdateResult struct {
	Skills   []UpdateSkillResult
	Updated  int
	Failed   int
	NoChange int
	Changed  bool
	DryRun   bool
}

// Update advances the selected skills to the newest revision their requested
// tracking policy allows and rewrites only the resolved lock state (FR-019).
// Each skill applies independently and atomically: one skill's failure never
// aborts or rolls back the others, and a partial run returns the
// partial-install sentinel after processing everything (FR-020). Eligibility
// is re-derived from the lockfile as it exists now, so a stale earlier
// selection is demoted to a no-op instead of blindly applied (FR-013).
func (a *App) Update(ctx context.Context, req UpdateRequest) (UpdateResult, error) {
	ctx = git.WithMemo(ctx)
	p, err := a.openProjectScoped(req.Root)
	if err != nil {
		return UpdateResult{}, err
	}
	if !fileExists(p.lockPath) {
		return UpdateResult{}, errNoLock()
	}
	out := UpdateResult{DryRun: req.DryRun}
	err = a.withLock(ctx, p, func() error {
		return a.runUpdate(ctx, p, req, &out)
	})
	// Count outcomes before inspecting err: a cancelled run has already
	// persisted its successful entries, and discarding the populated result
	// would hide applied changes from the user.
	countUpdateOutcomes(&out)
	return out, updateRunError(err, out)
}

// runUpdate executes the selected skills under the project lock.
func (a *App) runUpdate(ctx context.Context, p *project, req UpdateRequest, out *UpdateResult) error {
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return err
	}
	names, err := updateNames(lf, req.Names)
	if err != nil {
		return err
	}
	for k, name := range names {
		out.Skills = append(out.Skills,
			a.updateOne(stampSkill(ctx, name, k+1, len(names)), p, lf, name, req))
		// A cancelled context aborts the run (after persisting what already
		// succeeded) instead of marking the rest failed.
		if ctxErr := ctx.Err(); ctxErr != nil {
			if finishErr := a.finishUpdateRun(ctx, p, lf, req, out); finishErr != nil {
				return finishErr
			}
			return ctxErr
		}
	}
	return a.finishUpdateRun(ctx, p, lf, req, out)
}

// updateNames validates and selects the skills to process: the requested
// names (all must be declared), or every declared skill when none are named.
func updateNames(lf *skillslock.State, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return sortedKeys(lf.Skills), nil
	}
	names := sortedUnique(requested)
	for _, name := range names {
		if _, ok := lf.Skills[name]; !ok {
			return nil, errs.WithHint(
				fmt.Errorf("%w: skill %q is not declared", errs.ErrInvalidLock, name),
				"run 'gskill list' to see installed skills",
			)
		}
	}
	return names, nil
}

// countUpdateOutcomes fills the result's outcome counters.
func countUpdateOutcomes(out *UpdateResult) {
	for _, s := range out.Skills {
		switch s.Outcome {
		case UpdateOutcomeUpdated, UpdateOutcomeWould:
			out.Updated++
		case UpdateOutcomeFailed:
			out.Failed++
		case UpdateOutcomeNoChange:
			out.NoChange++
		}
	}
	out.Changed = !out.DryRun && out.Updated > 0
}

// updateRunError maps a finished run onto its error contract: interrupts keep
// their partial results with the cancellation code, and a dry run's failures
// never exit with the partial-installation code (nothing was attempted or
// written) — the per-skill failed rows still tell the story.
func updateRunError(err error, out UpdateResult) error {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errs.WithHint(errs.ErrCancelled,
				"updates applied before the interrupt were kept and recorded")
		}
		return err
	}
	if out.Failed > 0 && !out.DryRun {
		return fmt.Errorf("%w: %d of %d skills failed to update",
			errs.ErrPartialInstall, out.Failed, len(out.Skills))
	}
	return nil
}

// updateOne classifies and (unless dry-run) applies one skill's update.
// Every processed skill produces a row — including honest no-ops — so an
// update-all run keeps the historical one-row-per-declared-skill result
// shape (FR-015/FR-016).
func (a *App) updateOne(ctx context.Context, p *project, lf *skillslock.State, name string, req UpdateRequest) UpdateSkillResult {
	rec := lf.Skills[name]
	item := a.planOne(ctx, name, rec, UpdatePlanRequest{
		Root: req.Root, Offline: req.Offline, NoCache: req.NoCache,
	})
	res := UpdateSkillResult{
		Name: name, From: item.Current, To: item.Current,
		Status:      item.Status,
		ContentHash: rec.Resolved.ContentHash,
	}
	switch {
	case item.Status == StatusUnknown && item.DiscoveryErr != "":
		// A real discovery failure on a skill that might have been actionable
		// is a failure in every mode: the run must not report success while a
		// remote was unreachable. (A pin's failed informational lookup is not
		// a failure — the pin's eligibility never depended on it.)
		res.Outcome = UpdateOutcomeFailed
		res.Reason = item.Reason
		res.Err = errors.New(item.DiscoveryErr)
		return res
	case !item.Actionable():
		res.Outcome = UpdateOutcomeNoChange
		res.Reason = item.Reason
		return res
	case req.DryRun:
		res.Outcome = UpdateOutcomeWould
		res.To = item.Candidate
		return res
	}

	change, err := a.installOne(ctx, p, lf, name, intentFromRecord(rec), InstallRequest{
		Root: req.Root, Offline: req.Offline, NoCache: req.NoCache,
	})
	if err != nil {
		res.Outcome = UpdateOutcomeFailed
		res.Reason = err.Error()
		res.Err = err
		return res
	}
	res.Outcome = UpdateOutcomeUpdated
	res.To = updatedRevLabel(lf.Skills[name].Resolved)
	res.ContentHash = change.ContentHash
	return res
}

// updatedRevLabel names the revision an update landed on. Branch-tracked
// skills label with the installed commit (matching the plan's short-SHA
// Current/Candidate labels) — RevisionLabel would return the branch name,
// which never changes and hides the actual transition (FR-015).
func updatedRevLabel(r skillslock.Resolved) string {
	if r.RefKind == string(resolver.RefKindBranch) {
		return shortCommit(r.Commit)
	}
	return RevisionLabel(revFromLock(r))
}

// finishUpdateRun persists the run's successful entries. Dry runs and runs
// that changed nothing write nothing — never create or rewrite the lock as a
// side effect.
func (a *App) finishUpdateRun(ctx context.Context, p *project, lf *skillslock.State, req UpdateRequest, out *UpdateResult) error {
	if req.DryRun {
		return nil
	}
	updated := false
	for _, s := range out.Skills {
		if s.Outcome == UpdateOutcomeUpdated {
			updated = true
		}
	}
	if !updated {
		return nil
	}
	if err := saveLock(p.lockPath, lf); err != nil {
		return err
	}
	a.recordProjectState(ctx, p, lf)
	return nil
}

// sortedUnique returns names sorted with duplicates removed.
func sortedUnique(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
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
