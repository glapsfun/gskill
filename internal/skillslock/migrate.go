package skillslock

import (
	"sort"

	"github.com/glapsfun/gskill/internal/lockfile"
)

// MigrateFromLegacy converts a legacy gskill.lock into the shared
// skills-lock.json form (spec 012 FR-008): every mappable fact is preserved —
// compatible core fields plus the namespaced gskill block. computedHash is
// left empty (it is not derivable from the legacy record) and is recorded the
// next time content is hashed; entries carrying gskill metadata validate
// without it.
func MigrateFromLegacy(lf *lockfile.Lockfile) *Lock {
	l := New()
	names := make([]string, 0, len(lf.Skills))
	for name := range lf.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		l.SetEntry(name, FromLegacy(lf.Skills[name]))
	}
	return l
}
