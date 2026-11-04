package cli_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/glapsfun/gskill/internal/cli"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/selfupdate"
)

const standaloneEvidence = "release build outside any package-managed location"

// The self upgrade JSON contract: one document per outcome with a fixed key
// set (spec 031, contracts/cli-self-upgrade.md).
func TestSelfUpgradeJSON_Goldens(t *testing.T) {
	t.Parallel()

	upgraded := selfupdate.Outcome{
		Kind: selfupdate.OutcomeUpgraded, Upgraded: true,
		PreviousVersion: "0.9.0", TargetVersion: "0.10.0", InstalledVersion: "0.10.0",
		Executable: "/usr/local/bin/gskill", Platform: "linux/amd64",
		Channel: selfupdate.ChannelStandalone, Evidence: standaloneEvidence,
	}
	actionRequired := selfupdate.Outcome{
		Kind:            selfupdate.OutcomeActionRequired,
		PreviousVersion: "0.9.0", TargetVersion: "0.10.0", InstalledVersion: "0.9.0",
		Executable: "/opt/homebrew/Caskroom/gskill/0.9.0/gskill", Platform: "darwin/arm64",
		InvokedAs: "/opt/homebrew/bin/gskill", Channel: selfupdate.ChannelHomebrew,
		Evidence: "path contains /Caskroom/gskill/", NextCommand: []string{"brew upgrade glapsfun/tap/gskill"},
	}
	failed := upgraded
	failed.Kind, failed.Upgraded, failed.InstalledVersion = selfupdate.OutcomeFailed, false, "0.9.0"
	failed.Err = errs.WithHint(errs.New(errs.CodeIntegrity, "gskill_0.10.0_linux_amd64.tar.gz does not match checksums.txt"),
		"nothing was changed; retry, and report it if it happens again")

	goldenJSON(t, "self-upgrade-json-upgraded.golden", cli.SelfUpgradeJSON(upgraded))
	goldenJSON(t, "self-upgrade-json-action-required.golden", cli.SelfUpgradeJSON(actionRequired))
	goldenJSON(t, "self-upgrade-json-failed.golden", cli.SelfUpgradeJSON(failed))

	data, err := json.Marshal(cli.SelfUpgradeJSON(upgraded))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range doc {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{
		"channel", "error", "evidence", "executable", "installed_version", "invoked_as",
		"next_command", "outcome", "path_shadow", "platform", "previous_version", "target_version", "upgraded",
	}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if string(doc["next_command"]) != "[]" {
		t.Errorf("next_command = %s, want []", doc["next_command"])
	}
}
