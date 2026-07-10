// Package skillslock owns the shared project-level skills-lock.json v1 format
// that gskill co-owns with compatible external tooling (e.g. `npx skills`).
//
// The package provides a lossless model: fields it does not understand —
// unknown top-level keys, unknown skill-entry keys, and other tools' extension
// blocks — survive every rewrite with their values intact and their key order
// stable, so the file remains fully usable by the other tool. gskill's own
// metadata lives under the namespaced per-entry "gskill" field and never
// repurposes the shared core fields (source, sourceType, skillPath,
// computedHash).
//
// It also hosts the in-memory working model (State/Record) commands operate
// on, and the bridge between records and shared-format entries.
package skillslock
