package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
			pruned, pruneErr := a.pruneOrphans(p, lf)
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

// pruneOrphans removes gskill-managed installs that the lockfile no longer
// references. Only entries that are symlinks into this project's store are
// pruned, so disk content gskill never placed — hand-installed skills or those
// managed by another tool in the same shared agent directory — is left intact
// (FR-023). Copy-mode installs carry no such marker and are not auto-pruned;
// `gskill remove` deletes those by their recorded targets.
func (a *App) pruneOrphans(p *project, lf *lockfile.Lockfile) ([]string, error) {
	locked := make(map[string]bool, len(lf.Skills))
	for name := range lf.Skills {
		locked[name] = true
	}

	storeRoot, err := filepath.Abs(p.store.Root())
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}

	var pruned []string
	for _, ag := range a.agents.All() {
		container := ag.ProjectSkillDir(p.root)
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
			target := filepath.Join(container, entry.Name())
			managed, mErr := managedBySymlink(target, storeRoot)
			if mErr != nil {
				return nil, fmt.Errorf("inspect %s/%s: %w", ag.ID(), entry.Name(), mErr)
			}
			if !managed {
				continue
			}
			if rmErr := os.Remove(target); rmErr != nil {
				return nil, fmt.Errorf("prune %s/%s: %w", ag.ID(), entry.Name(), rmErr)
			}
			pruned = append(pruned, ag.ID()+":"+entry.Name())
		}
	}
	return pruned, nil
}

// managedBySymlink reports whether path is a symlink that resolves into the
// gskill store, i.e. an install gskill itself created. Plain directories and
// symlinks pointing elsewhere are treated as foreign and never pruned.
func managedBySymlink(path, storeRoot string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)
	root := filepath.Clean(storeRoot)
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator)), nil
}
