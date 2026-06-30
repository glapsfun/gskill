package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/lockfile"
	"github.com/glapsfun/gskill/internal/manifest"
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
// version within their constraints and rewrites the lockfile (FR-009).
func (a *App) Update(ctx context.Context, root string, names []string) (InstallResult, error) {
	p := openProject(root)
	if !p.manifestExists() {
		return InstallResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}

	targets := names
	if len(targets) == 0 {
		targets = sortedKeys(m.Skills)
	}

	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		for _, name := range targets {
			ms, ok := m.Skills[name]
			if !ok {
				return fmt.Errorf("%w: skill %q is not declared", errs.ErrInvalidManifest, name)
			}
			change, applyErr := a.installOne(ctx, p, lf, name, ms, InstallRequest{Root: root}, m.Defaults.Agents)
			if applyErr != nil {
				return applyErr
			}
			out.Skills = append(out.Skills, change)
			out.Changed = out.Changed || change.Changed
		}
		return lockfile.Save(p.lockPath, lf)
	})
	if err != nil {
		return InstallResult{}, err
	}
	return out, nil
}

// Lock recomputes the lockfile from the manifest, honoring existing pins without
// bumping skills whose declaration is unchanged.
func (a *App) Lock(ctx context.Context, root string) (InstallResult, error) {
	p := openProject(root)
	if !p.manifestExists() {
		return InstallResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}

	var out InstallResult
	err = a.withLock(ctx, p, func() error {
		old, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		next := lockfile.New()
		for _, name := range sortedKeys(m.Skills) {
			ms := m.Skills[name]
			if entry, ok := old.Skills[name]; ok && declarationUnchanged(ms, entry) {
				next.Skills[name] = entry // honor existing pin, no bump
				out.Skills = append(out.Skills, SkillChange{Name: name, ContentHash: entry.Resolved.ContentHash})
				continue
			}
			change, applyErr := a.installOne(ctx, p, next, name, ms, InstallRequest{Root: root}, m.Defaults.Agents)
			if applyErr != nil {
				return applyErr
			}
			out.Skills = append(out.Skills, change)
			out.Changed = true
		}
		return lockfile.Save(p.lockPath, next)
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

// Remove uninstalls the named skills from the manifest, lockfile, and every
// agent directory, then garbage-collects unreferenced store entries.
func (a *App) Remove(ctx context.Context, root string, names []string) (RemoveResult, error) {
	p := openProject(root)
	if !p.manifestExists() {
		return RemoveResult{}, fmt.Errorf("%w: no %s; run 'gskill init' first", errs.ErrInvalidManifest, ManifestName)
	}
	m, err := manifest.Load(p.manifestPath)
	if err != nil {
		return RemoveResult{}, err
	}

	var out RemoveResult
	err = a.withLock(ctx, p, func() error {
		lf, lockErr := loadOrNewLock(p.lockPath)
		if lockErr != nil {
			return lockErr
		}
		if rmErr := a.removeSkills(p, m, lf, names, &out); rmErr != nil {
			return rmErr
		}

		if saveErr := manifest.Save(p.manifestPath, m); saveErr != nil {
			return saveErr
		}
		if saveErr := lockfile.Save(p.lockPath, lf); saveErr != nil {
			return saveErr
		}

		gced, gcErr := p.store.GC(referencedHashes(lf))
		if gcErr != nil {
			return gcErr
		}
		out.StoreGCed = gced
		return nil
	})
	if err != nil {
		return RemoveResult{}, err
	}
	return out, nil
}

// removeSkills deletes the named skills from the manifest, lockfile, agent
// directories (confined), and the active layer, recording the outcome in out.
func (a *App) removeSkills(p *project, m *manifest.Manifest, lf *lockfile.Lockfile, names []string, out *RemoveResult) error {
	for _, name := range names {
		locked, inLock := lf.Skills[name]
		_, inManifest := m.Skills[name]
		if !inLock && !inManifest {
			out.NotPresent = append(out.NotPresent, name)
			continue
		}
		if inLock {
			scope := locked.Installation.Scope
			for _, id := range sortedKeys(locked.Installation.Targets) {
				if _, err := a.removeSafeTarget(p, scope, id, name, locked.Installation.Targets[id]); err != nil {
					return fmt.Errorf("remove target for %q: %w", name, err)
				}
			}
			if err := active.Remove(p.root, name); err != nil {
				return fmt.Errorf("remove active for %q: %w", name, err)
			}
		}
		delete(m.Skills, name)
		delete(lf.Skills, name)
		out.Removed = append(out.Removed, name)
	}
	return nil
}

// declarationUnchanged reports whether a manifest entry still matches its lock.
func declarationUnchanged(ms manifest.Skill, entry lockfile.LockedSkill) bool {
	return entry.Source.Original == ms.Source &&
		entry.Requested.Version == ms.Version &&
		entry.Requested.Ref == ms.Ref &&
		entry.Requested.Commit == ms.Commit
}

// referencedHashes collects the content hashes still referenced by the lockfile.
func referencedHashes(lf *lockfile.Lockfile) map[string]bool {
	refs := make(map[string]bool, len(lf.Skills))
	for _, s := range lf.Skills {
		if s.Resolved.ContentHash != "" {
			refs[s.Resolved.ContentHash] = true
		}
	}
	return refs
}
