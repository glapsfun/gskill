// Package migrate converts a project from the legacy project-local content
// store (<project>/.gskill/store) to the user-level global store (spec 015
// US5, FR-037/038). Migration is verified, deduplicating, and rollback-safe
// by construction: links switch only after every object is admitted and
// verified globally and every affected link is known re-pointable, and the
// local store is removed only after complete success.
package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/active"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/integrity"
)

// Plan describes what a migration would do (dry-run output, FR-037).
type Plan struct {
	Root          string
	LocalObjects  int
	AlreadyGlobal int
	ToCopy        int
	// SavingsBytes is the net disk the migration frees: the whole local
	// store goes away, minus the bytes newly copied into the global store.
	SavingsBytes int64
	// Corrupt lists local objects whose content no longer matches their key;
	// they are skipped (with their skills left on the local store) and
	// reported.
	Corrupt []string
}

// Result reports an executed migration.
type Result struct {
	Plan
	// AdmittedObjects is how many local objects now exist globally (reused
	// or copied).
	AdmittedObjects int
	// Relinked lists the skills whose active links now point into the
	// global store.
	Relinked []string
	// LocalStoreRemoved reports whether the legacy store directory was
	// deleted (only after complete success).
	LocalStoreRemoved bool
	// BlockedLinks lists active-layer entries that still resolve into the
	// legacy store but are not re-pointed by this migration — typically
	// entries another tool manages. Their presence blocks link switching
	// and legacy-store removal (FR-038).
	BlockedLinks []string
}

// LockedSkill is one lock entry's migration-relevant facts, supplied by the
// caller (the app layer owns lockfile parsing).
type LockedSkill struct {
	Name        string
	ContentHash string
	// Origin describes the entry's recorded source for global metadata.
	Origin globalstore.Origin
}

// localStoreDir returns the legacy store root under a project.
func localStoreDir(root string) string {
	return filepath.Join(root, ".gskill", "store")
}

// localCacheDir returns the legacy cache root under a project.
func localCacheDir(root string) string {
	return filepath.Join(root, ".gskill", "cache")
}

