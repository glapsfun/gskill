package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/projreg"
	"github.com/glapsfun/gskill/internal/projstate"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// StoreVerifyResult is the outcome of a store-wide verification (FR-022).
type StoreVerifyResult struct {
	Path     string
	Checked  int
	Healthy  int
	Findings []globalstore.ScanFinding
}

// Failed reports whether the scan found any problem.
func (r StoreVerifyResult) Failed() bool { return len(r.Findings) > 0 }

// openGlobalStore opens the user-level store regardless of any project's
// scope: store management commands always address the global store.
func (a *App) openGlobalStore() (*globalstore.Store, error) {
	h, err := a.openHome()
	if err != nil {
		return nil, fmt.Errorf("open gskill home: %w", err)
	}
	gs := globalstore.New(h)
	gs.SetLocker(globalstore.NewLocker(h, a.cfg.StoreLockTimeout, nil))
	return gs, nil
}

// StoreVerify scans every global store object: full content re-hash,
// metadata validation, layout, permissions, and stray staging (FR-022).
func (a *App) StoreVerify(_ context.Context) (StoreVerifyResult, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return StoreVerifyResult{}, err
	}
	rep, err := gs.VerifyStore(globalstore.ScanOptions{UsedBy: storeUsedBy(gs)})
	if err != nil {
		return StoreVerifyResult{}, err
	}
	return StoreVerifyResult{
		Path:     gs.Root(),
		Checked:  rep.Checked,
		Healthy:  rep.Healthy,
		Findings: rep.Findings,
	}, nil
}

// storeUsedBy resolves which known projects reference an object, from the
// advisory registry snapshots. It is never required for correctness (FR-027).
func storeUsedBy(gs *globalstore.Store) func(key string) []string {
	return func(key string) []string {
		entries, err := projreg.List(gs.Home())
		if err != nil {
			return nil
		}
		var roots []string
		for _, e := range entries {
			for _, ref := range e.References {
				if ref.StoreHash == key {
					label := e.Root
					if label == "" {
						label = e.ProjectID
					}
					roots = append(roots, label)
					break
				}
			}
		}
		return roots
	}
}

// storeMarkRefs builds the GC mark function (FR-024): registry snapshots are
// counted conservatively, and every registered project with a live root is
// re-read at call time — its lockfile's gskill blocks, its state file, and a
// scan of its active links into the store — so a stale registry snapshot can
// never justify deleting content a live link still uses (US6 scenario 2).
func storeMarkRefs(gs *globalstore.Store) func() map[string]bool {
	return func() map[string]bool {
		marks := map[string]bool{}
		entries, err := projreg.List(gs.Home())
		if err != nil {
			return marks
		}
		for _, e := range entries {
			for _, ref := range e.References {
				marks[ref.StoreHash] = true
			}
			if e.Root == "" {
				continue
			}
			markLockfile(marks, e.Lockfile)
			markState(marks, e.Root)
			markActiveLinks(marks, e.Root, gs.Root())
		}
		return marks
	}
}

// markLockfile re-reads a project lockfile's gskill store hashes.
func markLockfile(marks map[string]bool, lockPath string) {
	l, err := skillslock.Load(lockPath)
	if err != nil {
		return
	}
	for _, name := range l.Names() {
		if e, ok := l.Entry(name); ok && e.Ext != nil && e.Ext.StoreHash != "" {
			marks[e.Ext.StoreHash] = true
		}
	}
}

// markState re-reads a project's machine-local state hashes.
func markState(marks map[string]bool, root string) {
	st, err := projstate.LoadOrInit(root)
	if err != nil {
		return
	}
	for _, sk := range st.Skills {
		if sk.StoreHash != "" {
			marks[sk.StoreHash] = true
		}
	}
}

