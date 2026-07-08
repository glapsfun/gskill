package resolver

import (
	"context"
	"sort"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/source"
)

// Version kinds for SourceVersion (spec 011 US3).
const (
	VersionKindRelease = "release" // semver-parseable tag
	VersionKindTag     = "tag"     // non-semver tag
	VersionKindBranch  = "branch"
)

// SourceVersion is one selectable version of a git source.
type SourceVersion struct {
	Kind   string
	Name   string
	Commit string
}

// ListVersions lists a source's selectable versions: release tags in
// descending semver order, then other tags, then branch heads. Listing
// failures are returned to the caller — the app layer decides how to degrade
// (FR-012); the resolver only reports.
func ListVersions(ctx context.Context, runner git.Runner, ref source.Ref) ([]SourceVersion, error) {
	tags, err := runner.LsRemoteTags(ctx, ref.URL)
	if err != nil {
		return nil, err
	}
	heads, err := runner.LsRemoteHeads(ctx, ref.URL)
	if err != nil {
		return nil, err
	}

	type release struct {
		v   *semver.Version
		tag git.TagRef
	}
	var releases []release
	var plain []git.TagRef
	for _, tag := range tags {
		if v, vErr := semver.NewVersion(tag.Name); vErr == nil {
			releases = append(releases, release{v: v, tag: tag})
		} else {
			plain = append(plain, tag)
		}
	}
	sort.SliceStable(releases, func(i, j int) bool { return releases[i].v.GreaterThan(releases[j].v) })

	out := make([]SourceVersion, 0, len(tags)+len(heads))
	for _, r := range releases {
		out = append(out, SourceVersion{Kind: VersionKindRelease, Name: r.tag.Name, Commit: r.tag.Commit})
	}
	for _, tag := range plain {
		out = append(out, SourceVersion{Kind: VersionKindTag, Name: tag.Name, Commit: tag.Commit})
	}
	for _, head := range heads {
		out = append(out, SourceVersion{Kind: VersionKindBranch, Name: head.Name, Commit: head.Commit})
	}
	return out, nil
}
