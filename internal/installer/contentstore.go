package installer

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/store"
)

// ContentStore abstracts the canonical content store the installer reuses
// from, admits into, and activates out of (spec 015 FR-006). The legacy
// project-local store and the user-level global store both satisfy it.
type ContentStore interface {
	// Root is the store root that activation and ownership checks resolve
	// symlinks against.
	Root() string
	// Has reports whether content for hash is present (existence only).
	Has(hash string) bool
	// Path returns the content directory for hash, whether or not it exists.
	Path(hash string) string
	// Verify establishes that the content for hash is trustworthy before
	// activation, failing closed on corruption (FR-020/021).
	Verify(hash string) error
	// Put admits srcDir under hash and returns the stored content path. It is
	// idempotent; origin is descriptive metadata for stores that record it.
	Put(ctx context.Context, hash, srcDir string, origin ObjectOrigin) (string, error)
	// Touch records a best-effort last-used signal for hash.
	Touch(ctx context.Context, hash string)
	// ScopeLabel names the store's physical scope for reporting: "project" or
	// "global".
	ScopeLabel() string
}

// ObjectOrigin describes where admitted content came from. Stores that keep
// origin metadata (the global store) record it; others ignore it.
type ObjectOrigin struct {
	SourceType string
	Source     string
	SkillPath  string
	Version    string
	Ref        string
	Commit     string
}

// Store-reuse outcomes recorded on Result.StoreReuse.
const (
	StoreReused     = "reused"
	StoreDownloaded = "downloaded"
)

// legacyStore adapts the project-local store.Store to ContentStore with the
// pre-existing semantics: Put re-hashes the stored copy and fails closed on a
// mismatch, exactly like the old stageAndVerify.
type legacyStore struct {
	s *store.Store
}

func (l legacyStore) Root() string            { return l.s.Root() }
func (l legacyStore) Has(hash string) bool    { return l.s.Has(hash) }
func (l legacyStore) Path(hash string) string { return l.s.Path(hash) }
func (l legacyStore) ScopeLabel() string      { return "project" }

func (l legacyStore) Verify(hash string) error {
	check, err := integrity.HashDir(l.s.Path(hash))
	if err != nil {
		return fmt.Errorf("hash stored content: %w", err)
	}
	if check.ContentHash != hash {
		return fmt.Errorf("%w: stored content %s != expected %s",
			errs.ErrIntegrity, check.ContentHash, hash)
	}
	return nil
}

func (l legacyStore) Put(_ context.Context, hash, srcDir string, _ ObjectOrigin) (string, error) {
	storePath, err := l.s.Put(hash, srcDir)
	if err != nil {
		return "", err
	}
	if err := l.Verify(hash); err != nil {
		return "", err
	}
	return storePath, nil
}

func (l legacyStore) Touch(context.Context, string) {}
