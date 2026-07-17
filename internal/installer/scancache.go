package installer

import (
	"sync"

	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/source"
)

// ScanCache memoizes DiscoverAll results per immutable commit. Installers
// are constructed fresh per call site, so the App owns one ScanCache and
// injects it via WithScanCache; commit immutability makes App-lifetime reuse
// safe. Local sources are never cached — their trees can change between
// calls.
type ScanCache struct {
	mu sync.Mutex
	m  map[string]discovery.Result
}

// NewScanCache returns an empty scan cache safe for concurrent use.
func NewScanCache() *ScanCache {
	return &ScanCache{m: map[string]discovery.Result{}}
}

// WithScanCache attaches sc to the installer and returns it for chaining.
func (i *Installer) WithScanCache(sc *ScanCache) *Installer {
	i.scans = sc
	return i
}

// scanCacheKey derives the memo key for a request, reporting false when the
// scan must not be cached: local sources, unresolved commits, or any
// non-default scan option (the install pipeline always scans with a plain
// discovery.Options{RootID: ...}; filtered scans are rare and cheap to
// re-run relative to the risk of a stale filtered view).
func scanCacheKey(req Request, opts discovery.Options) (string, bool) {
	if req.Ref.Type == source.TypeLocal || req.Revision.Commit == "" {
		return "", false
	}
	if opts.MaxDepth != 0 || len(opts.Include) != 0 || len(opts.Exclude) != 0 || opts.IgnoreDirs != nil {
		return "", false
	}
	return req.Revision.Commit + "\x00" + opts.RootID, true
}

func (s *ScanCache) get(key string) (discovery.Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[key]
	return r, ok
}

func (s *ScanCache) put(key string, r discovery.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = r
}
