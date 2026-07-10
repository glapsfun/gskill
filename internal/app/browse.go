package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/skillslock"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/integrity"
)

// ListedSkill is one row of `gskill list`.
type ListedSkill struct {
	Name    string
	Source  string
	Version string
	Status  string
	Agents  []string
}

// List returns every locked skill with its drift status.
func (a *App) List(_ context.Context, root string) ([]ListedSkill, error) {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return nil, err
	}

	var out []ListedSkill
	for _, name := range sortedKeys(lf.Skills) {
		locked := lf.Skills[name]
		out = append(out, ListedSkill{
			Name:    name,
			Status:  string(classifySkill(root, name, lf)),
			Source:  locked.Source.Original,
			Version: displayVersion(locked.Resolved),
			Agents:  locked.Installation.Agents,
		})
	}
	return out, nil
}

// SkillInfo is the detail shown by `gskill info`.
type SkillInfo struct {
	Name        string
	Source      string
	Version     string
	Commit      string
	ContentHash string
	Description string
	License     string
	Requires    skillslock.Requires
	Agents      []string
	Targets     map[string]string
}

// Info returns the full detail for one locked skill.
func (a *App) Info(_ context.Context, root, name string) (SkillInfo, error) {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return SkillInfo{}, err
	}
	locked, ok := lf.Skills[name]
	if !ok {
		return SkillInfo{}, fmt.Errorf("%w: skill %q is not installed", errs.ErrUsage, name)
	}
	return SkillInfo{
		Name:        name,
		Source:      locked.Source.Original,
		Version:     displayVersion(locked.Resolved),
		Commit:      locked.Resolved.Commit,
		ContentHash: locked.Resolved.ContentHash,
		Description: locked.Metadata.Description,
		License:     locked.Metadata.License,
		Requires:    locked.Requires,
		Agents:      locked.Installation.Agents,
		Targets:     locked.Installation.Targets,
	}, nil
}

// DiffEntry reports how a locked skill differs from disk.
type DiffEntry struct {
	Name   string
	Status string
}

// Diff reports lock/disk drift per skill.
func (a *App) Diff(_ context.Context, root string) ([]DiffEntry, error) {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return nil, err
	}

	var out []DiffEntry
	for _, name := range sortedKeys(lf.Skills) {
		out = append(out, DiffEntry{
			Name:   name,
			Status: string(classifySkill(root, name, lf)),
		})
	}
	return out, nil
}

// SkillMarkdown returns the installed SKILL.md content for a skill, read from
// its first available agent target (for the TUI preview).
func (a *App) SkillMarkdown(_ context.Context, root, name string) (string, error) {
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return "", err
	}
	locked, ok := lf.Skills[name]
	if !ok {
		return "", fmt.Errorf("%w: skill %q is not installed", errs.ErrUsage, name)
	}
	for _, target := range locked.Installation.Targets {
		path := filepath.Join(resolveTarget(root, target), integrity.SkillFileName)
		if data, readErr := os.ReadFile(path); readErr == nil { //nolint:gosec // recorded target path
			return string(data), nil
		}
	}
	return "", fmt.Errorf("%w: no readable SKILL.md for %q", errs.ErrUsage, name)
}

// displayVersion picks the most meaningful version label for a resolution.
func displayVersion(r skillslock.Resolved) string {
	switch {
	case r.Version != "":
		return r.Version
	case r.Tag != "":
		return r.Tag
	case r.Branch != "":
		return r.Branch
	case r.RefKind == "local":
		return "local"
	case r.Commit != "":
		return shortCommit(r.Commit)
	default:
		return "unknown"
	}
}

// shortCommit truncates a commit SHA for display.
func shortCommit(sha string) string { return ShortCommit(sha) }

// ShortCommit abbreviates a commit SHA to the display width every human
// surface uses (plan/dry-run labels, version metadata, progress lines), so
// the same commit never renders at two different lengths in one run.
func ShortCommit(sha string) string {
	const n = 12
	if len(sha) > n {
		return sha[:n]
	}
	return sha
}
