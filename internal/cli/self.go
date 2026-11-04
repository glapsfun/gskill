package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/glapsfun/gskill/internal/app"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
)

// selfCmd groups the commands that manage the gskill installation itself, as
// opposed to the skills it manages (spec 031).
type selfCmd struct {
	Upgrade selfUpgradeCmd `cmd:"" help:"Upgrade this gskill executable to the latest stable release, or show how its package manager upgrades it."`
}

// selfUpgradeCmd replaces a standalone gskill with a verified newer release,
// or prints the command a package manager upgrades it with.
type selfUpgradeCmd struct {
	To string `name:"to" placeholder:"X.Y.Z" help:"Upgrade to this exact published release instead of the latest stable one."`
}

// Help returns the detailed help shown by `gskill self upgrade --help`.
func (selfUpgradeCmd) Help() string {
	return examplesHelp(
		"gskill self upgrade",
		"gskill --dry-run self upgrade",
		"gskill self upgrade --to 0.10.0",
		"gskill --yes --no-interactive self upgrade",
		"gskill --yes --json self upgrade",
	)
}

// channelNames are the human names of the package-managed channels.
var channelNames = map[selfupdate.Channel]string{
	selfupdate.ChannelHomebrew:   "Homebrew",
	selfupdate.ChannelNPMGlobal:  "npm",
	selfupdate.ChannelNPMProject: "npm",
	selfupdate.ChannelNPM:        "npm",
	selfupdate.ChannelNPX:        "npx",
	selfupdate.ChannelGo:         "Go",
	selfupdate.ChannelContainer:  "the container image",
	selfupdate.ChannelUnknown:    "an unknown tool",
}

// Run executes `gskill self upgrade`.
func (c selfUpgradeCmd) Run(ctx context.Context, out *Output, a *app.App, g Globals) error {
	if g.Offline {
		return selfUpgradeFailed(out, selfupdate.Outcome{},
			errs.WithHint(errs.New(errs.CodeUsage, "self upgrade needs the network to find releases"), "drop --offline"))
	}
	req := app.SelfUpgradeRequest{To: c.To, NoCache: g.NoCache}
	plan, err := a.SelfUpgradePlan(ctx, req)
	if plan.Action == selfupdate.ActionPrintAction {
		return printAction(out, plan, err)
	}
	if err != nil {
		return selfUpgradeFailed(out, selfupdate.OutcomeFor(plan), err)
	}
	inst := plan.Installation
	if plan.Action == selfupdate.ActionNone {
		defer shadowNote(out, inst)
		return out.Result(upToDateLine(inst.Version, inst), SelfUpgradeJSON(selfupdate.OutcomeFor(plan)))
	}

	target := plan.Target.Version.String()
	if g.DryRun {
		human := fmt.Sprintf("Dry run: would upgrade %s from %s to %s (%s). Nothing was downloaded or changed.",
			inst.Executable, inst.Version, target, inst.Channel)
		defer shadowNote(out, inst)
		return out.Result(human, SelfUpgradeJSON(selfupdate.OutcomeFor(plan)))
	}
	out.Info("gskill %s (%s, %s) at %s", inst.Version, inst.Channel, inst.Platform(), inst.Executable)
	if plan.Requested != "" {
		out.Info("Requested release: %s", target)
	} else {
		out.Info("Latest stable release: %s", target)
	}
	shadowNote(out, inst)
	if !g.Yes && !out.canPrompt() {
		return selfUpgradeFailed(out, selfupdate.OutcomeFor(plan),
			errs.WithHint(errs.New(errs.CodeGeneric, "self upgrade requires confirmation"), "pass --yes to upgrade without a prompt"))
	}
	if !out.Confirm(fmt.Sprintf("Upgrade %s from %s to %s?", inst.Executable, inst.Version, target), g.Yes) {
		declined := selfupdate.OutcomeFor(plan)
		declined.Kind = selfupdate.OutcomeDeclined
		return selfUpgradeFailed(out, declined, errs.New(errs.CodeGeneric, "aborted"))
	}

	outcome, err := a.SelfUpgradeApply(ctx, plan, req, func(line string) { out.Info("%s", line) })
	if err != nil {
		return selfUpgradeFailed(out, outcome, err)
	}
	human := fmt.Sprintf("Upgraded gskill from %s to %s at %s", outcome.PreviousVersion, outcome.InstalledVersion, outcome.Executable)
	// Another upgrade can finish while this one waits for the lock.
	if outcome.Kind == selfupdate.OutcomeUpToDate {
		human = upToDateLine(outcome.InstalledVersion, inst)
	}
	defer shadowNote(out, inst)
	return out.Result(human, SelfUpgradeJSON(outcome))
}

