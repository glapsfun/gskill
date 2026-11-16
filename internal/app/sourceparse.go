package app

import (
	"errors"

	"github.com/glapsfun/gskill/internal/skillslock"
	"github.com/glapsfun/gskill/internal/source"
)

// parseSource parses a source persisted in skills.toml or the lockfile. A
// GitLab path with more than two segments and no boundary marker is ambiguous
// to source.Parse (spec 035 FR-008), but a project that added it before that
// rule keeps the repository and folder its lockfile recorded (FR-015): when
// locked holds the same original spelling and a clone address, the reference
// is rebuilt from the lock instead of refused.
func parseSource(raw string, locked *skillslock.Source) (source.Ref, error) {
	ref, err := source.Parse(raw)
	if errors.Is(err, source.ErrAmbiguousSource) && locked != nil && locked.Original == raw && locked.URL != "" {
		return refFromLock(*locked), nil
	}
	return ref, err
}

// parseProjectSource is parseSource for callers that hold only a source
// string (the dashboard's version picker). The lockfile is read only when the
// source is ambiguous, the one case its recorded boundary can settle.
func parseProjectSource(root, raw string) (source.Ref, error) {
	ref, err := source.Parse(raw)
	if !errors.Is(err, source.ErrAmbiguousSource) {
		return ref, err
	}
	return parseSource(raw, lockedSource(root, raw))
}

// lockedSource returns the lock record of the skill in root whose original
// source is raw, or nil.
func lockedSource(root, raw string) *skillslock.Source {
	if root == "" {
		return nil
	}
	lf, err := loadOrNewLock(openProject(root).lockPath)
	if err != nil {
		return nil
	}
	for _, rec := range lf.Skills {
		if rec.Source.Original == raw {
			src := rec.Source
			return &src
		}
	}
	return nil
}
