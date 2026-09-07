package app

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/manifest"
	"github.com/glapsfun/gskill/internal/overrides"
	"github.com/glapsfun/gskill/internal/resolver"
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
	// Intent first, resolution only as a fallback. A skill added with a semver
	// constraint tracks that constraint; writing the tag it resolved to would
	// turn a range into a hard pin and leave `update` nothing to advance.
	switch {
	case r.Requested.Version != "":
		s.Version = r.Requested.Version
	case r.Requested.Ref != "":
		s.Ref = r.Requested.Ref
	case r.Requested.Commit != "":
		s.Commit = r.Requested.Commit
	case r.Resolved.Tag != "":
		s.Ref = r.Resolved.Tag
	case r.Resolved.Branch != "":
		s.Ref = r.Resolved.Branch
	default:
		s.Commit = r.Resolved.Commit
	}
	// Mode is deliberately not derived from r.Installation.Mode: that is the
	// *resolved* mode, so a machine that happens to support symlinks would
	// commit mode = "symlink" as if it were a choice, and every teammate would
	// inherit a decision nobody made. It is written only when the user asked
	// for one explicitly (see syncManifestSkills).
	sort.Strings(s.Agents)
	return s
}

// declarationFor returns the skill's tracking intent as the user declared it:
// the manifest entry when one exists, otherwise the projection a manifest
// would be generated from — the same one ensureManifest writes — so a project
// classifies identically before and after gaining its manifest (spec 024
// FR-019). The bool reports whether a manifest declaration was found.
func (a *App) declarationFor(root, name string, rec skillslock.Record) (manifest.Skill, bool) {
	if m, err := a.loadManifest(root); err == nil && m != nil {
		if decl, ok := m.Skills[name]; ok {
			return decl, true
		}
	}
	return manifestSkillFrom(name, rec), false
}

// resolverDeclaration maps a manifest declaration onto the resolver's view of
// intent.
func resolverDeclaration(decl manifest.Skill, rec skillslock.Record) resolver.Declaration {
	return resolver.Declaration{
		Version: decl.Version,
		Ref:     decl.Ref,
		Commit:  decl.Commit,
		Local:   rec.Resolved.RefKind == string(resolver.RefKindLocal),
	}
}

// intentFromDeclaration builds one skill's install intent from its manifest
// declaration, falling back to the lock record only for facts a declaration
// never carries (scope) or may omit (skill path, mode, agents). It is the
// counterpart of intentFromRecord for projects that have a manifest, and the
// only way `update` and `upgrade` derive what to resolve (spec 024 FR-004).
func intentFromDeclaration(decl manifest.Skill, rec skillslock.Record) skillIntent {
	in := skillIntent{
		Source:  decl.Source,
		Path:    decl.Skill,
		Version: decl.Version,
		Ref:     decl.Ref,
		Commit:  decl.Commit,
		Mode:    decl.Mode,
		Scope:   rec.Installation.Scope,
		Agents:  append([]string(nil), decl.Agents...),
	}
	if in.Source == "" {
		in.Source = rec.Source.Original
	}
	if in.Path == "" {
		in.Path = rec.Source.Path
	}
	if in.Mode == "" {
		in.Mode = rec.Installation.Mode
	}
	if len(in.Agents) == 0 {
		in.Agents = append([]string(nil), rec.Installation.Agents...)
	}
	return in
}

// syncManifestSkills writes the declarations for names into the project's
// manifest, creating it when absent (FR-019). Any override block a user
// authored for a skill is carried over untouched: `add` records how a skill is
// obtained, never how it is customized.
func (a *App) syncManifestSkills(p *project, lf *skillslock.State, names []string, explicitMode string) error {
	path := manifestPath(p.root)
	existing, err := a.loadManifest(p.root)
	if err != nil {
		return err
	}
	for _, name := range names {
		locked, ok := lf.Skills[name]
		if !ok {
			continue
		}
		decl := manifestSkillFrom(name, locked)
		if explicitMode != "" && explicitMode != manifest.ModeAuto {
			decl.Mode = explicitMode
		}
		decl = carryAuthored(decl, existing)
		if err := manifest.Upsert(path, decl); err != nil {
			return err
		}
		a.invalidateManifest(p.root)
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
	a.invalidateManifest(p.root)
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
	m, err := a.loadManifest(root)
	if err != nil || m == nil {
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
func (a *App) persistAdd(p *project, lf *skillslock.State, res AddResult, explicitMode string) error {
	if err := saveLock(p.lockPath, lf); err != nil {
		return err
	}
	return a.syncManifestAfterAdd(p, lf, res, explicitMode)
}

// syncManifestAfterAdd writes declarations for everything an add installed, in
// the same run as the lock entry, so the authored and generated halves can
// never disagree after a successful add (spec 023 FR-019).
func (a *App) syncManifestAfterAdd(p *project, lf *skillslock.State, res AddResult, explicitMode string) error {
	added := make([]string, 0, len(res.Installed))
	for _, s := range res.Installed {
		added = append(added, s.Name)
	}
	return a.syncManifestSkills(p, lf, added, explicitMode)
}

// loadManifest reads, validates, and memoizes the project manifest for the
// current run, surfacing its advisories exactly once.
//
// Memoizing matters: overrideFor is consulted per skill and more than once per
// skill, so an unmemoized read re-parses the whole manifest and re-hashes every
// override input O(N^2) times for an N-skill project. The cache is per run and
// per root, and any write invalidates it, so a user's edit between runs is
// always seen.
func (a *App) loadManifest(root string) (*manifest.Manifest, error) {
	a.manifestMu.Lock()
	defer a.manifestMu.Unlock()

	if cached, ok := a.manifests[root]; ok {
		return cached, nil
	}
	m, err := manifest.Load(manifestPath(root))
	if err != nil {
		return nil, err
	}
	if m != nil {
		if vErr := m.Validate(root); vErr != nil {
			return nil, vErr
		}
	}
	if a.manifests == nil {
		a.manifests = map[string]*manifest.Manifest{}
	}
	a.manifests[root] = m
	return m, nil
}

// invalidateManifest drops the memoized manifest for root after a write, so a
// later read in the same run observes what was just written.
func (a *App) invalidateManifest(root string) {
	a.manifestMu.Lock()
	defer a.manifestMu.Unlock()
	delete(a.manifests, root)
}

// manifestWarnings returns the manifest's non-fatal advisories (V3 unknown
// configuration key, V9 ref and commit both declared) so a command can put
// them in front of the user. They are computed during parsing, and a warning
// nobody sees is the same as no warning at all.
func (a *App) manifestWarnings(root string) []string {
	m, err := a.loadManifest(root)
	if err != nil || m == nil {
		return nil
	}
	return append([]string(nil), m.Warnings...)
}

// carryAuthored preserves the parts of an existing declaration that no lock
// entry can reconstruct: the override block, and a mode the user wrote by
// hand. `add` records how a skill is obtained, never how it is customized, so
// re-running it must not quietly discard either.
func carryAuthored(decl manifest.Skill, existing *manifest.Manifest) manifest.Skill {
	if existing == nil {
		return decl
	}
	prior, had := existing.Skills[decl.Name]
	if !had {
		return decl
	}
	if prior.Override != nil {
		decl.Override = prior.Override
	}
	if decl.Mode == "" {
		decl.Mode = prior.Mode
	}
	return decl
}
