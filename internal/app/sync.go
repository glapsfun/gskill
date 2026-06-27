package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/lockfile"
)

// SyncRequest describes a `sync` invocation.
type SyncRequest struct {
	Root    string
	Prune   bool
	Offline bool
}

// SyncResult reports a sync run.
type SyncResult struct {
	Reconciled []SkillChange
	Pruned     []string
}

// Sync makes disk exactly match the lockfile, reinstalling each locked skill.
// With Prune, agent skill directories not referenced by the lock are removed
// (FR-023).
func (a *App) Sync(ctx context.Context, req SyncRequest) (SyncResult, error) {
	p := openProject(req.Root)

	var out SyncResult
	err := a.withLock(ctx, p, func() error {
		lf, err := loadOrNewLock(p.lockPath)
		if err != nil {
			return err
		}
		for _, name := range sortedKeys(lf.Skills) {
			ireq, reqErr := a.frozenRequest(p, name, lf.Skills[name], InstallRequest{Root: req.Root, Offline: req.Offline})
			if reqErr != nil {
				return reqErr
			}
			if _, instErr := a.installerForScope(p, string(ireq.Scope)).Install(ctx, ireq); instErr != nil {
				return instErr
			}
			out.Reconciled = append(out.Reconciled, SkillChange{
				Name: name, ContentHash: lf.Skills[name].Resolved.ContentHash, Changed: true,
			})
		}
		if req.Prune {
			pruned, pruneErr := a.pruneOrphans(req.Root, lf)
			if pruneErr != nil {
				return pruneErr
			}
			out.Pruned = pruned
		}
		return nil
	})
	if err != nil {
		return SyncResult{}, err
	}
	return out, nil
}

// pruneOrphans removes agent skill directories not referenced by the lockfile.
func (a *App) pruneOrphans(root string, lf *lockfile.Lockfile) ([]string, error) {
	locked := make(map[string]bool, len(lf.Skills))
	for name := range lf.Skills {
		locked[name] = true
	}

	var pruned []string
	for _, ag := range a.agents.All() {
		container := ag.ProjectSkillDir(root)
		entries, err := os.ReadDir(container)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s skills: %w", ag.ID(), err)
		}
		for _, entry := range entries {
			if locked[entry.Name()] {
				continue
			}
			if rmErr := os.RemoveAll(filepath.Join(container, entry.Name())); rmErr != nil {
				return nil, fmt.Errorf("prune %s/%s: %w", ag.ID(), entry.Name(), rmErr)
			}
			pruned = append(pruned, ag.ID()+":"+entry.Name())
		}
	}
	return pruned, nil
}
