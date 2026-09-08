package cli

// projectCmd groups the maintenance commands that operate on this project's
// manifest, lockfile, and installed state. These are the only spellings: the
// flat top-level aliases they once shadowed were retired by spec 025, along
// with the `diff` subcommand whose report `check` already subsumed.
type projectCmd struct {
	Sync   syncCmd   `cmd:"" help:"Reconcile disk to the lock's declared state (--prune removes managed orphans)."`
	Repair repairCmd `cmd:"" help:"Re-materialize broken installs and clean up staging."`
	Verify verifyCmd `cmd:"" help:"Re-hash installed content against the lockfile."`
	Check  checkCmd  `cmd:"" help:"Report fast drift status."`
}