// markActiveLinks scans a project's active layer for symlinks into the store
// and marks the objects they resolve to.
func markActiveLinks(marks map[string]bool, root, storeRoot string) {
	dir := filepath.Join(root, ".agents", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := storeRoot + string(filepath.Separator)
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil || !strings.HasPrefix(target, prefix) {
			continue
		}
		// <store>/<algo>/<hash>/content → "<algo>:<hash>"
		rel := strings.TrimPrefix(target, prefix)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 {
			marks[parts[0]+":"+parts[1]] = true
		}
	}
}

// StoreRepair restores one corrupted object from its recorded exact origin
// (FR-023). It fails without touching the object when the exact source
// cannot be reproduced.
func (a *App) StoreRepair(ctx context.Context, key string) error {
	gs, err := a.openGlobalStore()
	if err != nil {
		return err
	}
	if !gs.Has(key) {
		return errs.WithHint(
			fmt.Errorf("%w: store object %s not found", errs.ErrUsage, key),
			"run 'gskill store verify' to list objects and problems",
		)
	}
	if a.git == nil {
		return fmt.Errorf("%w: no git runner available for repair", errs.ErrSourceUnavailable)
	}
	fetch := func(source, commit, dest string) error {
		return a.git.FetchCommit(ctx, source, commit, dest)
	}
	return gs.Repair(ctx, key, fetch)
}

// StoreStatusReport summarizes the global store (contracts §2).
type StoreStatusReport struct {
	Path      string
	Objects   int
	SizeBytes int64
	Projects  int
	Unused    int
	Corrupted int
}

// StoreStatus reports store-wide counts and sizes.
func (a *App) StoreStatus(ctx context.Context) (StoreStatusReport, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return StoreStatusReport{}, err
	}
	rep := StoreStatusReport{Path: gs.Root()}

	items, err := storeListItems(gs)
	if err != nil {
		return rep, err
	}
	rep.Objects = len(items)
	for _, it := range items {
		rep.SizeBytes += it.SizeBytes
	}
	if entries, regErr := projreg.List(gs.Home()); regErr == nil {
		rep.Projects = len(entries)
	}
	if gcRep, gcErr := gs.GC(ctx, globalstore.GCOptions{
		GracePeriod: time.Nanosecond, MarkRefs: storeMarkRefs(gs),
	}); gcErr == nil {
		rep.Unused = len(gcRep.Candidates)
	}
	if scan, scanErr := gs.VerifyStore(globalstore.ScanOptions{}); scanErr == nil {
		for _, f := range scan.Findings {
			if f.Kind == globalstore.FindingCorrupted {
				rep.Corrupted++
			}
		}
	}
	return rep, nil
}

// StoreListItem is one object's listing row (contracts §2).
type StoreListItem struct {
	Key       string
	Skill     string
	Version   string
	SizeBytes int64
	Projects  int
}

// StoreList lists every object with its origin-derived display facts and the
// count of known referencing projects.
func (a *App) StoreList(_ context.Context) ([]StoreListItem, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return nil, err
	}
	items, err := storeListItems(gs)
	if err != nil {
		return nil, err
	}
	usedBy := storeUsedBy(gs)
	for i := range items {
		items[i].Projects = len(usedBy(items[i].Key))
	}
	return items, nil
}

// storeListItems collects per-object metadata rows, sorted by key.
func storeListItems(gs *globalstore.Store) ([]StoreListItem, error) {
	keys, err := gs.ListKeys()
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	items := make([]StoreListItem, 0, len(keys))
	for _, key := range keys {
		it := StoreListItem{Key: key}
		if meta, err := globalstore.ReadMetadata(gs.MetadataPath(key)); err == nil {
			it.SizeBytes = meta.SizeBytes
			if len(meta.Origins) > 0 {
				it.Skill = meta.Origins[0].SkillPath
				it.Version = meta.Origins[0].Version
			}
		}
		items = append(items, it)
	}
	return items, nil
}

// StoreInspectReport is one object's detail view (contracts §2).
type StoreInspectReport struct {
	Key       string
	Integrity string // "verified" | the failure detail
	SizeBytes int64
	Origins   []globalstore.Origin
	UsedBy    []string
	Pinned    bool
}

