package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/glapsfun/gskill/internal/errs"
)

// The only identity a release manifest may be signed with: the release
// workflow of this repository at the release's own tag. npm-release.yml checks
// the same identity with cosign before publishing.
const (
	releaseIssuer    = "https://token.actions.githubusercontent.com"
	releaseSANPrefix = "https://github.com/glapsfun/gskill/.github/workflows/release.yml@refs/tags/"
)

// SigstoreVerifier checks release manifests against the Sigstore public-good
// instance, whose trusted root it refreshes through TUF.
type SigstoreVerifier struct {
	cacheDir string
	noCache  bool
}

// NewSigstoreVerifier returns a verifier that caches TUF metadata under
// cacheDir/sigstore-tuf, or keeps it in memory when noCache is set or
// cacheDir is empty.
func NewSigstoreVerifier(cacheDir string, noCache bool) *SigstoreVerifier {
	return &SigstoreVerifier{cacheDir: cacheDir, noCache: noCache}
}

// VerifyManifest authenticates manifest against its bundle for tag.
func (v *SigstoreVerifier) VerifyManifest(_ context.Context, manifest, bundleJSON []byte, tag string) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return signatureError(tag, err)
	}
	// The trusted root comes through TUF rather than being embedded, so an
	// old gskill keeps verifying new releases after Fulcio or Rekor keys rotate.
	trusted, err := root.FetchTrustedRootWithOptions(tufOptions(v.cacheDir, v.noCache))
	if err != nil {
		return errs.Wrap(errs.CodeSourceUnavailable, "fetch the Sigstore trusted root", err)
	}
	return verifyEntity(trusted, &b, manifest, tag)
}

func tufOptions(cacheDir string, noCache bool) *tuf.Options {
	opts := tuf.DefaultOptions()
	if cacheDir == "" || noCache {
		opts.DisableLocalCache = true
		return opts
	}
	return opts.WithCachePath(filepath.Join(cacheDir, "sigstore-tuf"))
}

func verifyEntity(trusted root.TrustedMaterial, entity verify.SignedEntity, manifest []byte, tag string) error {
	v, err := verify.NewVerifier(trusted, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return signatureError(tag, err)
	}
	id, err := verify.NewShortCertificateIdentity(releaseIssuer, "", releaseSANPrefix+tag, "")
	if err != nil {
		return signatureError(tag, err)
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(manifest)), verify.WithCertificateIdentity(id))
	if _, err := v.Verify(entity, policy); err != nil {
		return signatureError(tag, err)
	}
	return nil
}

func signatureError(tag string, cause error) error {
	return errs.WithHint(
		errs.Wrap(errs.CodeIntegrity, fmt.Sprintf("checksums.txt for %s is not signed by the gskill release workflow", tag), cause),
		"nothing was changed; report this at https://github.com/glapsfun/gskill/issues")
}
