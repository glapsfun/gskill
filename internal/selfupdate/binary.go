package selfupdate

import (
	"bytes"
	"context"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/glapsfun/gskill/internal/errs"
)

// maxProbeOutput bounds what `version --json` may print.
const maxProbeOutput = 64 << 10

// The CPU names gskill ships for, keyed by executable header value.
var (
	elfArch   = map[elf.Machine]string{elf.EM_X86_64: "amd64", elf.EM_AARCH64: "arm64"}
	machoArch = map[macho.Cpu]string{macho.CpuAmd64: "amd64", macho.CpuArm64: "arm64"}
)

// CheckPlatform fails unless the file at path is an executable for goos and
// goarch. It reads only the header, so a foreign binary is reported clearly
// instead of failing to start.
func CheckPlatform(path, goos, goarch string) error {
	gotOS, gotArch, err := executablePlatform(path)
	if err != nil {
		return errs.Wrap(errs.CodeIntegrity, "the new gskill binary is not a Linux or macOS executable", err)
	}
	if gotOS != goos || gotArch != goarch {
		return errs.New(errs.CodeIntegrity,
			fmt.Sprintf("binary is for %s/%s, this system is %s/%s", gotOS, gotArch, goos, goarch))
	}
	return nil
}

func executablePlatform(path string) (goos, goarch string, err error) {
	if f, ferr := elf.Open(path); ferr == nil {
		defer func() { _ = f.Close() }()
		goos = "linux"
		if f.OSABI != elf.ELFOSABI_NONE && f.OSABI != elf.ELFOSABI_LINUX {
			goos = f.OSABI.String()
		}
		if arch, ok := elfArch[f.Machine]; ok {
			return goos, arch, nil
		}
		return goos, f.Machine.String(), nil
	}
	if f, ferr := macho.Open(path); ferr == nil {
		defer func() { _ = f.Close() }()
		if arch, ok := machoArch[f.Cpu]; ok {
			return "darwin", arch, nil
		}
		return "darwin", f.Cpu.String(), nil
	}
	return "", "", errors.New("unrecognized executable header")
}

// ProbeVersion runs `<path> version --json` with stdin closed and returns the
// version it reports. Callers run it on the staged binary only after its
// archive digest is authenticated, on the running executable, and on the
// gskill PATH finds first, which the user would run anyway.
func ProbeVersion(ctx context.Context, path string, timeout time.Duration) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out cappedBuffer
	runErr := runBusyRetry(probeCtx, path, &out)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		return "", errs.New(errs.CodeIntegrity,
			fmt.Sprintf("%s did not answer `version --json` within %s", path, timeout))
	}
	if runErr != nil {
		return "", errs.Wrap(errs.CodeIntegrity, "run "+path+" version --json", runErr)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v.Version == "" {
		return "", errs.Wrap(errs.CodeIntegrity, path+" version --json printed no version", err)
	}
	return v.Version, nil
}

// runBusyRetry runs `<path> version --json`, retrying while the file is busy.
// On Linux a freshly written binary fails to start with ETXTBSY while any
// process still holds it open for writing, and a child forked elsewhere in
// the process can inherit that handle for a moment (golang/go#22315).
func runBusyRetry(ctx context.Context, path string, out *cappedBuffer) error {
	var err error
	for attempt := 1; attempt <= 10; attempt++ {
		out.Reset()
		cmd := exec.CommandContext(ctx, path, "version", "--json")
		cmd.Stdout = out
		cmd.WaitDelay = time.Second
		if err = cmd.Run(); !errors.Is(err, syscall.ETXTBSY) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt) * 20 * time.Millisecond):
		}
	}
	return err
}

// cappedBuffer keeps at most maxProbeOutput bytes and discards the rest.
type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxProbeOutput - b.Len(); room < len(p) {
		if room > 0 {
			_, _ = b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
