package app

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
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
	rep, err := gs.VerifyStore(globalstore.ScanOptions{UsedBy: a.storeUsedBy()})
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

// storeUsedBy resolves which known projects reference an object. It is
// advisory (registry-backed once available) and never required for
// correctness; without a registry it reports nothing.
func (a *App) storeUsedBy() func(key string) []string {
	return nil
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
