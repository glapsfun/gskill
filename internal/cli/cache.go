package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// cacheCmd groups cache-management subcommands.
type cacheCmd struct {
	Path  cachePathCmd  `cmd:"" help:"Print the cache directory."`
	Stats cacheStatsCmd `cmd:"" help:"Show cache size and entry count."`
	List  cacheListCmd  `cmd:"" help:"List cached entries."`
	Clean cacheCleanCmd `cmd:"" help:"Remove all cached material."`
}

func cacheDir(root string) string {
	return filepath.Join(root, ".gskill", "cache")
}

type cachePathCmd struct{}

// Help returns the detailed help shown by `gskill cache path --help`.
func (cachePathCmd) Help() string {
	return examplesHelp("gskill cache path")
}

// Run prints the cache directory.
func (cachePathCmd) Run(out *Output, root projectRoot) error {
	dir := cacheDir(string(root))
	return out.Result(dir, map[string]any{"path": dir})
}

type cacheStatsCmd struct{}

// Help returns the detailed help shown by `gskill cache stats --help`.
func (cacheStatsCmd) Help() string {
	return examplesHelp("gskill cache stats --json")
}

// Run reports cache file count and total size.
func (cacheStatsCmd) Run(out *Output, root projectRoot) error {
	var files int
	var bytes int64
	err := filepath.WalkDir(cacheDir(string(root)), func(_ string, d fs.DirEntry, err error) error {
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

// Run lists top-level cache entries (algorithm/hash).
func (cacheListCmd) Run(out *Output, root projectRoot) error {
	dir := cacheDir(string(root))
	keys := make([]string, 0)
	algos, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read cache: %w", err)
	}
	for _, algo := range algos {
		entries, _ := os.ReadDir(filepath.Join(dir, algo.Name()))
		for _, e := range entries {
			keys = append(keys, algo.Name()+":"+e.Name())
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

// Run removes all cached material.
func (cacheCleanCmd) Run(out *Output, root projectRoot) error {
	if err := os.RemoveAll(cacheDir(string(root))); err != nil {
		return fmt.Errorf("clean cache: %w", err)
	}
	human := "cache cleaned"
	human = out.summary(human)
	return out.Result(human, map[string]any{"cleaned": true})
}
