package app

import (
	"context"
	"fmt"
	"os"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
)

// UnlinkResult reports the outcome of an unlink.
type UnlinkResult struct {
	Skill           string
	UnlinkedAgent   string
	RemainingAgents []string
	Pruned          bool
	Unreferenced    bool
}

// Unlink removes a single agent's access to a skill without affecting other
// agents (FR-020, SC-008). When the last agent is unlinked, the active entry,
// store content, and lock entry are retained unless prune is set, in which
// case the skill is removed entirely and unreferenced store content is GC'd.
func (a *App) Unlink(ctx context.Context, root, skill, agentID string, prune bool) (UnlinkResult, error) {
	p := openProject(root)
	if _, ok := a.agents.Get(agentID); !ok {
		return UnlinkResult{}, errs.WithHint(
			fmt.Errorf("%w: unknown agent %q", errs.ErrUnsupportedAgent, agentID),
			"run 'gskill doctor' to list detected agents")
	}

	out := UnlinkResult{Skill: skill, UnlinkedAgent: agentID}
	err := a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		return a.unlinkOne(p, lf, skill, agentID, prune, &out)
	})
	if err != nil {
		return UnlinkResult{}, err
	}
	return out, nil
}

// unlinkOne performs the unlink against the loaded lock under the project lock.
func (a *App) unlinkOne(p *project, lf *skillslock.State, skill, agentID string, prune bool, out *UnlinkResult) error {
	locked, inLock := lf.Skills[skill]
	if !inLock {
		return errs.WithHint(
			fmt.Errorf("%w: skill %q is not declared", errs.ErrInvalidLock, skill),
			"run 'gskill list' to see installed skills")
	}

	current := locked.Installation.Agents
	if !contains(current, agentID) {
		return errs.WithHint(
			fmt.Errorf("%w: skill %q is not installed for agent %q", errs.ErrInvalidLock, skill, agentID),
			"run 'gskill status' to see each skill's agents")
	}

	// Remove the agent's recorded target (confined, and — for a real
	// copy-mode directory — ownership-checked so foreign-modified content
	// is never silently deleted, matching the guarantee spec 013's
	// install-narrowing path enforces via the same primitive), then drop it
	// from the lock.
	if recorded, ok := locked.Installation.Targets[agentID]; ok {
		target, safe, chkErr := a.checkSafeTargetRemoval(p, locked.Installation.Scope, agentID, skill, recorded, locked.Resolved.ContentHash)
		if chkErr != nil {
			return fmt.Errorf("remove %s target: %w", agentID, chkErr)
		}
		if safe {
			if rmErr := os.RemoveAll(target); rmErr != nil {
				return fmt.Errorf("remove %s target: %w", agentID, rmErr)
			}
		}
	}
	delete(locked.Installation.Targets, agentID)
	delete(locked.Installation.Modes, agentID)
	remaining := subtract(current, []string{agentID})
	locked.Installation.Agents = remaining
	out.RemainingAgents = remaining

	if len(remaining) > 0 {
		lf.Skills[skill] = locked
		return a.saveUnlink(p, lf, false)
	}

	out.Unreferenced = true
	if !prune {
		// Retain the active entry, store content, and lock entry.
		lf.Skills[skill] = locked
		return a.saveUnlink(p, lf, false)
	}

	// Prune: remove the active entry and lock entry, and GC the store.
	if rmErr := active.Remove(p.root, skill); rmErr != nil {
		return rmErr
	}
	delete(lf.Skills, skill)
	out.Pruned = true
	return a.saveUnlink(p, lf, true)
}

// saveUnlink persists the lock, optionally GC'ing the store.
func (a *App) saveUnlink(p *project, lf *skillslock.State, gc bool) error {
	if err := saveLock(p.lockPath, lf); err != nil {
		return err
	}
	if gc {
		if _, err := p.store.GC(referencedHashes(lf)); err != nil {
			return err
		}
	}
	return nil
}

// contains reports whether s is in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
