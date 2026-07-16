package globalstore

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// GCOptions configures a garbage-collection run (FR-024/025).
type GCOptions struct {
	// GracePeriod is the minimum object age (from max(createdAt, lastUsedAt))
	// before an unreferenced object becomes deletable. Zero uses 30 days.
	GracePeriod time.Duration
	// Apply performs deletions; false (the default) is a dry run.
	Apply bool
	// MarkRefs returns the content keys referenced by live projects —
	// lockfiles, state files, and active links, typically fed by the advisory
	// registry. It is re-invoked under the GC lock before any deletion so a
	// stale pre-lock scan can never justify a delete. A nil MarkRefs marks
	// nothing (degraded mode: only pins, locks, and the grace period protect).
	MarkRefs func() map[string]bool
}

// defaultGracePeriod is the conservative default before deletion (spec
// clarification: 30 days).
const defaultGracePeriod = 30 * 24 * time.Hour

// GCCandidate is one object eligible for deletion.
type GCCandidate struct {
	Key       string
	SizeBytes int64
	// Skill and Version come from the first recorded origin (display only).
	Skill    string
	Version  string
	LastUsed time.Time
}

// GCReport summarizes a GC run.
type GCReport struct {
	Candidates       []GCCandidate
	ReclaimableBytes int64
	// Deleted lists the keys actually removed (apply mode only).
	Deleted []string
	// Skipped lists keys that became protected between marking and deletion
	// (gc-conflict: lock held, re-marked, or re-pinned) — reported, never an
	// error (error contract).
	Skipped []string
	// Degraded reports that no reference marking was available.
	Degraded bool
}

// GC runs conservative mark-and-sweep over the store (FR-024/025). An object
// is a candidate only when it is unreferenced, unpinned, metadata-valid,
// safely owned, and older than the grace period. Dry runs only report; apply
// mode takes the global GC lock, re-marks, then re-checks each candidate
// under its object lock immediately before deletion (FR-031).
func (s *Store) GC(ctx context.Context, opts GCOptions) (GCReport, error) {
	var rep GCReport
	if opts.MarkRefs == nil {
		rep.Degraded = true
	}

	if !opts.Apply {
		candidates, err := s.gcCandidates(opts)
		if err != nil {
			return rep, err
		}
		rep.Candidates = candidates
		for _, c := range candidates {
			rep.ReclaimableBytes += c.SizeBytes
		}
		return rep, nil
	}

	gcLock, err := s.locker().LockGC(ctx)
	if err != nil {
		return rep, err
	}
	defer func() { _ = gcLock.Release() }()

	// Re-derive candidates under the GC lock: marks are re-collected so a
	// project registered after the caller's first look is still protected.
	candidates, err := s.gcCandidates(opts)
	if err != nil {
		return rep, err
	}
	rep.Candidates = candidates

	for _, c := range candidates {
		deleted, err := s.deleteIfStillUnused(ctx, c.Key, opts)
		if err != nil {
			return rep, err
		}
		if !deleted {
			rep.Skipped = append(rep.Skipped, c.Key)
			continue
		}
		rep.Deleted = append(rep.Deleted, c.Key)
		rep.ReclaimableBytes += c.SizeBytes
		_ = s.Unpin(c.Key) // clear any stale pin marker bookkeeping
	}

	// Sweep abandoned staging left by interrupted admissions (FR-032). An
	// hour is far past any live admission; in-flight staging is younger.
	if stale, err := fsutil.ListStaleDirs(s.home.TmpDir(), time.Hour); err == nil {
		for _, dir := range stale {
			_ = os.RemoveAll(dir)
		}
	}
	return rep, nil
}

// gcCandidates computes the deletable set per the sweep gates (FR-024).
func (s *Store) gcCandidates(opts GCOptions) ([]GCCandidate, error) {
	keys, err := s.ListKeys()
	if err != nil {
		return nil, err
	}
	marked := map[string]bool{}
	if opts.MarkRefs != nil {
		marked = opts.MarkRefs()
	}
	grace := opts.GracePeriod
	if grace <= 0 {
		grace = defaultGracePeriod
	}
	cutoff := time.Now().Add(-grace)

	var out []GCCandidate
	for _, key := range keys {
		if marked[key] || s.Pinned(key) {
			continue
		}
		meta, err := ReadMetadata(s.MetadataPath(key))
		if err != nil {
			continue // invalid metadata is never swept (FR-024); verify reports it
		}
		if err := s.home.CheckPathSafety(s.ObjectPath(key)); err != nil {
			continue // not safely gskill-owned: never swept
		}
		age := meta.CreatedAt
		if meta.LastUsedAt.After(age) {
			age = meta.LastUsedAt
		}
		if age.After(cutoff) {
			continue // inside the grace period
		}
		c := GCCandidate{Key: key, SizeBytes: meta.SizeBytes, LastUsed: age}
		if len(meta.Origins) > 0 {
			c.Skill = meta.Origins[0].SkillPath
			c.Version = meta.Origins[0].Version
		}
		out = append(out, c)
	}
	return out, nil
}

// deleteIfStillUnused takes the object's lock and re-checks every protection
// immediately before deleting (FR-031). A lock held by another process, or a
// protection that appeared meanwhile, skips the object (gc-conflict).
func (s *Store) deleteIfStillUnused(ctx context.Context, key string, opts GCOptions) (bool, error) {
	// A short timeout: a held lock means an active operation — skip, don't wait.
	shortLocker := NewLocker(s.home, 2*time.Second, nil)
	lock, err := shortLocker.LockObject(ctx, key)
	if err != nil {
		// Lock held means an active operation: gc-conflict skip, not an error.
		return false, nil
	}
	defer func() { _ = lock.Release() }()

	if s.Pinned(key) {
		return false, nil
	}
	if opts.MarkRefs != nil && opts.MarkRefs()[key] {
		return false, nil
	}
	if err := os.RemoveAll(s.ObjectPath(key)); err != nil {
		return false, fmt.Errorf("delete store object %s: %w", key, err)
	}
	return true, nil
}
