package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/projreg"
)

// ProjectInfo is one registered project's listing row (contracts §3).
type ProjectInfo struct {
	ProjectID string
	Root      string
	Skills    int
	LastSeen  time.Time
	// Missing reports a recorded root that no longer exists on disk.
	Missing bool
}

// ProjectsList lists the advisory registry (FR-028).
func (a *App) ProjectsList(_ context.Context) ([]ProjectInfo, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return nil, err
	}
	entries, err := projreg.List(gs.Home())
	if err != nil {
		return nil, err
	}
	out := make([]ProjectInfo, 0, len(entries))
	for _, e := range entries {
		info := ProjectInfo{
			ProjectID: e.ProjectID,
			Root:      e.Root,
			Skills:    len(e.References),
			LastSeen:  e.LastSeen,
		}
		if e.Root != "" {
			if _, statErr := os.Stat(e.Root); statErr != nil {
				info.Missing = true
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// ProjectsInspect returns one registry entry.
func (a *App) ProjectsInspect(_ context.Context, projectID string) (projreg.Entry, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return projreg.Entry{}, err
	}
	e, ok, err := projreg.Get(gs.Home(), projectID)
	if err != nil {
		return projreg.Entry{}, err
	}
	if !ok {
		return projreg.Entry{}, errs.WithHint(
			fmt.Errorf("%w: project %s is not registered", errs.ErrUsage, projectID),
			"run 'gskill projects list' to see registered projects")
	}
	return e, nil
}

// ProjectsPrune removes registry entries whose project no longer exists. It
// removes registry files only — never repository content (FR-028).
func (a *App) ProjectsPrune(ctx context.Context) ([]string, error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return nil, err
	}
	locker := globalstore.NewLocker(gs.Home(), a.cfg.StoreLockTimeout, nil)
	lock, err := locker.LockRegistry(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()
	return projreg.Prune(gs.Home())
}

// ProjectsRefresh re-derives every registered entry from its project's
// current lockfile, dropping entries per the privacy mode (FR-028/029).
func (a *App) ProjectsRefresh(ctx context.Context) (refreshed []string, err error) {
	gs, err := a.openGlobalStore()
	if err != nil {
		return nil, err
	}
	entries, err := projreg.List(gs.Home())
	if err != nil {
		return nil, err
	}
	locker := globalstore.NewLocker(gs.Home(), a.cfg.StoreLockTimeout, nil)
	lock, err := locker.LockRegistry(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()

	for _, e := range entries {
		if e.Root == "" {
			continue // minimal entries cannot be re-derived
		}
		lf, lfErr := loadOrNewLock(e.Lockfile)
		if lfErr != nil {
			continue
		}
		e.References = e.References[:0]
		for _, name := range sortedKeys(lf.Skills) {
			if hash := lf.Skills[name].Resolved.ContentHash; hash != "" {
				e.References = append(e.References, projreg.Reference{Skill: name, StoreHash: hash})
			}
		}
		e.LastSeen = time.Now().UTC().Truncate(time.Second)
		if wErr := projreg.Write(gs.Home(), e, a.cfg.PrivacyProjectRegistry); wErr != nil {
			return refreshed, wErr
		}
		refreshed = append(refreshed, e.ProjectID)
	}
	return refreshed, nil
}
