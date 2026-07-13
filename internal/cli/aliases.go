package cli

// Alias kinds and mechanisms.
const (
	aliasKindCommand = "command"
	aliasKindFlag    = "flag"

	aliasMechKong   = "kong-alias"     // visible annotation on the canonical command
	aliasMechHidden = "hidden-command" // absent from all help listings
)

// aliasMapping records one retained old invocation and the canonical form it
// resolves to. For Kind aliasKindFlag, Old and Canonical encode the owning
// command followed by the flag, space-separated (e.g. "add --agents" ->
// "add --agent").
type aliasMapping struct {
	Old       string // exact invocation that must keep working
	Canonical string // canonical form it maps to
	Kind      string // aliasKindCommand or aliasKindFlag
	Mechanism string // aliasMechKong or aliasMechHidden
}

// aliasTable is the single source of truth for backward-compatible aliases:
// it drives the alias-equivalence tests, typo suggestions, and shell
// completion. Every entry here must keep behaving identically to its
// canonical form, silently and forever (no deprecation nags).
var aliasTable = []aliasMapping{
	{Old: "find", Canonical: "search", Kind: aliasKindCommand, Mechanism: aliasMechKong},
	{Old: "tui", Canonical: "dashboard", Kind: aliasKindCommand, Mechanism: aliasMechKong},
	{Old: "sync", Canonical: "project sync", Kind: aliasKindCommand, Mechanism: aliasMechHidden},
	{Old: "repair", Canonical: "project repair", Kind: aliasKindCommand, Mechanism: aliasMechHidden},
	{Old: "verify", Canonical: "project verify", Kind: aliasKindCommand, Mechanism: aliasMechHidden},
	{Old: "check", Canonical: "project check", Kind: aliasKindCommand, Mechanism: aliasMechHidden},
	{Old: "diff", Canonical: "project diff", Kind: aliasKindCommand, Mechanism: aliasMechHidden},
	{Old: "status", Canonical: "list", Kind: aliasKindCommand, Mechanism: aliasMechHidden},

	// Flag audit result (spec FR-009): the shared vocabulary — --agent
	// (repeatable), --global/--project, --force, --all, --dry-run, --yes,
	// --prune, --ref, --max-depth, --include/--exclude — was already
	// consistent across commands, so no flag was renamed and no flag-kind
	// rows exist. A future flag rename must add a row here shaped like
	// {Old: "add --agents", Canonical: "add --agent", Kind: "flag", ...}
	// so the alias tests pick it up automatically.
}
