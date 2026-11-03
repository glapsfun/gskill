package selfupdate

import (
	"context"
	"io"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/glapsfun/gskill/internal/version"
)

// Env is everything an upgrade reads from the outside world. Production code
// builds it from the real process; tests replace parts of it.
type Env struct {
	Source     Source
	Verifier   Verifier
	Executable func() (string, error)
	// Args0 is the name the process was started with, used to report the
	// symlink an executable was invoked through.
	Args0    string
	LookPath func(string) (string, error)
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	Build    BuildInfo
	// Probe runs `<path> version --json` and returns the reported version.
	Probe       func(ctx context.Context, path string) (string, error)
	LockTimeout time.Duration
	GOOS        string
	GOARCH      string
}

// BuildInfo describes how the running binary was built.
type BuildInfo struct {
	Version    string
	Kind       version.BuildKind
	ModulePath string
}

// Source finds published releases and opens their assets.
type Source interface {
	Latest(ctx context.Context) (Release, error)
	ByTag(ctx context.Context, tag string) (Release, error)
	Open(ctx context.Context, a Asset) (io.ReadCloser, error)
}

// Verifier authenticates a release's checksum manifest against its signature
// bundle for the given tag.
type Verifier interface {
	VerifyManifest(ctx context.Context, manifest, bundle []byte, tag string) error
}

// Channel is how the running gskill was installed.
type Channel string

// The installation channels; contracts in spec 031 define the evidence for each.
const (
	ChannelStandalone  Channel = "standalone"
	ChannelHomebrew    Channel = "homebrew"
	ChannelNPMGlobal   Channel = "npm-global"
	ChannelNPMProject  Channel = "npm-project"
	ChannelNPX         Channel = "npx"
	ChannelNPM         Channel = "npm"
	ChannelGo          Channel = "go"
	ChannelContainer   Channel = "container"
	ChannelDevelopment Channel = "development"
	ChannelUnknown     Channel = "unknown"
)

// Installation is the gskill that is running.
type Installation struct {
	Executable string
	InvokedAs  string
	Version    string
	Kind       version.BuildKind
	OS         string
	Arch       string
	Channel    Channel
	Evidence   string
	PathShadow *ShadowExecutable
	// NPMProjectDir is the project whose package.json declares gskill, set
	// only for ChannelNPMProject.
	NPMProjectDir string
	// GoInstallDir is where `go install` writes binaries (GOBIN, else
	// GOPATH/bin, else ~/go/bin), set only for ChannelGo.
	GoInstallDir string
}

// Platform returns the installation's "<os>/<arch>".
func (i Installation) Platform() string { return i.OS + "/" + i.Arch }

// ShadowExecutable is a different gskill found first on PATH.
type ShadowExecutable struct {
	Path    string
	Version string
}

// Release is one published gskill version.
type Release struct {
	Version    *semver.Version
	Tag        string
	Prerelease bool
	Draft      bool
	Assets     []Asset
}

// Asset returns the release asset with exactly this name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name string
	URL  string
	Size int64
}

// ArchiveName is the release archive name for a platform. It is the archive
// naming contract in .goreleaser.yaml, which scripts/install.sh also relies on.
func ArchiveName(version, goos, goarch string) string {
	return "gskill_" + version + "_" + goos + "_" + goarch + ".tar.gz"
}

// PlanRequest carries the user's choices for a self-upgrade.
type PlanRequest struct {
	// To is the exact version requested with --to, or empty for the latest
	// stable release.
	To string
}

// Action is what a plan will do.
type Action string

// The plan actions.
const (
	ActionUpgrade     Action = "upgrade"
	ActionNone        Action = "none"
	ActionPrintAction Action = "print-action"
)

// UpgradePlan is what the command will do, decided before any write.
type UpgradePlan struct {
	Installation Installation
	Target       *Release
	Requested    string
	Action       Action
	NextCommand  []string
}

// OutcomeKind is the result of a run.
type OutcomeKind string

// The outcome kinds, spelled as the JSON "outcome" field.
const (
	OutcomeUpgraded       OutcomeKind = "upgraded"
	OutcomeUpToDate       OutcomeKind = "up_to_date"
	OutcomeDryRun         OutcomeKind = "dry_run"
	OutcomeActionRequired OutcomeKind = "action_required"
	OutcomeDeclined       OutcomeKind = "declined"
	OutcomeFailed         OutcomeKind = "failed"
)

// Outcome is the single result of a run, for humans and for --json.
type Outcome struct {
	Kind             OutcomeKind
	Upgraded         bool
	PreviousVersion  string
	TargetVersion    string
	InstalledVersion string
	Executable       string
	Platform         string
	InvokedAs        string
	Channel          Channel
	Evidence         string
	NextCommand      []string
	PathShadow       *ShadowExecutable
	Err              error
}
