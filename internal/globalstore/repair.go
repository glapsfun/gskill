package globalstore

import (
	"context"
	"fmt"
	"os"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
)

// FetchFunc downloads the exact commit of a source into dest. The installer
// wires this to the git runner; tests substitute stubs.
type FetchFunc func(source, commit, dest string) error

// Repair restores the object for key from its recorded origin (FR-023): it
// picks a commit-bearing origin, re-fetches exactly that commit into staging,
// verifies the staged content hashes to key, and atomically replaces the
// object. It fails — leaving the existing object untouched — when no origin
// records an exact commit or when the re-fetched content differs: an object
// is never silently replaced with different content.
func (s *Store) Repair(ctx context.Context, key string, fetch FetchFunc) error {
	lock, err := s.locker().LockObject(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		return fmt.Errorf("repair %s: %w", key, err)
	}
	origin, ok := commitOrigin(meta.Origins)
	if !ok {
		return errs.WithHint(
			fmt.Errorf("repair %s: no recorded origin carries an exact commit — the exact source cannot be reproduced", key),
			"re-install the skill in a project that locks this content to re-admit it")
	}

	stage, err := fsutil.OwnerOnlyTempDir(s.home.TmpDir(), "repair-"+shortKey(key)+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	fetched := stage + "/fetched"
	if err := fetch(origin.Source, origin.Commit, fetched); err != nil {
		return fmt.Errorf("repair %s: fetch %s@%s: %w", key, origin.Source, origin.Commit, err)
	}
	skillDir := fetched
	if origin.SkillPath != "" {
		skillDir = fetched + "/" + origin.SkillPath
	}

	staged, err := stageVerified(key, skillDir, stage)
	if err != nil {
		return fmt.Errorf("repair %s: %w", key, err)
	}

	// Atomic replace: swap the fresh content in, then drop the old one.
	old := s.ContentPath(key) + ".pre-repair"
	if err := os.Rename(s.ContentPath(key), old); err != nil {
		return fmt.Errorf("repair %s: set aside corrupted content: %w", key, err)
	}
	if err := os.Rename(staged, s.ContentPath(key)); err != nil {
		_ = os.Rename(old, s.ContentPath(key)) // restore; repair failed
		return fmt.Errorf("repair %s: swap in repaired content: %w", key, err)
	}
	_ = os.RemoveAll(old)

	meta.SizeBytes, _ = fsutil.DirSize(s.ContentPath(key))
	return WriteMetadata(s.MetadataPath(key), meta)
}

// commitOrigin returns the first origin recording an exact commit.
func commitOrigin(origins []Origin) (Origin, bool) {
	for _, o := range origins {
		if o.Commit != "" {
			return o, true
		}
	}
	return Origin{}, false
}
