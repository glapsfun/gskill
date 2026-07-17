package installer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/resolver"
	"github.com/glapsfun/gskill/internal/source"
	"github.com/glapsfun/gskill/internal/store"
)

const scanCommit = "1111111111111111111111111111111111111111"

// cachedInstaller returns an Installer whose commit cache already holds one
// skill tree for scanCommit, so DiscoverAll needs no git runner at all.
func cachedInstaller(t *testing.T, sc *installer.ScanCache) (*installer.Installer, *cache.Cache) {
	t.Helper()

	material := t.TempDir()
	body := "---\nname: widgets\ndescription: a skill\n---\n# widgets\n"
	if err := os.WriteFile(filepath.Join(material, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := cache.New(t.TempDir())
	if _, err := c.Put(scanCommit, material); err != nil {
		t.Fatal(err)
	}
	inst := installer.New(nil, c, store.New(filepath.Join(t.TempDir(), "store")))
	if sc != nil {
		inst = inst.WithScanCache(sc)
	}
	return inst, c
}

func scanCacheRequest(t *testing.T) installer.Request {
	t.Helper()

	ref, err := source.Parse("github.com/acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	return installer.Request{Ref: ref, Revision: resolver.Revision{Commit: scanCommit}}
}

// addSkillToCachedTree drops a second skill into the cached material, so a
// real re-scan would find 2 skills while a memo hit still reports 1.
func addSkillToCachedTree(t *testing.T, c *cache.Cache) {
	t.Helper()

	dir := filepath.Join(c.Path(scanCommit), "extra")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: extra\ndescription: a skill\n---\n# extra\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDiscoverAll_ScanCacheSkipsRescan: with a scan cache, the second
// DiscoverAll for the same commit is served from memory — proven by adding a
// skill to the cached tree between calls: a re-scan would see it, the memo
// must not (commits are immutable; the cache dir only mutates in tests).
func TestDiscoverAll_ScanCacheSkipsRescan(t *testing.T) {
	t.Parallel()

	inst, c := cachedInstaller(t, installer.NewScanCache())
	ctx := context.Background()
	req := scanCacheRequest(t)

	first, err := inst.DiscoverAll(ctx, req, discovery.Options{})
	if err != nil {
		t.Fatalf("first DiscoverAll: %v", err)
	}
	if len(first.Skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(first.Skills))
	}

	addSkillToCachedTree(t, c)
	second, err := inst.DiscoverAll(ctx, req, discovery.Options{})
	if err != nil {
		t.Fatalf("second DiscoverAll (memo expected): %v", err)
	}
	if len(second.Skills) != 1 {
		t.Errorf("memoized skills = %d, want 1 (a re-scan would report 2)", len(second.Skills))
	}
}

// TestDiscoverAll_ScanCacheIsSharedAcrossInstallers: installers are built
// fresh per call site, so the cache injected via WithScanCache must carry
// results between instances.
func TestDiscoverAll_ScanCacheIsSharedAcrossInstallers(t *testing.T) {
	t.Parallel()

	sc := installer.NewScanCache()
	inst1, c := cachedInstaller(t, sc)
	ctx := context.Background()
	req := scanCacheRequest(t)
	if _, err := inst1.DiscoverAll(ctx, req, discovery.Options{}); err != nil {
		t.Fatal(err)
	}
	addSkillToCachedTree(t, c)

	inst2 := installer.New(nil, c, store.New(filepath.Join(t.TempDir(), "store"))).WithScanCache(sc)
	got, err := inst2.DiscoverAll(ctx, req, discovery.Options{})
	if err != nil {
		t.Fatalf("second installer, same cache: %v", err)
	}
	if len(got.Skills) != 1 {
		t.Errorf("memoized skills = %d, want 1 (a re-scan would report 2)", len(got.Skills))
	}
}

// TestDiscoverAll_ScanCachePrunedTreeFallsThrough: a memo hit whose material
// directory was removed (cache GC mid-run) must not hand out paths into the
// deleted tree — it forgets the entry and re-materializes, which here fails
// loudly (nil git runner) instead of returning stale Dirs.
func TestDiscoverAll_ScanCachePrunedTreeFallsThrough(t *testing.T) {
	t.Parallel()

	inst, c := cachedInstaller(t, installer.NewScanCache())
	ctx := context.Background()
	req := scanCacheRequest(t)
	if _, err := inst.DiscoverAll(ctx, req, discovery.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(c.Path(scanCommit)); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.DiscoverAll(ctx, req, discovery.Options{}); err == nil {
		t.Error("memo served a scan of a pruned tree; must fall through to materialize")
	}
}

// TestDiscoverAll_NonDefaultOptionsBypassScanCache: only the plain options
// shape used by the install pipeline is cached; filtered scans re-run.
func TestDiscoverAll_NonDefaultOptionsBypassScanCache(t *testing.T) {
	t.Parallel()

	inst, c := cachedInstaller(t, installer.NewScanCache())
	ctx := context.Background()
	req := scanCacheRequest(t)
	if _, err := inst.DiscoverAll(ctx, req, discovery.Options{MaxDepth: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(c.Path(scanCommit)); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.DiscoverAll(ctx, req, discovery.Options{MaxDepth: 1}); err == nil {
		t.Error("filtered scan was served from cache; non-default options must bypass")
	}
}
