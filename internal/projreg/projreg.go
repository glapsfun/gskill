// Package projreg maintains the advisory per-user project registry under
// <home>/projects (spec 015 FR-027–029). Every entry is rebuilt from the
// project itself whenever gskill touches it, so the registry is never a
// source of truth: deleting it breaks nothing, and reproduction always comes
// from the project's own skills-lock.json.
package projreg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glapsfun/gskill/internal/config"
	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/home"
)

// schemaVersion is the registry entry schema this build reads and writes.
const schemaVersion = 1

// Entry is one registered project (data-model §4). In minimal privacy mode
// Root and Lockfile stay empty; registry files are owner-only and never
// carry credentials.
type Entry struct {
	SchemaVersion int         `json:"schemaVersion"`
	ProjectID     string      `json:"projectId"`
	Root          string      `json:"root,omitempty"`
	Lockfile      string      `json:"lockfile,omitempty"`
	LastSeen      time.Time   `json:"lastSeen"`
	References    []Reference `json:"references"`
}

// Reference is one skill's content identity at last-seen time.
type Reference struct {
	Skill     string `json:"skill"`
	StoreHash string `json:"storeHash"`
}

// entryPath returns the registry file for a project ID.
func entryPath(h *home.Home, projectID string) string {
	return filepath.Join(h.ProjectsDir(), sanitizeID(projectID)+".json")
}

// sanitizeID confines a project ID to a safe file-name alphabet.
func sanitizeID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
}

// Write registers or refreshes a project entry per the privacy mode:
// "full" records everything, "minimal" drops the absolute paths, and
// "disabled" writes nothing and removes any existing entry (FR-029). The
// caller holds the registry lock.
func Write(h *home.Home, e Entry, privacy string) error {
	path := entryPath(h, e.ProjectID)
	if privacy == config.PrivacyDisabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove registry entry: %w", err)
		}
		return nil
	}
	e.SchemaVersion = schemaVersion
	if privacy == config.PrivacyMinimal {
		e.Root = ""
		e.Lockfile = ""
	}
	sort.Slice(e.References, func(i, j int) bool { return e.References[i].Skill < e.References[j].Skill })
	if e.References == nil {
		e.References = []Reference{}
	}
	return fsutil.WriteJSONAtomic(path, e, home.FilePerm())
}

// List returns every registry entry, sorted by project ID. Unreadable or
// foreign files are skipped — the registry is advisory.
func List(h *home.Home) ([]Entry, error) {
	files, err := os.ReadDir(h.ProjectsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var out []Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		e, err := readEntry(filepath.Join(h.ProjectsDir(), f.Name()))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectID < out[j].ProjectID })
	return out, nil
}

// Get returns the entry for projectID.
func Get(h *home.Home, projectID string) (Entry, bool, error) {
	e, err := readEntry(entryPath(h, projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, false, nil
		}
		return Entry{}, false, err
	}
	return e, true, nil
}

// readEntry loads and validates one registry file.
func readEntry(path string) (Entry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // registry-internal path
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("parse registry entry %s: %w", path, err)
	}
	if e.SchemaVersion != schemaVersion {
		return Entry{}, fmt.Errorf("registry entry %s has schema version %d; this gskill understands %d",
			path, e.SchemaVersion, schemaVersion)
	}
	return e, nil
}

// Prune removes entries whose recorded root no longer exists or is no longer
// a gskill project (no lockfile). It removes registry files only — never
// repository content (FR-028). Minimal-mode entries (no recorded root) are
// kept: their liveness cannot be judged. The caller holds the registry lock.
func Prune(h *home.Home) (removed []string, err error) {
	entries, err := List(h)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Root == "" {
			continue
		}
		if _, statErr := os.Stat(e.Lockfile); statErr == nil {
			continue
		}
		if rmErr := os.Remove(entryPath(h, e.ProjectID)); rmErr != nil && !os.IsNotExist(rmErr) {
			return removed, fmt.Errorf("prune %s: %w", e.ProjectID, rmErr)
		}
		removed = append(removed, e.ProjectID)
	}
	return removed, nil
}
