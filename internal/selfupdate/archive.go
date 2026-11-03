package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/glapsfun/gskill/internal/errs"
)

// maxBinaryBytes bounds the extracted gskill binary (about 30 MB today).
const maxBinaryBytes = 256 << 20

// ChecksumFor returns the SHA-256 that the manifest lists for name. The
// manifest must hold exactly one line for it.
func ChecksumFor(manifest []byte, name string) (string, error) {
	var found []string
	for line := range strings.SplitSeq(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			found = append(found, fields[0])
		}
	}
	if len(found) != 1 || !isSHA256Hex(found[0]) {
		return "", errs.New(errs.CodeIntegrity, "checksums.txt has no single valid entry for "+name)
	}
	return found[0], nil
}

func isSHA256Hex(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// ExtractBinary streams the archive r, writes its gskill binary to dst, and
// checks that the whole archive matches wantSHA256. The digest is only known
// once the stream ends, so callers must not install dst unless it returns nil.
func ExtractBinary(r io.Reader, name, wantSHA256 string, dst io.Writer, maxBinary int64) error {
	src := &trackingReader{r: r}
	h := sha256.New()
	tee := io.TeeReader(src, h)

	found, err := copyBinary(tee, name, dst, maxBinary)
	if err == nil {
		// Drain the rest so the digest covers every byte of the archive.
		_, err = io.Copy(io.Discard, tee)
	}
	if err != nil {
		var wErr writeError
		switch {
		case errors.As(err, &wErr):
			return errs.Wrap(errs.CodeGeneric, "write the new gskill binary", wErr.err)
		case src.err != nil:
			// The download broke, not the archive. A source error that
			// already carries a code (an oversized body) keeps it.
			var coded *errs.Error
			if errors.As(src.err, &coded) {
				return src.err
			}
			return errs.Wrap(errs.CodeSourceUnavailable, "download "+name, src.err)
		default:
			return errs.Wrap(errs.CodeIntegrity, "read "+name, err)
		}
	}
	if !found {
		return errs.New(errs.CodeIntegrity, "no gskill binary in "+name)
	}
	if hex.EncodeToString(h.Sum(nil)) != wantSHA256 {
		return errs.WithHint(errs.New(errs.CodeIntegrity, name+" does not match checksums.txt"),
			"nothing was changed; retry, and report it if it happens again")
	}
	return nil
}

func copyBinary(r io.Reader, name string, dst io.Writer, maxBinary int64) (bool, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return false, fmt.Errorf("open gzip stream of %s: %w", name, err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return found, nil
		}
		if err != nil {
			return found, fmt.Errorf("read tar entry of %s: %w", name, err)
		}
		// Only the exact root name counts: bin/gskill or ../gskill is not the
		// binary the release contract ships.
		if hdr.Name != "gskill" || found {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return found, fmt.Errorf("gskill in %s is not a regular file", name)
		}
		if hdr.Size > maxBinary {
			return found, fmt.Errorf("gskill in %s is %d bytes, over the %d-byte limit", name, hdr.Size, maxBinary)
		}
		if _, err := io.Copy(writerOnly{dst}, io.LimitReader(tr, maxBinary)); err != nil {
			return found, fmt.Errorf("extract gskill from %s: %w", name, err)
		}
		found = true
	}
}

// trackingReader remembers a read error from the underlying download, so a
// dropped connection is reported as a network failure, not a bad archive.
type trackingReader struct {
	r   io.Reader
	err error
}

func (t *trackingReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		t.err = err
	}
	return n, err
}

// writerOnly marks errors from dst so they are not mistaken for a bad archive.
type writerOnly struct{ w io.Writer }

type writeError struct{ err error }

func (e writeError) Error() string { return e.err.Error() }

func (w writerOnly) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		return n, writeError{err}
	}
	return n, nil
}
