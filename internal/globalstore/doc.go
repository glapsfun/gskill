// Package globalstore implements the user-level, content-addressed skill
// store under the gskill home (~/.gskill/store). Objects are immutable
// content directories keyed by their canonical integrity.HashDir hash, with a
// schema-versioned metadata record alongside. Admission stages content under
// an object lock and promotes it with an atomic rename; verification fully
// re-hashes content before any activation and quarantines corruption
// fail-closed. Deletion happens only through the conservative mark-and-sweep
// garbage collector, never as a side effect of a project operation.
package globalstore
