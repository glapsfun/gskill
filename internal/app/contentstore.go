package app

import (
	"context"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/globalstore"
	"github.com/glapsfun/gskill/internal/installer"
)

// globalContentStore adapts the user-level global store to the installer's
// ContentStore seam (spec 015 FR-006). Verification depth follows
// store.verify_on_use: a full content re-hash on every activation by default
// (clarification #2), an existence/metadata check when disabled.
type globalContentStore struct {
	gs          *globalstore.Store
	verifyOnUse bool
}

func newGlobalContentStore(gs *globalstore.Store, cfg *config.Config) *globalContentStore {
	return &globalContentStore{gs: gs, verifyOnUse: cfg.StoreVerifyOnUse}
}

func (g *globalContentStore) Root() string            { return g.gs.Root() }
func (g *globalContentStore) Has(hash string) bool    { return g.gs.Has(hash) }
func (g *globalContentStore) Path(hash string) string { return g.gs.ContentPath(hash) }
func (g *globalContentStore) ScopeLabel() string      { return config.StoreScopeGlobal }

func (g *globalContentStore) Verify(hash string) error {
	if !g.verifyOnUse {
		// Light check: the object and its metadata must exist and parse; the
		// object is refused when unsafely owned or writable (FR-033).
		if _, err := g.gs.Open(hash); err != nil {
			return err
		}
		return g.gs.Home().CheckPathSafety(g.gs.ObjectPath(hash))
	}
	if err := g.gs.Home().CheckPathSafety(g.gs.ObjectPath(hash)); err != nil {
		return err
	}
	return g.gs.VerifyObject(hash)
}

func (g *globalContentStore) Put(ctx context.Context, hash, srcDir string, origin installer.ObjectOrigin) (string, error) {
	reused, err := g.gs.Admit(ctx, hash, srcDir, globalstore.Origin{
		SourceType: origin.SourceType,
		Source:     origin.Source,
		SkillPath:  origin.SkillPath,
		Version:    origin.Version,
		Ref:        origin.Ref,
		Commit:     origin.Commit,
	})
	if err != nil {
		return "", err
	}
	if reused {
		// Admission was satisfied by a pre-existing object: it, not the
		// freshly fetched content, is what activation will link — so it must
		// verify before use, or a tampered shared object would silently serve
		// this project (FR-020/021).
		if err := g.Verify(hash); err != nil {
			return "", err
		}
	}
	return g.gs.ContentPath(hash), nil
}

func (g *globalContentStore) Touch(ctx context.Context, hash string) {
	// Best-effort GC bookkeeping; a failure never fails an install.
	_ = g.gs.TouchLastUsed(ctx, hash)
}

// RecordOrigin merges origin metadata into an already-admitted object
// (installer.OriginRecorder) without re-copying or re-verifying content the
// caller just verified.
func (g *globalContentStore) RecordOrigin(ctx context.Context, hash string, origin installer.ObjectOrigin) error {
	return g.gs.RecordOrigin(ctx, hash, globalstore.Origin{
		SourceType: origin.SourceType,
		Source:     origin.Source,
		SkillPath:  origin.SkillPath,
		Version:    origin.Version,
		Ref:        origin.Ref,
		Commit:     origin.Commit,
	})
}
