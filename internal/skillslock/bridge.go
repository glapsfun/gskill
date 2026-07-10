package skillslock

import (
	"path"
	"strings"

	"github.com/glapsfun/gskill/internal/integrity"
)

// ExtState carries the residual gskill install state that has no place in the
// interop-visible Ext fields but is still consumed by existing commands
// (update skip logic, target removal, drift checks, display metadata). It
// lives nested under gskill.state so the top of the extension block stays the
// small, documented interop surface.
type ExtState struct {
	// SourceKind is gskill's resolved source type (e.g. a "local" entry whose
	// path is a git repo resolves as "git"); the entry's core sourceType stays
	// whatever the external tool wrote.
	SourceKind     string `json:"sourceKind,omitempty"`
	SourceOriginal string `json:"sourceOriginal,omitempty"`
	SourceOwner    string `json:"sourceOwner,omitempty"`
	SourceRepo     string `json:"sourceRepo,omitempty"`
	SourcePath     string `json:"sourcePath,omitempty"`

	RequestedVersion string `json:"requestedVersion,omitempty"`
	RequestedRef     string `json:"requestedRef,omitempty"`
	RequestedCommit  string `json:"requestedCommit,omitempty"`

	RefKind       string `json:"refKind,omitempty"`
	Tag           string `json:"tag,omitempty"`
	Branch        string `json:"branch,omitempty"`
	TreeHash      string `json:"treeHash,omitempty"`
	MutableRef    bool   `json:"mutableRef,omitempty"`
	LocalPathHash string `json:"localPathHash,omitempty"`

	MetaName        string `json:"metaName,omitempty"`
	MetaDescription string `json:"metaDescription,omitempty"`
	MetaVersion     string `json:"metaVersion,omitempty"`
	MetaLicense     string `json:"metaLicense,omitempty"`

	RequiresSkills      []string `json:"requiresSkills,omitempty"`
	RequiresCommands    []string `json:"requiresCommands,omitempty"`
	RequiresEnvironment []string `json:"requiresEnvironment,omitempty"`
	RequiresMCP         []string `json:"requiresMcp,omitempty"`

	ActivePath string            `json:"activePath,omitempty"`
	Targets    map[string]string `json:"targets,omitempty"`
	Modes      map[string]string `json:"modes,omitempty"`

	Trust string `json:"trust,omitempty"`
}

// FromRecord maps an in-memory record into a shared-format entry: core fields
// stay npx-skills-compatible, everything gskill-specific goes under the
// namespaced extension.
func FromRecord(ls Record) Entry {
	src := ls.Source.Original
	if ls.Source.Type == "github" && ls.Source.Owner != "" && ls.Source.Repo != "" {
		src = ls.Source.Owner + "/" + ls.Source.Repo
	}
	skillPath := integrity.SkillFileName
	if ls.Source.Path != "" {
		skillPath = path.Join(ls.Source.Path, integrity.SkillFileName)
	}

	ref := ls.Resolved.Tag
	if ref == "" {
		ref = ls.Resolved.Branch
	}

	ext := &Ext{
		SourceURL:     ls.Source.URL,
		Ref:           ref,
		Commit:        ls.Resolved.Commit,
		Version:       ls.Resolved.Version,
		Agents:        ls.Installation.Agents,
		InstallMode:   ls.Installation.Mode,
		Scope:         ls.Installation.Scope,
		StoreHash:     ls.Resolved.ContentHash,
		SkillFileHash: ls.Resolved.SkillFileHash,
		InstalledAt:   ls.Provenance.FetchedAt,
		UpdatedAt:     ls.Provenance.UpdatedAt,
		State: &ExtState{
			SourceKind:          ls.Source.Type,
			SourceOriginal:      ls.Source.Original,
			SourceOwner:         ls.Source.Owner,
			SourceRepo:          ls.Source.Repo,
			SourcePath:          ls.Source.Path,
			RequestedVersion:    ls.Requested.Version,
			RequestedRef:        ls.Requested.Ref,
			RequestedCommit:     ls.Requested.Commit,
			RefKind:             ls.Resolved.RefKind,
			Tag:                 ls.Resolved.Tag,
			Branch:              ls.Resolved.Branch,
			TreeHash:            ls.Resolved.TreeHash,
			MutableRef:          ls.Resolved.MutableRef,
			LocalPathHash:       ls.Resolved.LocalPathHash,
			MetaName:            ls.Metadata.Name,
			MetaDescription:     ls.Metadata.Description,
			MetaVersion:         ls.Metadata.Version,
			MetaLicense:         ls.Metadata.License,
			RequiresSkills:      ls.Requires.Skills,
			RequiresCommands:    ls.Requires.Commands,
			RequiresEnvironment: ls.Requires.Environment,
			RequiresMCP:         ls.Requires.MCP,
			ActivePath:          ls.Installation.ActivePath,
			Targets:             ls.Installation.Targets,
			Modes:               ls.Installation.Modes,
			Trust:               ls.Provenance.Trust,
		},
	}
	return Entry{
		Source:       src,
		Ref:          ls.Requested.Ref,
		SourceType:   ls.Source.Type,
		SkillPath:    skillPath,
		ComputedHash: ls.Resolved.CompatHash,
		Ext:          ext,
	}
}

