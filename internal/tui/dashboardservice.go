package tui

import (
	"context"

	"github.com/glapsfun/gskill/internal/source"
)

// DashboardService is the dashboard's only route to the project. Every method
// is safe to call off the UI goroutine (from a tea.Cmd), and every mutation
// maps one-to-one onto an existing application service so a dashboard action
// and the matching CLI command have identical effect (spec 033 FR-021). The
// adapter in internal/cli enforces all policy, locking, atomicity, and
// rollback; the dashboard only presents, confirms, and refreshes.
type DashboardService interface {
	// Inventory lists the project's installed skills, grouped by repository,
	// with update status, candidate, policy, agents, and health (read-only).
	Inventory(ctx context.Context) ([]DashboardSkill, error)
	// Markdown returns a skill's installed SKILL.md for the preview.
	Markdown(ctx context.Context, name string) (string, error)
	// Versions lists the selectable versions of a skill's source (read-only).
	Versions(ctx context.Context, name string) ([]string, error)

	// PlanUpdate previews a compatible update for one skill (no writes).
	PlanUpdate(ctx context.Context, name string) (ProposedChange, error)
	// PlanVersionChange previews an explicit version change (no writes).
	PlanVersionChange(ctx context.Context, name, target string, latest bool) (ProposedChange, error)
	// PlanRemove previews a removal, including affected installations.
	PlanRemove(ctx context.Context, name string) (ProposedChange, error)

	// ApplyUpdate applies a compatible update to one skill.
	ApplyUpdate(ctx context.Context, name string) ([]ActionOutcome, error)
	// ApplyVersionChange applies an explicit version change to one skill.
	ApplyVersionChange(ctx context.Context, name, target string, latest bool) ([]ActionOutcome, error)
	// ApplyRemove removes one skill.
	ApplyRemove(ctx context.Context, name string) ([]ActionOutcome, error)
	// ApplyGroupUpdate applies a compatible update to a whole repository group.
	ApplyGroupUpdate(ctx context.Context, repoKey string) ([]ActionOutcome, error)
}

// DashboardSkill is one installed skill as the dashboard shows it, keyed by
// its stable Name so selection and in-flight actions survive searching and
// refreshing (spec 033 data-model). Health feeds the STATUS column;
// UpdateStatus drives action eligibility and is not shown as the column.
type DashboardSkill struct {
	Name             string
	Repo             source.Repo
	SourceLabel      string
	Path             string
	InstalledVersion string
	Candidate        string
	Health           string // integrity.DriftStatus: installed/modified/outdated/...
	UpdateStatus     string // app.UpdateStatus: update-available/pinned/...
	Reason           string
	Policy           string
	Agents           []string
	Informational    string
	Markdown         string
}

// Updatable reports whether a normal (compatible) update can move this skill.
// It mirrors app.UpdatePlanItem.Actionable() and is never re-derived from
// other fields in the view (spec 033 FR-007).
func (s DashboardSkill) Updatable() bool { return s.UpdateStatus == statusUpdateAvailable }

// statusUpdateAvailable is the one UpdateStatus a normal update can apply. It
// matches app.StatusUpdateAvailable ("update-available") without importing the
// app package into the view.
const statusUpdateAvailable = "update-available"

// RepoGroup is the installed skills that share one repository identity, in
// display order, used for grouping rows and the group-update flow.
type RepoGroup struct {
	Repo  source.Repo
	Label string
	Names []string // skill names in this group, in display order
}

// ProposedChange is a described, not-yet-applied action for one skill or
// group: the kind, the installed and target revisions, and the skills.toml
// declaration before and after when the action rewrites it.
type ProposedChange struct {
	Kind          ChangeKind
	Target        string // skill name, or repository key for a group update
	FromRev       string
	ToRev         string
	DeclBefore    string
	DeclAfter     string
	Installations []string // affected project/agent targets, for a removal
	// Target version inputs carried through to the apply call.
	Version string
	Latest  bool
}

// ChangeKind names the three actions the dashboard confirms.
type ChangeKind string

// Change kinds.
const (
	ChangeUpdate      ChangeKind = "update"
	ChangeVersion     ChangeKind = "version"
	ChangeRemove      ChangeKind = "remove"
	ChangeGroupUpdate ChangeKind = "group-update"
)

// ActionOutcome is one skill's result from an executed action, shown in the
// results view and used to refresh the table.
type ActionOutcome struct {
	Name   string
	Kind   ChangeKind
	Result string // updated / no-change / removed / refused / failed / ...
	Detail string // reason or error text
}

// Options carries what the dashboard needs from the CLI beyond the service and
// the run context.
type Options struct {
	Offline bool
}
