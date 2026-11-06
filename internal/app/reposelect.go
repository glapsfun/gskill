package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// repoFor returns the repository a skill is grouped under and the skill's
// path inside it (spec 032 FR-001, FR-004). The source is the one an update
// would fetch from: the declaration when it parses, else the lock entry. It is
// read before promoteLocalGit, so a local git repository stays a local group.
func repoFor(root string, decl manifest.Skill, rec skillslock.Record) (source.Repo, string) {
	raw := effectiveSourceLabel(decl, rec.Source)
	if raw == "" {
		raw = rec.Source.URL
	}
	ref, err := source.Parse(raw)
	if err != nil {
		return source.Repo{Kind: source.RepoInvalid, Key: raw, Label: raw}, ""
	}
	path := decl.Skill
	if path == "" {
		path = ref.Path
	}
	if path == decl.Name {
		path = ""
	}
	return source.RepoOf(ref, root), path
}

// selectSkills resolves which tracked skills a plan or an update processes
// (spec 032 FR-020 to FR-026): every tracked skill, the named ones, or the
// ones from the --repo groups, narrowed to any names. It reads only the lock
// and the manifest, so a bad selector fails before any network call and
// before anything is written. Errors are usage errors (exit 2), except an
// untracked name, which keeps the error it has without --repo.
func selectSkills(root string, lf *skillslock.State, m *manifest.Manifest, repos, names []string) ([]string, error) {
	if len(repos) == 0 {
		return updateNames(lf, names)
	}
	selected, err := parseRepoSelectors(root, repos)
	if err != nil {
		return nil, err
	}
	all := sortedKeys(lf.Skills)
	tracked := make([]source.Repo, len(all))
	for i, name := range all {
		rec := lf.Skills[name]
		decl := manifestSkillFrom(name, rec)
		if m != nil {
			if d, ok := m.Skills[name]; ok {
				decl = d
			}
		}
		tracked[i], _ = repoFor(root, decl, rec)
	}
	groups := groupOrder(tracked)
	if err := requireTracked(repos, selected, groups); err != nil {
		return nil, err
	}

	if len(names) > 0 {
		named, err := updateNames(lf, names)
		if err != nil {
			return nil, err
		}
		for _, name := range named {
			repo := tracked[sort.SearchStrings(all, name)]
			if indexOfRepo(selected, repo) < 0 {
				label := groups[indexOfRepo(groups, repo)].Label
				return nil, errs.New(errs.CodeUsage,
					fmt.Sprintf("skill %q comes from %s, not from the selected repository", name, label))
			}
		}
		return named, nil
	}
	var out []string
	for i, name := range all {
		if indexOfRepo(selected, tracked[i]) >= 0 {
			out = append(out, name)
		}
	}
	return out, nil
}

// parseRepoSelectors parses every --repo value, failing on the first that
// names a path inside a repository or does not parse.
func parseRepoSelectors(root string, repos []string) ([]source.Repo, error) {
	selected := make([]source.Repo, 0, len(repos))
	for _, v := range repos {
		r, err := source.ParseRepo(v, root)
		var pathErr *source.RepoPathError
		switch {
		case errors.As(err, &pathErr):
			return nil, errs.WithHint(errs.New(errs.CodeUsage, pathErr.Error()),
				"use --repo "+pathErr.Repo+" and select skills by name")
		case err != nil:
			return nil, errs.New(errs.CodeUsage, fmt.Sprintf("--repo %q: %v", v, err))
		}
		selected = append(selected, r)
	}
	return selected, nil
}

// requireTracked fails on the first --repo value that matches no tracked
// skill, listing the project's repositories. It never falls back to
// updating the whole project (spec 032 FR-024).
func requireTracked(repos []string, selected, groups []source.Repo) error {
	for i, r := range selected {
		if indexOfRepo(groups, r) >= 0 {
			continue
		}
		labels := make([]string, len(groups))
		for j, g := range groups {
			labels[j] = g.Label
		}
		return errs.WithHint(
			errs.New(errs.CodeUsage, fmt.Sprintf("no tracked skill comes from %q", repos[i])),
			"repositories in this project: "+strings.Join(labels, ", "))
	}
	return nil
}

// manifestOrNil loads root's skills.toml for selection, treating an
// unreadable one like an absent one, as declarationFor does.
func (a *App) manifestOrNil(root string) *manifest.Manifest {
	m, err := a.loadManifest(root)
	if err != nil {
		return nil
	}
	return m
}

// groupOrder returns the distinct repositories of repos in display order:
// by lowercased label, then key. A repository's label is the one its first
// entry carries, and entries arrive name-sorted, so the order is stable.
func groupOrder(repos []source.Repo) []source.Repo {
	var out []source.Repo
	for _, r := range repos {
		if indexOfRepo(out, r) < 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Label), strings.ToLower(out[j].Label)
		if li != lj {
			return li < lj
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// indexOfRepo returns the index of the repository equal to r, or -1.
func indexOfRepo(repos []source.Repo, r source.Repo) int {
	for i, o := range repos {
		if o.Equal(r) {
			return i
		}
	}
	return -1
}
