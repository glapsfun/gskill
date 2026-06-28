package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/discovery"
)

// FindScope selects where a search looks. Exactly one of Source/Owner may be
// set; when both are empty the configured repositories are searched. Local
// installed skills are always included.
type FindScope struct {
	Source string
	Owner  string
	Root   string
}

// SearchHit is one search result, attributed to its source and in-repo path.
type SearchHit struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	RepoPath    string `json:"repo_path"`
	Installed   bool   `json:"installed"`
	Score       int    `json:"score"`
}

// Find searches for skills matching query within a source, across a GitHub
// owner, or across the configured repositories — always including locally
// installed skills. Unreachable repositories are reported as warnings rather
// than aborting the search (FR-036..FR-041). It returns hits and warnings.
func (a *App) Find(ctx context.Context, query string, scope FindScope) ([]SearchHit, []string, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	installed := a.installedIndex(scope.Root)

	sources, warnings := a.findSources(ctx, scope)

	var hits []SearchHit
	seen := make(map[string]bool) // source\x00repoPath\x00id (duplicate ids at different paths both surface)
	for _, src := range sources {
		res, err := a.SourceList(ctx, src, ScanOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", src, err))
			continue
		}
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
			})
		}
	}

	rankHits(hits)
	return hits, warnings, nil
}

// findSources resolves the scope to a list of source arguments to scan.
func (a *App) findSources(ctx context.Context, scope FindScope) (sources, warnings []string) {
	switch {
	case scope.Source != "":
		return []string{scope.Source}, nil
	case scope.Owner != "":
		repos, err := a.repos.ListOwnerRepos(ctx, scope.Owner)
		if err != nil {
			return nil, []string{fmt.Sprintf("list repos for %q: %v", scope.Owner, err)}
		}
		for _, r := range repos {
			sources = append(sources, r.CloneURL)
		}
		return sources, nil
	default:
		return a.cfg.Repositories, nil
	}
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
