package globalstore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/integrity"
)

// Admit stores srcDir's content under expectedKey, recording origin. It is
// the only way content enters the store (FR-006):
//
//  1. take the object lock and re-check existence (a concurrent admitter may
//     have won);
//  2. stage into an owner-only temp dir under <home>/tmp;
//  3. validate the content (path traversal, escaping symlinks) — fetched
//     content is never executed;
//  4. re-hash the staged copy and verify it equals expectedKey, failing
//     closed on mismatch with nothing admitted;
//  5. write metadata, then promote content with an atomic same-filesystem
//     rename.
//
// An object that already exists is reused: its metadata gains origin and the
// call reports reused=true.
func (s *Store) Admit(ctx context.Context, expectedKey, srcDir string, origin Origin) (reused bool, err error) {
	locker := s.locker()
	lock, err := locker.LockObject(ctx, expectedKey)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Release() }()

	if s.Has(expectedKey) {
		if origin != (Origin{}) {
			if err := s.recordOriginLocked(expectedKey, origin); err != nil {
				return true, err
			}
		}
		return true, nil
	}

	stage, err := fsutil.OwnerOnlyTempDir(s.home.TmpDir(), "object-"+shortKey(expectedKey)+"-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	staged, err := stageVerified(expectedKey, srcDir, stage)
	if err != nil {
		return false, err
	}

	size, err := dirSize(staged)
	if err != nil {
		return false, fmt.Errorf("measure staged content: %w", err)
	}
	objDir := s.ObjectPath(expectedKey)
	if err := os.MkdirAll(objDir, 0o700); err != nil {
		return false, fmt.Errorf("create object dir: %w", err)
	}
	meta := Metadata{
		SchemaVersion: metadataSchemaVersion,
		ContentHash:   expectedKey,
		SizeBytes:     size,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		Origins:       MergeOrigins(nil, origin),
	}
	if origin == (Origin{}) {
		meta.Origins = []Origin{}
	}
	if err := WriteMetadata(s.MetadataPath(expectedKey), meta); err != nil {
		return false, err
	}

	if err := os.Rename(staged, s.ContentPath(expectedKey)); err != nil {
		// Clean the half-made object dir so no metadata-only husk remains.
		_ = os.RemoveAll(objDir)
		if s.Has(expectedKey) { // concurrent admitter won the rename race
			return true, nil
		}
		return false, fmt.Errorf("promote into store: %w", err)
	}
	return false, nil
}

// RecordOrigin merges origin into an admitted object's metadata under the
// object lock. Content is never touched (FR-004).
func (s *Store) RecordOrigin(ctx context.Context, key string, origin Origin) error {
	lock, err := s.locker().LockObject(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return s.recordOriginLocked(key, origin)
}

// recordOriginLocked merges origin while the caller holds the object lock.
func (s *Store) recordOriginLocked(key string, origin Origin) error {
	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		return err
	}
	merged := MergeOrigins(meta.Origins, origin)
	if len(merged) == len(meta.Origins) {
		return nil // already recorded
	}
	meta.Origins = merged
	return WriteMetadata(s.MetadataPath(key), meta)
}

// TouchLastUsed stamps the object's lastUsedAt under the object lock. It is
// best-effort bookkeeping for GC reporting; content is never touched.
func (s *Store) TouchLastUsed(ctx context.Context, key string) error {
	lock, err := s.locker().LockObject(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		return err
	}
	meta.LastUsedAt = time.Now().UTC().Truncate(time.Second)
	return WriteMetadata(s.MetadataPath(key), meta)
}

// SetLocker overrides the store's locker (timeout, notice writer). A nil
// locker resets to the default.
func (s *Store) SetLocker(l *Locker) { s.locks = l }

// locker returns the configured locker, defaulting to a silent 60s one.
func (s *Store) locker() *Locker {
	if s.locks == nil {
		s.locks = NewLocker(s.home, 60*time.Second, nil)
	}
	return s.locks
}

// shortKey returns a filesystem-friendly abbreviation of a content key for
// staging directory names.
func shortKey(key string) string {
	const maxLen = 20
	safe := make([]byte, 0, maxLen)
	for i := 0; i < len(key) && len(safe) < maxLen; i++ {
		c := key[i]
		if c == ':' {
			continue
		}
		safe = append(safe, c)
	}
	return string(safe)
}

// dirSize sums the sizes of regular files under dir.
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

// stageVerified copies srcDir into the staging area, validates it (path
// traversal, escaping symlinks — content is never executed), and verifies the
// staged copy hashes to expectedKey, failing closed on mismatch.
func stageVerified(expectedKey, srcDir, stage string) (string, error) {
	staged := filepath.Join(stage, contentDirName)
	if err := fsutil.CopyDir(srcDir, staged); err != nil {
		return "", fmt.Errorf("stage content: %w", err)
	}
	if _, err := integrity.ValidateContent(staged); err != nil {
		return "", fmt.Errorf("validate staged content: %w", err)
	}
	hashes, err := integrity.HashDir(staged)
	if err != nil {
		return "", fmt.Errorf("hash staged content: %w", err)
	}
	if hashes.ContentHash != expectedKey {
		return "", errs.WithHint(
			fmt.Errorf("%w: staged content hashes to %s, expected %s — refusing to admit",
				errs.ErrIntegrity, hashes.ContentHash, expectedKey),
			"the source may have changed; re-resolve the skill or update the lockfile")
	}
	return staged, nil
}
