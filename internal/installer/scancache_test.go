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

// TestDiscoverAll_ScanCacheSkipsRescan: with a scan cache, the second
// DiscoverAll for the same commit is served from memory — proven by deleting
// the cached tree between calls.
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

	// Remove the material: a re-scan would now fail (nil git runner, cache
	// miss), so success proves the memo answered.
	if err := os.RemoveAll(c.Path(scanCommit)); err != nil {
		t.Fatal(err)
	}
	second, err := inst.DiscoverAll(ctx, req, discovery.Options{})
	if err != nil {
		t.Fatalf("second DiscoverAll (memo expected): %v", err)
	}
	if len(second.Skills) != 1 {
		t.Errorf("memoized skills = %d, want 1", len(second.Skills))
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
	if err := os.RemoveAll(c.Path(scanCommit)); err != nil {
		t.Fatal(err)
	}

	inst2 := installer.New(nil, c, store.New(filepath.Join(t.TempDir(), "store"))).WithScanCache(sc)
	if _, err := inst2.DiscoverAll(ctx, req, discovery.Options{}); err != nil {
		t.Fatalf("second installer, same cache: %v", err)
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