func upToDateLine(version string, inst selfupdate.Installation) string {
	return fmt.Sprintf("gskill %s is up to date (%s, %s, %s)", version, inst.Channel, inst.Platform(), inst.Executable)
}

// shadowNote tells the user when `gskill` on PATH is a different executable,
// whose version this upgrade does not change (FR-011).
func shadowNote(out *Output, inst selfupdate.Installation) {
	if s := inst.PathShadow; s != nil {
		out.Info("Note: `gskill` on PATH is %s (%s), not this executable. It was not changed.", s.Path, s.Version)
	}
}

// printAction reports a package-managed install: nothing is written, and the
// run exits with err's "update available" code without an error line.
func printAction(out *Output, plan selfupdate.UpgradePlan, err error) error {
	inst := plan.Installation
	var b strings.Builder
	fmt.Fprintf(&b, "gskill %s (%s) is managed by %s (%s)\n", inst.Version, inst.Platform(), channelNames[inst.Channel], inst.Executable)
	fmt.Fprintf(&b, "gskill %s is available. No upgrade was performed. To upgrade, run:", plan.Target.Version)
	for _, line := range plan.NextCommand {
		b.WriteString("\n  " + line)
	}
	if rerr := out.Result(b.String(), SelfUpgradeJSON(selfupdate.OutcomeFor(plan))); rerr != nil {
		return rerr
	}
	shadowNote(out, inst)
	return reportedError{err}
}

// selfUpgradeFailed reports a failure. With --json the outcome document is the
// one thing on stdout, so the error is returned as already reported.
func selfUpgradeFailed(out *Output, outcome selfupdate.Outcome, err error) error {
	if !out.JSON() {
		return err
	}
	if outcome.Kind != selfupdate.OutcomeDeclined {
		outcome.Kind = selfupdate.OutcomeFailed
	}
	outcome.Err = err
	if rerr := out.Result("", SelfUpgradeJSON(outcome)); rerr != nil {
		return errors.Join(err, rerr)
	}
	return reportedError{err}
}

// selfUpgradeDoc is the `self upgrade --json` document, fields in contract
// order (spec 031).
type selfUpgradeDoc struct {
	Outcome          string            `json:"outcome"`
	Upgraded         bool              `json:"upgraded"`
	PreviousVersion  string            `json:"previous_version"`
	TargetVersion    *string           `json:"target_version"`
	InstalledVersion string            `json:"installed_version"`
	Executable       string            `json:"executable"`
	Platform         string            `json:"platform"`
	InvokedAs        *string           `json:"invoked_as"`
	Channel          string            `json:"channel"`
	Evidence         string            `json:"evidence"`
	NextCommand      []string          `json:"next_command"`
	PathShadow       *selfShadowDoc    `json:"path_shadow"`
	Error            *selfUpgradeError `json:"error"`
}

type selfShadowDoc struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type selfUpgradeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

// SelfUpgradeJSON is the `self upgrade --json` document for an outcome.
func SelfUpgradeJSON(o selfupdate.Outcome) any {
	doc := selfUpgradeDoc{
		Outcome:          string(o.Kind),
		Upgraded:         o.Upgraded,
		PreviousVersion:  o.PreviousVersion,
		TargetVersion:    nullable(o.TargetVersion),
		InstalledVersion: o.InstalledVersion,
		Executable:       o.Executable,
		Platform:         o.Platform,
		InvokedAs:        nullable(o.InvokedAs),
		Channel:          string(o.Channel),
		Evidence:         o.Evidence,
		NextCommand:      o.NextCommand,
	}
	if doc.NextCommand == nil {
		doc.NextCommand = []string{}
	}
	if o.PathShadow != nil {
		doc.PathShadow = &selfShadowDoc{Path: o.PathShadow.Path, Version: o.PathShadow.Version}
	}
	if o.Err != nil {
		doc.Error = &selfUpgradeError{Code: errs.ExitCode(o.Err), Message: o.Err.Error(), Hint: errs.HintOf(o.Err)}
	}
	return doc
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
