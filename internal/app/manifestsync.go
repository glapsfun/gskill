package app

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/overrides"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// manifestPath is the project's authored declaration file (spec 023).
func manifestPath(root string) string { return filepath.Join(root, manifest.FileName) }

// manifestSkillFrom projects a lock record onto its manifest declaration,
// using the field mapping in contracts/manifest.md. It is the single place
// that mapping lives, so `add` and migration generation cannot drift apart.
//
// Overrides are never derived here: they are authored intent that no lock
// entry can reconstruct, so an existing declaration's override block is
// preserved by the caller rather than regenerated.
func manifestSkillFrom(name string, r skillslock.Record) manifest.Skill {
	s := manifest.Skill{
		Name:   name,
		Source: r.Source.Original,
		Agents: append([]string(nil), r.Installation.Agents...),
	}
	if s.Source == "" {
		s.Source = r.Source.URL
	}
	// `skill` is omitted when it matches the installed name, which is the
	// common case and keeps generated manifests terse.
	if r.Source.Path != "" && r.Source.Path != name {
		s.Skill = r.Source.Path
	}
	switch {
	case r.Requested.Ref != "":
		s.Ref = r.Requested.Ref
	case r.Resolved.Tag != "":
		s.Ref = r.Resolved.Tag
	case r.Resolved.Branch != "":
		s.Ref = r.Resolved.Branch
	default:
		s.Commit = r.Resolved.Commit
	}
	if m := r.Installation.Mode; m != "" && m != manifest.ModeAuto {
		s.Mode = m
	}
	sort.Strings(s.Agents)
	return s
}

// syncManifestSkills writes the declarations for names into the project's
// manifest, creating it when absent (FR-019). Any override block a user
// authored for a skill is carried over untouched: `add` records how a skill is
// obtained, never how it is customized.
func (a *App) syncManifestSkills(p *project, lf *skillslock.State, names []string) error {
	path := manifestPath(p.root)
	existing, err := manifest.Load(path)
	if err != nil {
		return err
	}
	for _, name := range names {
		locked, ok := lf.Skills[name]
		if !ok {
			continue
		}
		decl := manifestSkillFrom(name, locked)
		if existing != nil {
			if prior, had := existing.Skills[name]; had && prior.Override != nil {
				decl.Override = prior.Override
			}
		}
		if err := manifest.Upsert(path, decl); err != nil {
			return err
		}
	}
	return nil
}

// dropManifestSkills deletes declarations for names (FR-020). A manifest left
// with no declarations at all is removed, so `remove`-ing the last skill
// returns the project to the state it started in rather than leaving an empty
// file behind.
func (a *App) dropManifestSkills(p *project, names []string) error {
	path := manifestPath(p.root)
	for _, name := range names {
		if err := manifest.Remove(path, name); err != nil {
			return err
		}
	}
	m, err := manifest.Load(path)
	if err != nil || m == nil {
		return err
	}
	if len(m.Skills) == 0 && len(m.Config) == 0 {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
	}
	return nil
}

// overrideFor resolves the override declaration for one skill and its canonical
// identity (spec 023 FR-007). A project with no manifest, or a skill with no
// override, yields an empty spec and an empty digest — which is exactly how a
// spec 022 entry behaves, so nothing changes for projects that declare none.
//
// Validation runs here, before any fetch, so a malformed declaration costs
// nothing (FR-005).
func (a *App) overrideFor(root, name string) (overrides.Spec, string, error) {
	m, err := manifest.Load(manifestPath(root))
	if err != nil || m == nil {
		return overrides.Spec{}, "", err
	}
	if err := m.Validate(root); err != nil {
		return overrides.Spec{}, "", err
	}
	decl := m.Skills[name].Override
	if decl.Empty() {
		return overrides.Spec{}, "", nil
	}

	spec := overrides.Spec{
		Replace: decl.Replace,
		Patch:   decl.Patch,
		Prepend: decl.Prepend,
		Append:  decl.Append,
	}
	digest, err := integrity.OverrideDigest(root, integrity.OverrideSpec(spec))
	if err != nil {
		return overrides.Spec{}, "", err
	}
	return spec, digest, nil
}

// recordOverride stamps a resolved declaration onto a lock record so the
// lockfile stays self-describing about what was applied (plan.md Deviation 1).
// An empty declaration clears the fields rather than leaving stale ones behind,
// so removing an override cannot leave a phantom identity in the lock.
func recordOverride(r *skillslock.Resolved, root string, spec overrides.Spec, baseHash string) error {
	if spec.Empty() {
		// With nothing overridden the base *is* the content, so recording it
		// twice would grow every spec 022 entry with a redundant field that
		// FR-008 says is omitted.
		r.BaseHash = ""
		r.OverrideDigest = ""
		r.Override = nil
		return nil
	}
	// An install served from committed content never sees the upstream
	// extract and so reports no base hash; leave whatever the record already
	// carries rather than stamping an empty one over it.
	if baseHash != "" {
		r.BaseHash = baseHash
	}
	digest, err := integrity.OverrideDigest(root, integrity.OverrideSpec(spec))
	if err != nil {
		return err
	}
	r.OverrideDigest = digest
	r.Override = &skillslock.OverrideDecl{
		Replace: spec.Replace,
		Patch:   spec.Patch,
		Prepend: spec.Prepend,
		Append:  spec.Append,
	}
	return nil
}

// overrideMatchesLock reports whether the declared override still agrees with
// what the lock recorded. A mismatch is what makes install re-resolve just that
// entry instead of trusting the lock (FR-012).
func overrideMatchesLock(r skillslock.Record, digest string) bool {
	return r.Resolved.OverrideDigest == digest
}

// persistAdd writes both halves of the record in one step: the generated lock
// and the authored declarations. They are written together because a failure
// between them would leave the two disagreeing (spec 023 FR-019).
func (a *App) persistAdd(p *project, lf *skillslock.State, res AddResult) error {
	if err := saveLock(p.lockPath, lf); err != nil {
		return err
	}
	return a.syncManifestAfterAdd(p, lf, res)
}

// syncManifestAfterAdd writes declarations for everything an add installed, in
// the same run as the lock entry, so the authored and generated halves can
// never disagree after a successful add (spec 023 FR-019).
func (a *App) syncManifestAfterAdd(p *project, lf *skillslock.State, res AddResult) error {
	added := make([]string, 0, len(res.Installed))
	for _, s := range res.Installed {
		added = append(added, s.Name)
	}
	return a.syncManifestSkills(p, lf, added)
}
