package selection

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/glapsfun/gskill/internal/discovery"
)

// Sentinel selection errors. Callers map these to exit codes at the CLI boundary
// (ErrAmbiguousSelection/NoMatchError → usage/2, ErrInvalidSelection → 3).
var (
	// ErrAmbiguousSelection is returned when a selector cannot be resolved
	// without guessing — a duplicated id with no disambiguating path, or a
	// wildcard over a source that still contains duplicates.
	ErrAmbiguousSelection = errors.New("ambiguous skill selection")
	// ErrInvalidSelection is returned when an explicitly selected skill is invalid.
	ErrInvalidSelection = errors.New("selected skill is invalid")
)

// NoMatchError is returned when a name selector matches no discovered skill; it
// carries the closest candidate names as suggestions (FR-017).
type NoMatchError struct {
	Name        string
	Suggestions []string
}

func (e *NoMatchError) Error() string {
	if len(e.Suggestions) > 0 {
		return fmt.Sprintf("no skill named %q (did you mean: %s?)", e.Name, strings.Join(e.Suggestions, ", "))
	}
	return fmt.Sprintf("no skill named %q", e.Name)
}

// Selector is a parsed user selection. Implementations: allSelector,
// nameSelector, pathSelector.
type Selector interface{ isSelector() }

type (
	allSelector  struct{}
	nameSelector struct{ name string }
	pathSelector struct{ name, path string }
)

func (allSelector) isSelector()  {}
func (nameSelector) isSelector() {}
func (pathSelector) isSelector() {}

// Parse turns raw --skill values, the --all flag, and a --path disambiguator
// into selectors. A "*" --skill value or all=true yields the wildcard selector;
// a "name@path" value yields a path-qualified selector.
func Parse(skillFlags []string, all bool, path string) ([]Selector, error) {
	var sels []Selector
	if all {
		sels = append(sels, allSelector{})
	}
	for _, raw := range skillFlags {
		raw = strings.TrimSpace(raw)
		switch {
		case raw == "":
			continue
		case raw == "*":
			sels = append(sels, allSelector{})
		case strings.Contains(raw, "@"):
			name, p, _ := strings.Cut(raw, "@")
			sels = append(sels, pathSelector{name: strings.TrimSpace(name), path: strings.TrimSpace(p)})
		case path != "":
			sels = append(sels, pathSelector{name: raw, path: path})
		default:
			sels = append(sels, nameSelector{name: raw})
		}
	}
	return sels, nil
}

// Resolve maps selectors onto discovered skills, preferring exact matches and
// refusing to guess between duplicates non-interactively. The returned skills
// are de-duplicated and returned in deterministic (RepoPath, ID) order.
func Resolve(res discovery.Result, sels []Selector, interactive bool) ([]discovery.DiscoveredSkill, error) {
	picked := make(map[string]bool) // key: id\x00repoPath
	var out []discovery.DiscoveredSkill
	add := func(s discovery.DiscoveredSkill) {
		key := s.ID + "\x00" + s.RepoPath
		if !picked[key] {
			picked[key] = true
			out = append(out, s)
		}
	}

	for _, sel := range sels {
		matched, err := resolveOne(res, sel, interactive)
		if err != nil {
			return nil, err
		}
		for _, s := range matched {
			add(s)
		}
	}
	sortSkills(out)
	return out, nil
}

// resolveOne resolves a single selector.
func resolveOne(res discovery.Result, sel Selector, interactive bool) ([]discovery.DiscoveredSkill, error) {
	switch s := sel.(type) {
	case allSelector:
		if len(res.Duplicates) > 0 {
			return nil, fmt.Errorf("%w: source contains duplicate skill identities; disambiguate with <name>@<path>", ErrAmbiguousSelection)
		}
		var valid []discovery.DiscoveredSkill
		for _, sk := range res.Skills {
			if sk.Valid {
				valid = append(valid, sk)
			}
		}
		return valid, nil
	case pathSelector:
		return resolvePath(res, s)
	case nameSelector:
		return resolveName(res, s, interactive)
	default:
		return nil, fmt.Errorf("%w: unknown selector", ErrAmbiguousSelection)
	}
}

// resolvePath resolves a path-qualified selector to the skill at that in-repo path.
func resolvePath(res discovery.Result, s pathSelector) ([]discovery.DiscoveredSkill, error) {
	for _, sk := range res.Skills {
		if sk.RepoPath == s.path && (s.name == "" || sk.ID == s.name || discovery.NormalizeID(s.name) == sk.ID) {
			if !sk.Valid {
				return nil, fmt.Errorf("%w: %q at %s", ErrInvalidSelection, sk.ID, sk.RepoPath)
			}
			return []discovery.DiscoveredSkill{sk}, nil
		}
	}
	return nil, &NoMatchError{Name: s.name + "@" + s.path}
}

// resolveName resolves a bare name: exact id, then display-name slug; a
// duplicated id is ambiguous; no match yields suggestions.
func resolveName(res discovery.Result, s nameSelector, interactive bool) ([]discovery.DiscoveredSkill, error) {
	want := discovery.NormalizeID(s.name)
	var matches []discovery.DiscoveredSkill
	for _, sk := range res.Skills {
		if sk.ID == want || sk.ID == s.name || discovery.NormalizeID(sk.DisplayName) == want {
			matches = append(matches, sk)
		}
	}
	switch len(matches) {
	case 1:
		if !matches[0].Valid {
			return nil, fmt.Errorf("%w: %q at %s", ErrInvalidSelection, matches[0].ID, matches[0].RepoPath)
		}
		return matches, nil
	case 0:
		// Fuzzy (substring) matching is offered only interactively, and only
		// when it resolves to exactly one valid skill (FR-019). Non-interactive
		// runs never guess.
		if interactive {
			if fuzzy := substringMatches(res, want); len(fuzzy) == 1 && fuzzy[0].Valid {
				return fuzzy, nil
			}
		}
		return nil, &NoMatchError{Name: s.name, Suggestions: Closest(want, candidateIDs(res), 3)}
	default:
		paths := make([]string, 0, len(matches))
		for _, m := range matches {
			paths = append(paths, m.RepoPath)
		}
		sort.Strings(paths)
		return nil, fmt.Errorf("%w: %q matches %d skills (%s); disambiguate with <name>@<path>",
			ErrAmbiguousSelection, s.name, len(matches), strings.Join(paths, ", "))
	}
}

// substringMatches returns discovered skills whose id contains the wanted
// substring; used only for interactive fuzzy resolution.
func substringMatches(res discovery.Result, want string) []discovery.DiscoveredSkill {
	var out []discovery.DiscoveredSkill
	for _, sk := range res.Skills {
		if strings.Contains(sk.ID, want) {
			out = append(out, sk)
		}
	}
	return out
}

// candidateIDs returns the unique discovered ids for suggestion ranking.
func candidateIDs(res discovery.Result) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, s := range res.Skills {
		if !seen[s.ID] {
			seen[s.ID] = true
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// sortSkills orders skills deterministically by (RepoPath, ID).
func sortSkills(skills []discovery.DiscoveredSkill) {
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].RepoPath != skills[j].RepoPath {
			return skills[i].RepoPath < skills[j].RepoPath
		}
		return skills[i].ID < skills[j].ID
	})
}
