package resolver

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// DeclarationShape is the kind of tracking intent a manifest declaration
// expresses (spec 024 data-model.md §1). It decides whether a normal update
// may move the skill and how an upgrade rewrites the declaration.
type DeclarationShape string

// Declaration shapes. Values are stable: they appear in JSON output and in
// the lockfile's gskill extension.
const (
	ShapeRangeCaret   DeclarationShape = "range-caret"
	ShapeRangeTilde   DeclarationShape = "range-tilde"
	ShapeRangeOther   DeclarationShape = "range-other"
	ShapeExactVersion DeclarationShape = "exact-version"
	ShapeTag          DeclarationShape = "tag"
	ShapeBranch       DeclarationShape = "branch"
	ShapeCommit       DeclarationShape = "commit"
	ShapeLocal        DeclarationShape = "local"
	ShapeUnpinned     DeclarationShape = "unpinned"
)

// Pinned reports whether the shape fixes one revision, so only a change of
// intent can move it.
func (s DeclarationShape) Pinned() bool {
	return s == ShapeExactVersion || s == ShapeTag || s == ShapeCommit
}

// Floating reports whether a normal update may move the skill within the
// declaration.
func (s DeclarationShape) Floating() bool {
	switch s {
	case ShapeRangeCaret, ShapeRangeTilde, ShapeRangeOther, ShapeBranch:
		return true
	case ShapeExactVersion, ShapeTag, ShapeCommit, ShapeLocal, ShapeUnpinned:
		return false
	default:
		return false
	}
}

// Declaration is a skill's tracking intent as the user declared it: at most
// one of Version, Ref, and Commit drives resolution, with Commit winning over
// Version and Version over Ref, matching the resolver's own precedence.
type Declaration struct {
	Version string
	Ref     string
	Commit  string
	Local   bool
}

// bareVersion matches a full MAJOR.MINOR.PATCH with optional pre-release and
// build metadata; an optional leading "v" or "=" is tolerated because both
// are common in hand-written manifests and mean the same exact version.
var bareVersion = regexp.MustCompile(`^[=v]?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// ClassifyDeclaration derives the shape of a declaration. refKindHint is the
// resolved kind recorded for the skill's ref, when known: whether a ref is a
// tag or a branch is decided at resolution time, and without a hint a ref is
// treated as mutable so nothing is ever wrongly reported as pinned.
func ClassifyDeclaration(d Declaration, refKindHint RefKind) (DeclarationShape, error) {
	switch {
	case d.Local:
		return ShapeLocal, nil
	case d.Commit != "":
		return ShapeCommit, nil
	case d.Version != "":
		return classifyConstraint(d.Version)
	case d.Ref != "":
		if refKindHint == RefKindTag {
			return ShapeTag, nil
		}
		return ShapeBranch, nil
	default:
		return ShapeUnpinned, nil
	}
}

func classifyConstraint(c string) (DeclarationShape, error) {
	trimmed := strings.TrimSpace(c)
	if _, err := semver.NewConstraint(trimmed); err != nil {
		return "", fmt.Errorf("parse constraint %q: %w", c, err)
	}
	switch {
	case bareVersion.MatchString(trimmed):
		return ShapeExactVersion, nil
	case strings.HasPrefix(trimmed, "^") && bareVersion.MatchString(trimmed[1:]):
		return ShapeRangeCaret, nil
	case strings.HasPrefix(trimmed, "~") && bareVersion.MatchString(trimmed[1:]):
		return ShapeRangeTilde, nil
	default:
		return ShapeRangeOther, nil
	}
}

// admitsPrerelease reports whether a declaration names a pre-release itself,
// in which case newer pre-releases are fair candidates for it.
func admitsPrerelease(constraint string) bool {
	v, err := semver.NewVersion(strings.TrimLeft(strings.TrimSpace(constraint), "^~=v"))
	return err == nil && v.Prerelease() != ""
}