// listLocalObjects enumerates the legacy store's content keys.
func listLocalObjects(root string) ([]string, error) {
	storeRoot := localStoreDir(root)
	algos, err := os.ReadDir(storeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read local store: %w", err)
	}
	var keys []string
	for _, algo := range algos {
		if !algo.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(storeRoot, algo.Name()))
		if err != nil {
			return nil, fmt.Errorf("read local store %s: %w", algo.Name(), err)
		}
		for _, e := range entries {
			if e.IsDir() {
				keys = append(keys, algo.Name()+":"+e.Name())
			}
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// localObjectPath maps a key to its legacy store directory.
func localObjectPath(root, key string) string {
	return fsutil.KeyPath(localStoreDir(root), key)
}

// BuildPlan inspects the project's local store against the global store
// without changing anything (FR-037 dry-run).
func BuildPlan(root string, gs *globalstore.Store) (Plan, error) {
	plan, _, err := buildPlan(root, gs)
	return plan, err
}

// buildPlan is BuildPlan plus the enumerated key list, so Run never has to
// re-list (and possibly disagree with) the store the plan was computed from.
func buildPlan(root string, gs *globalstore.Store) (Plan, []string, error) {
	plan := Plan{Root: root}
	keys, err := listLocalObjects(root)
	if err != nil {
		return plan, nil, err
	}
	plan.LocalObjects = len(keys)

	var localTotal, copyBytes int64
	for _, key := range keys {
		dir := localObjectPath(root, key)
		size, err := fsutil.DirSize(dir)
		if err != nil {
			return plan, nil, err
		}
		localTotal += size

		hashes, err := integrity.HashDir(dir)
		if err != nil || hashes.ContentHash != key {
			plan.Corrupt = append(plan.Corrupt, key)
			continue
		}
		if gs.Has(key) {
			plan.AlreadyGlobal++
		} else {
			plan.ToCopy++
			copyBytes += size
		}
	}
	plan.SavingsBytes = localTotal - copyBytes
	return plan, keys, nil
}

// Run executes the migration (research R11): verify each local object,
// dedupe-or-copy into the global store, verify, re-point the project-active
// links for the given lock entries, and remove the legacy store and cache
// only after complete success. Links are switched only when the whole
// migration is known to complete: a corrupt or unadmittable object, or an
// active link migration cannot re-point (an external tool's entry resolving
// into the legacy store), leaves every link and the local store untouched so
// the project stays fully usable (FR-038).
func Run(ctx context.Context, root string, gs *globalstore.Store, skills []LockedSkill) (Result, error) {
	plan, keys, err := buildPlan(root, gs)
	if err != nil {
		return Result{}, err
	}
	res := Result{Plan: plan}
	if plan.LocalObjects == 0 {
		return res, nil // nothing to migrate
	}

	originByHash := make(map[string]globalstore.Origin, len(skills))
	for _, sk := range skills {
		originByHash[sk.ContentHash] = sk.Origin
	}

	corrupt := make(map[string]bool, len(plan.Corrupt))
	for _, key := range plan.Corrupt {
		corrupt[key] = true
	}

	admitted, err := admitAll(ctx, gs, root, keys, corrupt, originByHash, &res)
	if err != nil {
		return res, err
	}

	// Completeness is decided BEFORE any link switches: relinking only some
	// skills while the kept legacy store still pins the project to scope=
	// project would make the switched links fail closed as foreign on the
	// next run (FR-038). The old local store is preserved unless everything
	// below is known to succeed for every object, skill, and active link
	// (FR-037 step 13).
	relinkable, complete := relinkableSkills(gs, skills, corrupt, admitted)
	unmanaged, err := legacyActiveLinks(root, relinkable)
	if err != nil {
		return res, err
	}
	res.BlockedLinks = unmanaged
	if !complete || len(unmanaged) > 0 {
		return res, nil
	}

	if err := relinkAll(gs, root, skills, relinkable, &res); err != nil {
		return res, err
	}
	if err := os.RemoveAll(localStoreDir(root)); err != nil {
		return res, fmt.Errorf("remove legacy store: %w", err)
	}
	_ = os.RemoveAll(localCacheDir(root))
	res.LocalStoreRemoved = true
	return res, nil
}

// relinkableSkills returns the entries whose active links can switch to the
// global store, plus whether every entry and object migrated cleanly.
func relinkableSkills(gs *globalstore.Store, skills []LockedSkill, corrupt map[string]bool, admitted bool) (map[string]bool, bool) {
	relinkable := make(map[string]bool, len(skills))
	complete := admitted
	for _, sk := range skills {
		if sk.ContentHash == "" || corrupt[sk.ContentHash] || !gs.Has(sk.ContentHash) {
			complete = false
			continue
		}
		relinkable[sk.Name] = true
	}
	return relinkable, complete
}

// admitAll dedupes-or-copies every healthy local object into the global
// store. It reports whether every object made it. A fresh admission is
// already fail-closed verified by Admit's staged re-hash; only a reused
// pre-existing object is re-verified here, since its bytes were not part of
// this run's staging.
func admitAll(ctx context.Context, gs *globalstore.Store, root string, keys []string, corrupt map[string]bool, originByHash map[string]globalstore.Origin, res *Result) (bool, error) {
	complete := true
	for _, key := range keys {
		if corrupt[key] {
			complete = false
			continue
		}
		// Admit verifies the staged copy against key and re-checks under the
		// object lock; an existing identical object is reused (FR-037).
		reused, err := gs.Admit(ctx, key, localObjectPath(root, key), originByHash[key])
		if err != nil {
			return false, fmt.Errorf("admit %s: %w", key, err)
		}
		if reused {
			if err := gs.VerifyObject(key); err != nil {
				return false, fmt.Errorf("verify admitted %s: %w", key, err)
			}
		}
		res.AdmittedObjects++
	}
	return complete, nil
}

// legacyActiveLinks returns the active-layer entries that resolve into the
// legacy store but are NOT among the skills this migration re-points —
// typically entries another tool manages (no gskill block). Removing the
// legacy store would break them, so their presence blocks removal. Both
// sides of the comparison are symlink-resolved: a literal prefix match
// would fail OPEN for links reaching the store through a symlinked prefix
// (macOS /tmp → /private/tmp, symlinked home dirs), which is exactly the
// data-loss class this guard exists to close.
func legacyActiveLinks(root string, relinkable map[string]bool) ([]string, error) {
	names, err := active.List(root)
	if err != nil {
		return nil, err
	}
	storeRoot, err := filepath.Abs(localStoreDir(root))
	if err != nil {
		return nil, err
	}
	storeRoot = resolvePath(storeRoot)
	var blocked []string
	for _, name := range names {
		if relinkable[name] {
			continue
		}
		target, err := os.Readlink(active.Path(root, name))
		if err != nil {
			continue // absent or not a symlink: nothing in the legacy store
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(active.Dir(root), target)
		}
		// A dangling link fails EvalSymlinks and keeps its literal path; it
		// no longer reaches the store, so failing open for it loses nothing.
		rel, err := filepath.Rel(storeRoot, resolvePath(filepath.Clean(target)))
		if err != nil || strings.HasPrefix(filepath.ToSlash(rel), "..") {
			continue // resolves outside the legacy store
		}
		blocked = append(blocked, name)
	}
	sort.Strings(blocked)
	return blocked, nil
}

// resolvePath follows symlinks in path, falling back to the input when the
// path (or a prefix of it) does not exist.
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// relinkAll re-points each relinkable lock entry's active link at its
// verified global object. Agent links point through the active layer, so
// they follow untouched.
func relinkAll(gs *globalstore.Store, root string, skills []LockedSkill, relinkable map[string]bool, res *Result) error {
	for _, sk := range skills {
		if !relinkable[sk.Name] {
			continue
		}
		if _, err := active.EnsureActive(root, sk.Name, gs.ContentPath(sk.ContentHash),
			gs.Root(), localStoreDir(root)); err != nil {
			return fmt.Errorf("relink %s: %w", sk.Name, err)
		}
		res.Relinked = append(res.Relinked, sk.Name)
	}
	return nil
}
