// Package projstate manages a project's gitignored machine-local state file
// (.gskill/state.json): the stable project identifier, the global store
// object each skill activates, and the links gskill owns. The state exists
// for safe repair and removal only — a project is always restorable from its
// committed skills-lock.json alone, and deleting the state file simply
// regenerates it with a fresh project ID.
package projstate
