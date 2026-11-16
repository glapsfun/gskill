package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/discovery"
	"github.com/glapsfun/gskill/internal/registry"
	"github.com/glapsfun/gskill/internal/source"
)

// FindScope selects where a search looks. Exactly one of Source/Owner may be
// set; when both are empty the configured repositories are searched. Local
// installed skills are always included. Transport picks the clone address of
// repositories found through Owner ("" means HTTPS).
type FindScope struct {
	Source    string
	Owner     string
	Root      string
	Transport string
}

// SearchHit is one search result, attributed to its source and in-repo path.
// Source is the string to pass to `gskill add`; Provider and Repository name
// where the skill lives (spec 035 FR-030).
type SearchHit struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	RepoPath    string `json:"repo_path"`
	Installed   bool   `json:"installed"`
	Score       int    `json:"score"`
	Provider    string `json:"provider"`
	Repository  string `json:"repository"`
}

// Find searches for skills matching query within a source, across a provider
// scope, or across the configured repositories, always including locally
// installed skills. Unreachable repositories are reported as warnings rather
// than aborting the search (FR-036..FR-041), but a scope that cannot be listed
// fails the search: an empty result would read as a real answer. It returns
// hits and warnings.
func (a *App) Find(ctx context.Context, query string, scope FindScope) ([]SearchHit, []string, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	installed := a.installedIndex(scope.Root)

	sources, warnings, err := a.findSources(ctx, scope)
	if err != nil {
		return nil, warnings, err
	}

	var hits []SearchHit
	seen := make(map[string]bool) // source\x00repoPath\x00id (duplicate ids at different paths both surface)
	for _, src := range sources {
		res, err := a.SourceList(ctx, src, ScanOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", source.Redact(src), err))
			continue
		}
		provider, repository := hitRepository(src, scope.Root)
		for _, s := range res.Skills {
			score := matchScore(q, s)
			if score == 0 {
				continue
			}
			key := src + "\x00" + s.RepoPath + "\x00" + s.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			hits = append(hits, SearchHit{
				ID: s.ID, DisplayName: s.DisplayName, Description: s.Description,
				Source: src, RepoPath: s.RepoPath, Installed: installed[s.ID], Score: score,
				Provider: provider, Repository: repository,
			})
		}
	}

	rankHits(hits)
	return hits, warnings, nil
}

// hitRepository names the provider and repository a scanned source belongs
// to: github, gitlab, bitbucket, or git for a remote, local for a directory.
func hitRepository(src, root string) (provider, repository string) {
	ref, err := source.Parse(src)
	if err != nil {
		return "", source.Redact(src)
	}
	repo := source.RepoOf(ref, root)
	if ref.Type == source.TypeLocal || repo.Kind == source.RepoLocal {
		return "local", repo.Label
	}
	return string(ref.Provider()), repo.Label
}

// findSources resolves the scope to a list of source arguments to scan.
func (a *App) findSources(ctx context.Context, scope FindScope) (sources, warnings []string, err error) {
	switch {
	case scope.Source != "":
		return []string{scope.Source}, nil, nil
	case scope.Owner != "":
		s, err := registry.ParseScope(scope.Owner)
		if err != nil {
			return nil, nil, err
		}
		if s.Transport, err = registry.ParseTransport(scope.Transport); err != nil {
			return nil, nil, err
		}
		listing, err := a.repos.ListRepos(ctx, s)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range listing.Repos {
			sources = append(sources, r.CloneURL(s.Transport))
		}
		return sources, listing.Warnings, nil
	default:
		// Project configuration participates here too (FR-002): a repository
		// list declared in the manifest travels with the repo, so a teammate
		// searching the same project sees the same sources with no local
		// setup. Without this, [config] repositories would parse and then be
		// silently ignored.
		if cfg, err := a.projectConfig(scope.Root); err == nil && cfg != nil && len(cfg.Repositories) > 0 {
			return repositorySources(cfg.Repositories), nil, nil
		}
		return repositorySources(a.cfg.Repositories), nil, nil
	}
}

// repositorySources reads configured repositories the way --repo reads its
// values: an entry names a repository, never a folder, so an unmarked nested
// GitLab path is the whole repository rather than an ambiguity.
func repositorySources(repos []string) []string {
	out := make([]string, 0, len(repos))
	for _, raw := range repos {
		if _, err := source.Parse(raw); errors.Is(err, source.ErrAmbiguousSource) {
			raw = strings.TrimRight(strings.TrimSpace(raw), "/") + ".git"
		}
		out = append(out, raw)
	}
	return out
}

// installedIndex maps installed skill ids to true, read from the project lockfile.
func (a *App) installedIndex(root string) map[string]bool {
	out := make(map[string]bool)
	if root == "" {
		return out
	}
	p := openProject(root)
	lf, err := loadOrNewLock(p.lockPath)
	if err != nil {
		return out
	}
	for name := range lf.Skills {
		out[name] = true
	}
	return out
}

// matchScore ranks a skill against a lowercased query: id exact (3), id/name
// substring (2), description substring (1), no match (0). An empty query matches
// everything at the lowest score.
func matchScore(q string, s discovery.DiscoveredSkill) int {
	if q == "" {
		return 1
	}
	id := strings.ToLower(s.ID)
	switch {
	case id == q:
		return 3
	case strings.Contains(id, q) || strings.Contains(strings.ToLower(s.DisplayName), q):
		return 2
	case strings.Contains(strings.ToLower(s.Description), q):
		return 1
	default:
		return 0
	}
}

// rankHits orders hits by score (desc), then source, then id, deterministically.
func rankHits(hits []SearchHit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Source != hits[j].Source {
			return hits[i].Source < hits[j].Source
		}
		return hits[i].ID < hits[j].ID
	})
}
