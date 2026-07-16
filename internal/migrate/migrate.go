// Package migrate converts a project from the legacy project-local content
// store (<project>/.gskill/store) to the user-level global store (spec 015
// US5, FR-037/038). Migration is verified, deduplicating, and rollback-safe
// by construction: links switch only after the global object is verified,
// and the local store is removed only after complete success.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/active"
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
	return filepath.Join(localStoreDir(root), filepath.FromSlash(keyToPath(key)))
}

// keyToPath converts "sha256:abc" to "sha256/abc".
func keyToPath(key string) string {
	for i := range len(key) {
		if key[i] == ':' {
			return key[:i] + "/" + key[i+1:]
		}
	}
	return key
}

// BuildPlan inspects the project's local store against the global store
// without changing anything (FR-037 dry-run).
func BuildPlan(root string, gs *globalstore.Store) (Plan, error) {
	plan := Plan{Root: root}
	keys, err := listLocalObjects(root)
	if err != nil {
		return plan, err
	}
	plan.LocalObjects = len(keys)

	var localTotal, copyBytes int64
	for _, key := range keys {
		dir := localObjectPath(root, key)
		size, err := dirSize(dir)
		if err != nil {
			return plan, err
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
	return plan, nil
}

// Run executes the migration (research R11): verify each local object,
// dedupe-or-copy into the global store, verify, re-point the project-active
// links for the given lock entries, and remove the legacy store and cache
// only after complete success. Corrupt local objects are skipped and
// reported; any skip, admission failure, or relink failure leaves the local
// store in place so the project stays fully usable (FR-038).
func Run(ctx context.Context, root string, gs *globalstore.Store, skills []LockedSkill) (Result, error) {
	plan, err := BuildPlan(root, gs)
	if err != nil {
		return Result{}, err
	}
	res := Result{Plan: plan}
	if plan.LocalObjects == 0 {
		return res, nil // nothing to migrate
	}

	keys, err := listLocalObjects(root)
	if err != nil {
		return res, err
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
	relinked, err := relinkAll(gs, root, skills, corrupt, &res)
	if err != nil {
		return res, err
	}
	complete := admitted && relinked

	// The old local store is preserved until everything above succeeded for
	// every object and skill (FR-037 step 13, FR-038).
	if complete {
		if err := os.RemoveAll(localStoreDir(root)); err != nil {
			return res, fmt.Errorf("remove legacy store: %w", err)
		}
		_ = os.RemoveAll(localCacheDir(root))
		res.LocalStoreRemoved = true
	}
	return res, nil
}

// dirSize sums regular-file sizes under dir.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// admitAll dedupes-or-copies every healthy local object into the global
// store, verifying each admission. It reports whether every object made it.
func admitAll(ctx context.Context, gs *globalstore.Store, root string, keys []string, corrupt map[string]bool, originByHash map[string]globalstore.Origin, res *Result) (bool, error) {
	complete := true
	for _, key := range keys {
		if corrupt[key] {
			complete = false
			continue
		}
		// Admit verifies the staged copy against key and re-checks under the
		// object lock; an existing identical object is reused (FR-037).
		if _, err := gs.Admit(ctx, key, localObjectPath(root, key), originByHash[key]); err != nil {
			return false, fmt.Errorf("admit %s: %w", key, err)
		}
		if err := gs.VerifyObject(key); err != nil {
			return false, fmt.Errorf("verify admitted %s: %w", key, err)
		}
		res.AdmittedObjects++
	}
	return complete, nil
}

// relinkAll re-points each lock entry's active link at its verified global
// object. Agent links point through the active layer, so they follow
// untouched. It reports whether every skill was relinked.
func relinkAll(gs *globalstore.Store, root string, skills []LockedSkill, corrupt map[string]bool, res *Result) (bool, error) {
	complete := true
	for _, sk := range skills {
		if sk.ContentHash == "" || corrupt[sk.ContentHash] || !gs.Has(sk.ContentHash) {
			complete = false
			continue
		}
		if _, err := active.EnsureActive(root, sk.Name, gs.ContentPath(sk.ContentHash),
			gs.Root(), localStoreDir(root)); err != nil {
			return false, fmt.Errorf("relink %s: %w", sk.Name, err)
		}
		res.Relinked = append(res.Relinked, sk.Name)
	}
	return complete, nil
}
