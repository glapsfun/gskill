package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
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
// store content, and manifest entry are retained unless prune is set, in which
// case the skill is removed entirely and unreferenced store content is GC'd.
func (a *App) Unlink(ctx context.Context, root, skill, agentID string, prune bool) (UnlinkResult, error) {
	p := openProject(root)
	if !p.manifestExists() {
		return UnlinkResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	if _, ok := a.agents.Get(agentID); !ok {
		return UnlinkResult{}, fmt.Errorf("%w: unknown agent %q", errs.ErrUnsupportedAgent, agentID)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return UnlinkResult{}, err
	}

	out := UnlinkResult{Skill: skill, UnlinkedAgent: agentID}
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		return a.unlinkOne(p, m, lf, skill, agentID, prune, &out)
	})
	if err != nil {
		return UnlinkResult{}, err
	}
	return out, nil
}

// unlinkOne performs the unlink against loaded manifest and lockfile under lock.
func (a *App) unlinkOne(p *project, m *manifest.Manifest, lf *lockfile.Lockfile, skill, agentID string, prune bool, out *UnlinkResult) error {
	locked, inLock := lf.Skills[skill]
	ms, inManifest := m.Skills[skill]
	switch {
	case !inLock && !inManifest:
		return fmt.Errorf("%w: skill %q is not declared", errs.ErrInvalidManifest, skill)
	case !inLock || !inManifest:
		// A half-present skill would otherwise have its missing side written back
		// from a zero value, corrupting the manifest or lockfile.
		return fmt.Errorf("%w: skill %q is out of sync between the manifest and lockfile; run 'gskill sync' (or 'gskill lock') first",
			errs.ErrLockMismatch, skill)
	}

	current := installedAgentIDs(lf, skill, ms)
	if !contains(current, agentID) {
		return fmt.Errorf("%w: skill %q is not installed for agent %q", errs.ErrInvalidManifest, skill, agentID)
	}

	// Remove the agent's recorded target (confined), then drop it from lock +
	// manifest.
	if target, ok := locked.Installation.Targets[agentID]; ok {
		if _, rmErr := a.removeSafeTarget(p, locked.Installation.Scope, agentID, skill, target); rmErr != nil {
			return fmt.Errorf("remove %s target: %w", agentID, rmErr)
		}
	}
	delete(locked.Installation.Targets, agentID)
	delete(locked.Installation.Modes, agentID)
	remaining := subtract(current, []string{agentID})
	locked.Installation.Agents = remaining
	ms.Agents = remaining
	out.RemainingAgents = remaining

	if len(remaining) > 0 {
		lf.Skills[skill] = locked
		m.Skills[skill] = ms
		return a.saveUnlink(p, m, lf, false)
	}

	out.Unreferenced = true
	if !prune {
		// Retain the active entry, store content, and manifest entry.
		lf.Skills[skill] = locked
		m.Skills[skill] = ms
		return a.saveUnlink(p, m, lf, false)
	}

	// Prune: remove the active entry, manifest + lock entries, and GC the store.
	if rmErr := active.Remove(p.root, skill); rmErr != nil {
		return rmErr
	}
	delete(lf.Skills, skill)
	delete(m.Skills, skill)
	out.Pruned = true
	return a.saveUnlink(p, m, lf, true)
}

// saveUnlink persists the manifest and lockfile, optionally GC'ing the store.
func (a *App) saveUnlink(p *project, m *manifest.Manifest, lf *lockfile.Lockfile, gc bool) error {
	if err := manifest.Save(p.manifestPath, m); err != nil {
		return err
	}
	if err := lockfile.Save(p.lockPath, lf); err != nil {
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
