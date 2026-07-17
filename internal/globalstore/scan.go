package globalstore

import (
	"fmt"
	"os"
	"time"

	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/integrity"
)

// FindingKind classifies one store-scan problem.
type FindingKind string

// Store-scan finding kinds (FR-022).
const (
	FindingCorrupted       FindingKind = "corrupted"
	FindingMalformed       FindingKind = "malformed"
	FindingInvalidMetadata FindingKind = "invalid-metadata"
	FindingUnsafePerms     FindingKind = "unsafe-permissions"
	FindingStrayStaging    FindingKind = "stray-staging"
)

// ScanFinding is one problem the store scan found.
type ScanFinding struct {
	Kind FindingKind
	// Key is the affected object's content key (empty for stray staging).
	Key string
	// Detail is a human-readable description.
	Detail string
	// Expected and Actual carry the hash pair for corruption findings.
	Expected string
	Actual   string
	// UsedBy lists the known projects referencing the object.
	UsedBy []string
	// Path is the offending filesystem path.
	Path string
}

// ScanReport summarizes a store-wide verification (FR-022).
type ScanReport struct {
	Checked  int
	Healthy  int
	Findings []ScanFinding
}

// ScanOptions configures VerifyStore.
type ScanOptions struct {
	// UsedBy resolves the projects known to reference an object key; nil
	// leaves UsedBy empty (the scan stays registry-independent).
	UsedBy func(key string) []string
	// StrayAge is the minimum age of a tmp/ entry to report as abandoned
	// staging; zero uses one hour.
	StrayAge time.Duration
}

// VerifyStore scans every object: full content re-hash, metadata validation,
// malformed-layout detection, unsafe permissions, and abandoned staging
// directories (FR-022). It reports; it never quarantines or deletes — those
// are activation's and repair's jobs.
func (s *Store) VerifyStore(opts ScanOptions) (ScanReport, error) {
	var rep ScanReport
	keys, err := s.ListKeys()
	if err != nil {
		return rep, err
	}
	usedBy := opts.UsedBy
	if usedBy == nil {
		usedBy = func(string) []string { return nil }
	}

	for _, key := range keys {
		rep.Checked++
		if f, healthy := s.scanObject(key, usedBy); healthy {
			rep.Healthy++
		} else {
			rep.Findings = append(rep.Findings, f)
		}
	}

	if perms, err := s.home.CheckPerms(); err == nil {
		for _, p := range perms {
			rep.Findings = append(rep.Findings, ScanFinding{
				Kind: FindingUnsafePerms, Detail: p.String(), Path: p.Path,
			})
		}
	}

	strayAge := opts.StrayAge
	if strayAge <= 0 {
		strayAge = time.Hour
	}
	stale, err := fsutil.ListStaleDirs(s.home.TmpDir(), strayAge)
	if err != nil {
		return rep, err
	}
	for _, dir := range stale {
		rep.Findings = append(rep.Findings, ScanFinding{
			Kind: FindingStrayStaging, Path: dir,
			Detail: "abandoned staging directory (interrupted admission)",
		})
	}
	return rep, nil
}

// scanObject classifies one object: healthy, malformed, invalid metadata, or
// corrupted.
func (s *Store) scanObject(key string, usedBy func(string) []string) (ScanFinding, bool) {
	if _, err := os.Stat(s.ContentPath(key)); err != nil {
		return ScanFinding{
			Kind: FindingMalformed, Key: key, Path: s.ObjectPath(key),
			Detail: "object directory has no content/",
		}, false
	}
	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		return ScanFinding{
			Kind: FindingInvalidMetadata, Key: key, Path: s.MetadataPath(key),
			Detail: err.Error(), UsedBy: usedBy(key),
		}, false
	}
	hashes, err := integrity.HashDir(s.ContentPath(key))
	if err != nil {
		return ScanFinding{
			Kind: FindingMalformed, Key: key, Path: s.ContentPath(key),
			Detail: fmt.Sprintf("content unreadable: %v", err),
		}, false
	}
	if hashes.ContentHash != key || meta.ContentHash != key {
		return ScanFinding{
			Kind: FindingCorrupted, Key: key, Path: s.ObjectPath(key),
			Detail:   "content does not match its recorded identity",
			Expected: key, Actual: hashes.ContentHash,
			UsedBy: usedBy(key),
		}, false
	}
	return ScanFinding{}, true
}
