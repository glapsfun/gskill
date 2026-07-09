package cli

import (
	"context"
	"fmt"

	"github.com/glapsfun/gskill/internal/app"
)

// unlinkCmd detaches a single agent from a skill, retaining shared content
// unless --prune.
type unlinkCmd struct {
	Skill string `arg:"" help:"The installed skill to unlink an agent from."`
	Agent string `required:"" help:"The agent ID to detach."`
	Prune bool   `help:"If this was the last agent, also remove the skill, active entry, and store content."`
}

// Help returns the detailed help shown by `gskill unlink --help`.
func (unlinkCmd) Help() string {
	return examplesHelp(
		"gskill unlink my-skill --agent claude",
		"gskill unlink my-skill --agent claude --prune",
	)
}

// Run executes `gskill unlink`.
func (c unlinkCmd) Run(ctx context.Context, out *Output, a *app.App, root projectRoot) error {
	res, err := a.Unlink(ctx, string(root), c.Skill, c.Agent, c.Prune)
	if err != nil {
		return err
	}

	var human string
	switch {
	case res.Pruned:
		human = out.summary(fmt.Sprintf("Unlinked %s from %s and pruned the skill (no agents remain)", c.Agent, c.Skill))
	case res.Unreferenced:
		human = out.warnSummary(fmt.Sprintf("Unlinked %s from %s; skill retained but unreferenced (run with --prune to remove)", c.Agent, c.Skill))
	default:
		human = out.summary(fmt.Sprintf("Unlinked %s from %s; %d agent(s) remain", c.Agent, c.Skill, len(res.RemainingAgents)))
	}
	return out.Result(human, map[string]any{
		"skill":            res.Skill,
		"unlinked_agent":   res.UnlinkedAgent,
		"remaining_agents": res.RemainingAgents,
		"pruned":           res.Pruned,
		"unreferenced":     res.Unreferenced,
	})
}
