package globalstore

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
)

// VerifyObject establishes that the object for key is trustworthy: its
// metadata parses and matches, and a full re-hash of content/ equals key
// (clarification #2). On corruption the object is moved to quarantine and a
// fail-closed integrity error carrying expected and actual hashes is
// returned (FR-020/021). Missing objects report ErrObjectNotFound.
func (s *Store) VerifyObject(key string) error {
	if !s.Has(key) {
		return fmt.Errorf("%w: %s", ErrObjectNotFound, key)
	}

	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		if errors.Is(err, ErrSchemaVersion) {
			// A different gskill generation wrote this record; the object may
			// be healthy. Refuse to use it here, but never quarantine shared
			// content another (newer) binary can still serve.
			return fmt.Errorf("store object %s: %w", key, err)
		}
		if qErr := s.Quarantine(key); qErr != nil {
			return fmt.Errorf("%w (quarantine also failed: %w)", err, qErr)
		}
		return fmt.Errorf("store object %s: %w", key, err)
	}
	if meta.ContentHash != key {
		if qErr := s.Quarantine(key); qErr != nil {
			return fmt.Errorf("metadata/key mismatch on %s (quarantine also failed: %w)", key, qErr)
		}
		return errs.WithHint(
			fmt.Errorf("%w: object %s metadata records contentHash %s", errs.ErrIntegrity, key, meta.ContentHash),
			"run 'gskill store repair "+key+"' to restore it from its recorded origin")
	}

	hashes, err := integrity.HashDir(s.ContentPath(key))
	if err != nil {
		return fmt.Errorf("hash store object %s: %w", key, err)
	}
	if hashes.ContentHash != key {
		if qErr := s.Quarantine(key); qErr != nil {
			return fmt.Errorf("corrupted object %s (quarantine also failed: %w)", key, qErr)
		}
		return errs.WithHint(
			fmt.Errorf("%w: corrupted global store object\n  expected: %s\n  actual:   %s",
				errs.ErrIntegrity, key, hashes.ContentHash),
			"run 'gskill store repair "+key+"' to restore it from its recorded origin")
	}
	return nil
}

// Quarantine moves the object for key out of the store into
// <home>/quarantine/<key>-<timestamp>/ so it can never be activated, while
// preserving the evidence for repair and inspection (FR-021).
func (s *Store) Quarantine(key string) error {
	dest := fmt.Sprintf("%s/%s-%d", s.home.QuarantineDir(), safeKeyName(key), time.Now().UnixNano())
	if err := os.Rename(s.ObjectPath(key), dest); err != nil {
		return fmt.Errorf("quarantine %s: %w", key, err)
	}
	return nil
}
