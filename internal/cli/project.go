package cli

// projectCmd groups the maintenance commands that operate on this project's
// manifest, lockfile, and installed state. Each subcommand reuses the exact
// command struct that also backs its hidden top-level alias (see aliasTable),
// so the old flat invocations stay behaviorally identical by construction.
type projectCmd struct {
	Sync   syncCmd   `cmd:"" help:"Reconcile disk to the manifest's desired state (--prune removes orphans)."`
	Repair repairCmd `cmd:"" help:"Re-materialize broken installs and clean up staging."`
	Lock   lockCmd   `cmd:"" help:"Recompute the lockfile from the manifest."`
	Verify verifyCmd `cmd:"" help:"Re-hash installed content against the lockfile."`
	Check  checkCmd  `cmd:"" help:"Report fast drift status."`
	Diff   diffCmd   `cmd:"" help:"Show manifest/lock/disk differences."`
}
