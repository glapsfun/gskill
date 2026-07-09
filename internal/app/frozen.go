package app

import (
	"context"
	"fmt"
	"os"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
)

// installFrozen restores skills exactly from the lockfile without re-resolving
// or modifying it. If the manifest and lockfile disagree it aborts before
// touching any agent directory (exit 4); content that does not match the locked
// hash fails closed (FR-037, SC-001, SC-002).
func (a *App) installFrozen(ctx context.Context, req InstallRequest) (InstallResult, error) {
	p := openProject(req.Root)
	if !p.manifestExists() {
		return InstallResult{}, errNoManifest()
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return InstallResult{}, err
	}

	// Consistency is checked before any mutation, so a mismatch modifies nothing.
	if err := lockfile.CheckConsistency(m, lf); err != nil {
		return InstallResult{}, err
	}

	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		names := sortedKeys(lf.Skills)
		for k, name := range names {
			ireq, reqErr := a.frozenRequest(p, name, lf.Skills[name], req)
			if reqErr != nil {
				return reqErr
			}
			sctx := stampSkill(ctx, name, k+1, len(names))
			if _, instErr := a.installerForScope(p, string(ireq.Scope)).Install(sctx, ireq); instErr != nil {
				return instErr
			}
			out.Skills = append(out.Skills, SkillChange{
				Name:        name,
				ContentHash: lf.Skills[name].Resolved.ContentHash,
				Changed:     false,
			})
		}
		return nil
	})
	if err != nil {
		return InstallResult{}, err
	}
	return out, nil
}

// frozenRequest reconstructs an installer request from a locked skill entry.
func (a *App) frozenRequest(p *project, name string, locked lockfile.LockedSkill, req InstallRequest) (installer.Request, error) {
	agents, err := a.agentsByID(locked.Installation.Agents)
	if err != nil {
		return installer.Request{}, err
	}

	ref := refFromLock(locked.Source)
	rev := revFromLock(locked.Resolved)

	home, _ := os.UserHomeDir()
	return installer.Request{
		Ref:               ref,
		Revision:          rev,
		Name:              name,
		Path:              ref.Path,
		Agents:            agents,
		Scope:             installer.Scope(locked.Installation.Scope),
		ModePref:          locked.Installation.Mode,
		ProjectRoot:       p.root,
		Home:              home,
		Offline:           req.Offline,
		ExpectContentHash: locked.Resolved.ContentHash,
	}, nil
}

// agentsByID resolves agent IDs to adapters, failing if one is unavailable.
func (a *App) agentsByID(ids []string) ([]agent.Agent, error) {
	out := make([]agent.Agent, 0, len(ids))
	for _, id := range ids {
		ag, ok := a.agents.Get(id)
		if !ok {
			return nil, errs.WithHint(
				fmt.Errorf("%w: locked agent %q is not available", errs.ErrUnsupportedAgent, id),
				"run 'gskill doctor' to list detected agents")
		}
		out = append(out, ag)
	}
	return out, nil
}
