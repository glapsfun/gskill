// Package home resolves and manages the per-user gskill home directory
// (default ~/.gskill, relocatable only via GSKILL_HOME). The home holds the
// global content-addressed store plus its cache, staging area, locks,
// advisory project registry, GC pins, quarantine, and user config — all under
// one root so atomic renames never cross filesystems.
package home
