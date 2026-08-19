package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/app"
)

// cacheCmd groups cache-management subcommands. The cache is the home-level
// commit-keyed clone cache shared by every project on the machine (spec 022):
// cleaning it affects all projects, but it is purely a performance cache —
// anything removed is re-fetched on demand.
type cacheCmd struct {
	Path  cachePathCmd  `cmd:"" help:"Print the cache directory."`
	Stats cacheStatsCmd `cmd:"" help:"Show cache size and entry count."`
	List  cacheListCmd  `cmd:"" help:"List cached entries."`
	Clean cacheCleanCmd `cmd:"" help:"Remove all cached material."`
}

type cachePathCmd struct{}

// Help returns the detailed help shown by `gskill cache path --help`.
func (cachePathCmd) Help() string {
	return examplesHelp("gskill cache path")
}

// Run prints the cache directory.
func (cachePathCmd) Run(out *Output, a *app.App) error {
	dir, err := a.CacheDir()
	if err != nil {
		return err
	}
	return out.Result(dir, map[string]any{"path": dir})
}

type cacheStatsCmd struct{}

// Help returns the detailed help shown by `gskill cache stats --help`.
func (cacheStatsCmd) Help() string {
	return examplesHelp("gskill cache stats --json")
}

// Run reports cache file count and total size.
func (cacheStatsCmd) Run(out *Output, a *app.App) error {
	dir, dirErr := a.CacheDir()
	if dirErr != nil {
		return dirErr
	}
	var files int
	var bytes int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		files++
		bytes += info.Size()
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("scan cache: %w", err)
	}
	human := fmt.Sprintf("%d file(s), %d bytes", files, bytes)
	human = out.summary(human)
	return out.Result(human, map[string]any{"files": files, "bytes": bytes})
}

type cacheListCmd struct{}

// Help returns the detailed help shown by `gskill cache list --help`.
func (cacheListCmd) Help() string {
	return examplesHelp("gskill cache list")
}

// Run lists cached entries (commit-keyed clones).
func (cacheListCmd) Run(out *Output, a *app.App) error {
	dir, dirErr := a.CacheDir()
	if dirErr != nil {
		return dirErr
	}
	keys := make([]string, 0)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read cache: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			keys = append(keys, e.Name())
		}
	}
	human := fmt.Sprintf("%d cached entr(ies)", len(keys))
	human = out.summary(human)
	return out.Result(human, map[string]any{"entries": keys})
}

type cacheCleanCmd struct{}

// Help returns the detailed help shown by `gskill cache clean --help`.
func (cacheCleanCmd) Help() string {
	return examplesHelp("gskill cache clean")
}

// Run removes all cached material (the shared home clone cache — affects
// every project on this machine; content is re-fetched on demand).
func (cacheCleanCmd) Run(out *Output, a *app.App) error {
	dir, dirErr := a.CacheDir()
	if dirErr != nil {
		return dirErr
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clean cache: %w", err)
	}
	human := "cache cleaned"
	human = out.summary(human)
	return out.Result(human, map[string]any{"cleaned": true})
}
