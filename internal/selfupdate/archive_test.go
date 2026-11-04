package selfupdate_test

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

const archiveName = "gskill_0.10.0_linux_amd64.tar.gz"

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestChecksumFor(t *testing.T) {
	t.Parallel()

	manifest, err := os.ReadFile("testdata/v0.9.0/checksums.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := selfupdate.ChecksumFor(manifest, "gskill_0.9.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "99c59babcec411d7b0d2910872be7406c2fd10ab987c4dbf516c52da2a98f50e" {
		t.Errorf("digest = %s", got)
	}

	line := strings.Repeat("a", 64) + "  " + archiveName + "\n"
	for name, m := range map[string]string{"missing": "", "duplicated": line + line} {
		if _, err := selfupdate.ChecksumFor([]byte(m), archiveName); errs.ExitCode(err) != 6 {
			t.Errorf("%s line: err = %v, want exit 6", name, err)
		}
	}
}

func TestExtractBinary_WritesOnlyTheBinary(t *testing.T) {
	t.Parallel()

	data := selfupdatetest.Archive(t,
		selfupdatetest.Entry{Name: "LICENSE", Body: []byte("license")},
		selfupdatetest.Entry{Name: "gskill", Body: []byte("binary bytes")},
		selfupdatetest.Entry{Name: "README.md", Body: []byte("readme")},
	)
	var dst bytes.Buffer
	if err := selfupdate.ExtractBinary(bytes.NewReader(data), archiveName, digest(data), &dst, 1<<20); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "binary bytes" {
		t.Errorf("extracted %q, want the gskill entry only", dst.String())
	}
}

func TestExtractBinary_DigestMismatch(t *testing.T) {
	t.Parallel()

	data := selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: []byte("bin")})
	err := selfupdate.ExtractBinary(bytes.NewReader(data), archiveName, strings.Repeat("0", 64), &bytes.Buffer{}, 1<<20)
	if err == nil || err.Error() != archiveName+" does not match checksums.txt" {
		t.Fatalf("err = %v, want digest mismatch", err)
	}
	if got := errs.ExitCode(err); got != 6 {
		t.Errorf("ExitCode = %d, want 6", got)
	}
}

func TestExtractBinary_RejectsMalformedArchives(t *testing.T) {
	t.Parallel()

	bin := []byte("binary bytes")
	valid := selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: bin})
	tests := map[string]struct {
		data []byte
		msg  string
	}{
		"truncated gzip":     {data: valid[:len(valid)/2]},
		"no gskill entry":    {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "LICENSE", Body: []byte("l")}), msg: "no gskill binary in " + archiveName},
		"symlink entry":      {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Type: tar.TypeSymlink})},
		"directory entry":    {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Type: tar.TypeDir})},
		"nested only":        {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "bin/gskill", Body: bin}), msg: "no gskill binary in " + archiveName},
		"path traversal":     {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "../gskill", Body: bin}), msg: "no gskill binary in " + archiveName},
		"oversized gskill":   {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Size: 256<<20 + 1})},
		"oversized at limit": {data: selfupdatetest.Archive(t, selfupdatetest.Entry{Name: "gskill", Body: bytes.Repeat([]byte("x"), 2048)})},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limit := int64(256 << 20)
			if name == "oversized at limit" {
				limit = 1024
			}
			var dst bytes.Buffer
			err := selfupdate.ExtractBinary(bytes.NewReader(tt.data), archiveName, digest(tt.data), &dst, limit)
			if got := errs.ExitCode(err); got != 6 {
				t.Fatalf("ExitCode = %d (err %v), want 6", got, err)
			}
			if tt.msg != "" && err.Error() != tt.msg {
				t.Errorf("error = %q, want %q", err, tt.msg)
			}
			if bytes.Contains(dst.Bytes(), bin) && name != "oversized at limit" {
				t.Errorf("binary bytes were written for a rejected archive")
			}
		})
	}
}