// ToRecord reconstructs the in-memory record from a shared-format
// entry. name is the map key, used as the display-name fallback for entries
// that never carried gskill state (external-only entries).
func ToRecord(name string, e Entry) Record {
	ext := e.Ext
	if ext == nil {
		ext = &Ext{}
	}
	st := ext.State
	if st == nil {
		st = &ExtState{}
	}

	original := st.SourceOriginal
	if original == "" {
		original = e.Source
	}
	owner, repo := st.SourceOwner, st.SourceRepo
	if owner == "" && repo == "" && e.SourceType == "github" {
		if i := strings.Index(e.Source, "/"); i > 0 {
			owner, repo = e.Source[:i], e.Source[i+1:]
		}
	}
	srcPath := st.SourcePath
	if srcPath == "" && e.SkillPath != "" {
		if d := path.Dir(e.SkillPath); d != "." {
			srcPath = d
		}
	}

	metaName := st.MetaName
	if metaName == "" {
		metaName = name
	}
	srcType := st.SourceKind
	if srcType == "" {
		srcType = e.SourceType
	}

	return Record{
		Source: Source{
			Type:     srcType,
			Original: original,
			URL:      ext.SourceURL,
			Owner:    owner,
			Repo:     repo,
			Path:     srcPath,
		},
		Requested: Requested{
			Version: st.RequestedVersion, Ref: st.RequestedRef, Commit: st.RequestedCommit,
		},
		Resolved: Resolved{
			Version:       ext.Version,
			RefKind:       st.RefKind,
			Tag:           st.Tag,
			Branch:        st.Branch,
			Commit:        ext.Commit,
			TreeHash:      st.TreeHash,
			ContentHash:   ext.StoreHash,
			SkillFileHash: ext.SkillFileHash,
			MutableRef:    st.MutableRef,
			LocalPathHash: st.LocalPathHash,
			CompatHash:    e.ComputedHash,
		},
		Metadata: Metadata{
			Name: metaName, Description: st.MetaDescription,
			Version: st.MetaVersion, License: st.MetaLicense,
		},
		Requires: Requires{
			Skills: st.RequiresSkills, Commands: st.RequiresCommands,
			Environment: st.RequiresEnvironment, MCP: st.RequiresMCP,
		},
		Installation: Installation{
			Scope: ext.Scope, Mode: ext.InstallMode, Agents: ext.Agents,
			ActivePath: st.ActivePath, Targets: st.Targets, Modes: st.Modes,
		},
		Provenance: Provenance{
			FetchedAt: ext.InstalledAt, UpdatedAt: ext.UpdatedAt, Trust: st.Trust,
		},
	}
}