// StoreInspect verifies (full re-hash) and describes one object.
func (a *App) StoreInspect(_ context.Context, key string) (StoreInspectReport, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return StoreInspectReport{}, err
	}
	obj, err := gs.Open(key)
	if err != nil {
		return StoreInspectReport{}, errs.WithHint(
			fmt.Errorf("%w: store object %s not found", errs.ErrUsage, key),
			"run 'gskill store list' to see stored objects")
	}
	rep := StoreInspectReport{
		Key:       key,
		SizeBytes: obj.Metadata.SizeBytes,
		Origins:   obj.Metadata.Origins,
		UsedBy:    storeUsedBy(gs)(key),
		Pinned:    gs.Pinned(key),
		Integrity: "verified",
	}
	if err := gs.VerifyObject(key); err != nil {
		rep.Integrity = err.Error()
	}
	return rep, nil
}

// StoreGCReport is the outcome of a GC run (contracts §2).
type StoreGCReport struct {
	Applied bool
	globalstore.GCReport
}

// StoreGC runs garbage collection: dry-run by default, deleting only with
// apply (FR-025). olderThan overrides the configured grace period when > 0.
func (a *App) StoreGC(ctx context.Context, apply bool, olderThan time.Duration) (StoreGCReport, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return StoreGCReport{}, err
	}
	grace := a.cfg.StoreGCGracePeriod
	if olderThan > 0 {
		grace = olderThan
	}
	rep, err := gs.GC(ctx, globalstore.GCOptions{
		GracePeriod: grace,
		Apply:       apply,
		MarkRefs:    storeMarkRefs(gs),
	})
	if err != nil {
		return StoreGCReport{}, err
	}
	return StoreGCReport{Applied: apply, GCReport: rep}, nil
}

// StorePin exempts an object from GC (FR-026).
func (a *App) StorePin(_ context.Context, key string) error {
	gs, err := a.openGlobalStore()
	if err != nil {
		return err
	}
	return gs.Pin(key)
}

// StoreUnpin removes a GC exemption.
func (a *App) StoreUnpin(_ context.Context, key string) error {
	gs, err := a.openGlobalStore()
	if err != nil {
		return err
	}
	return gs.Unpin(key)
}

// StorePins lists pinned objects.
func (a *App) StorePins(_ context.Context) ([]string, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return nil, err
	}
	return gs.Pins()
}

// registerProject refreshes the project's advisory registry entry after a
// run that touched it (FR-027). Best-effort: a registration failure warns and
// never fails the operation.
func (a *App) registerProject(ctx context.Context, p *project, lf *skillslock.State) {
	if p.storeScope != config.StoreScopeGlobal || p.global == nil || !a.cfg.ProjectsRegistry {
		return
	}
	st, err := projstate.LoadOrInit(p.root)
	if err != nil {
		a.log.Warn("register project", "error", err)
		return
	}
	entry := projreg.Entry{
		ProjectID: st.ProjectID,
		Root:      p.root,
		Lockfile:  p.lockPath,
		LastSeen:  time.Now().UTC().Truncate(time.Second),
	}
	for _, name := range sortedKeys(lf.Skills) {
		if hash := lf.Skills[name].Resolved.ContentHash; hash != "" {
			entry.References = append(entry.References, projreg.Reference{Skill: name, StoreHash: hash})
		}
	}

	locker := globalstore.NewLocker(p.global.Home(), a.cfg.StoreLockTimeout, nil)
	lock, err := locker.LockRegistry(ctx)
	if err != nil {
		a.log.Warn("register project", "error", err)
		return
	}
	defer func() { _ = lock.Release() }()
	if err := projreg.Write(p.global.Home(), entry, a.cfg.PrivacyProjectRegistry); err != nil {
		a.log.Warn("register project", "error", err)
	}
}
