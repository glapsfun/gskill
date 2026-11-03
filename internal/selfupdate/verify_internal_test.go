package selfupdate

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tuf"

	"github.com/glapsfun/gskill/internal/errs"
)

var updateTrustedRoot = flag.Bool("update-trusted-root", false,
	"fetch the current Sigstore trusted root into testdata/v0.9.0 (needs network)")

const fixtureDir = "testdata/v0.9.0"

func TestVerifyRealRelease(t *testing.T) {
	t.Parallel()

	rootPath := filepath.Join(fixtureDir, "trusted_root.json")
	if *updateTrustedRoot {
		fetched, err := root.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithCachePath(t.TempDir()))
		if err != nil {
			t.Fatalf("fetch trusted root: %v", err)
		}
		data, err := fetched.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rootPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	trusted, err := root.NewTrustedRootFromPath(rootPath)
	if err != nil {
		t.Fatalf("load %s (create it with -update-trusted-root): %v", rootPath, err)
	}
	manifest, err := os.ReadFile(filepath.Join(fixtureDir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := os.ReadFile(filepath.Join(fixtureDir, "checksums.txt.sigstore.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		t.Fatal(err)
	}

	if err := verifyEntity(trusted, &b, manifest, "v0.9.0"); err != nil {
		t.Fatalf("real v0.9.0 bundle: %v", err)
	}

	err = verifyEntity(trusted, &b, manifest, "v0.8.0")
	assertSignatureError(t, err, "v0.8.0")
	if err != nil && !strings.Contains(err.Error(), "SAN") {
		t.Errorf("wrong-tag error %q does not mention the SAN", err)
	}

	tampered := bytes.Clone(manifest)
	tampered[0] ^= 1
	assertSignatureError(t, verifyEntity(trusted, &b, tampered, "v0.9.0"), "v0.9.0")
}

func TestVerifyVirtualSigstore(t *testing.T) {
	t.Parallel()

	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte("abc  gskill_1.2.3_linux_amd64.tar.gz\n")

	good, err := vs.Sign(releaseSANPrefix+"v1.2.3", releaseIssuer, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyEntity(vs, good, manifest, "v1.2.3"); err != nil {
		t.Errorf("release workflow signature: %v", err)
	}

	otherWorkflow, err := vs.Sign("https://github.com/glapsfun/gskill/.github/workflows/other.yml@refs/tags/v1.2.3", releaseIssuer, manifest)
	if err != nil {
		t.Fatal(err)
	}
	assertSignatureError(t, verifyEntity(vs, otherWorkflow, manifest, "v1.2.3"), "v1.2.3")

	otherIssuer, err := vs.Sign(releaseSANPrefix+"v1.2.3", "https://accounts.google.com", manifest)
	if err != nil {
		t.Fatal(err)
	}
	assertSignatureError(t, verifyEntity(vs, otherIssuer, manifest, "v1.2.3"), "v1.2.3")
}

func TestTUFOptions(t *testing.T) {
	t.Parallel()

	if o := tufOptions("/c", false); o.CachePath != "/c/sigstore-tuf" || o.DisableLocalCache {
		t.Errorf("tufOptions(/c, false) = cache %q, disabled %v", o.CachePath, o.DisableLocalCache)
	}
	if o := tufOptions("/c", true); !o.DisableLocalCache {
		t.Error("tufOptions(/c, true) keeps the local cache, want it disabled")
	}
	if o := tufOptions("", false); !o.DisableLocalCache {
		t.Error("tufOptions with no cache dir keeps the local cache, want it disabled")
	}
}

func assertSignatureError(t *testing.T, err error, tag string) {
	t.Helper()
	if err == nil {
		t.Fatal("verification passed, want it to fail")
	}
	want := "checksums.txt for " + tag + " is not signed by the gskill release workflow"
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want prefix %q", err, want)
	}
	if got := errs.ExitCode(err); got != 6 {
		t.Errorf("ExitCode = %d, want 6", got)
	}
}
