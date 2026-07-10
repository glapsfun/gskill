// Package lockfile parses the LEGACY gskill.lock format and provides the
// in-memory record types the app layer still works with.
//
// Since spec 012 the committed project lock file is the shared
// skills-lock.json (internal/skillslock); gskill.lock is read only to migrate
// it (gskill migrate lockfile, or automatically by lock-touching commands)
// and is never written again. Persistence goes through the skillslock bridge:
// LockedSkill records round-trip into shared-format entries with a namespaced
// gskill extension block.
package lockfile
