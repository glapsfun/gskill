package discovery

import "strings"

// NormalizeID exposes the normalized-identity derivation for callers outside the
// package (e.g. the installer comparing a selected id to frontmatter).
func NormalizeID(folder string) string { return normalizeID(folder) }

// normalizeID derives a stable, normalized skill identifier from a folder name
// (research R2): lowercased, with every run of characters outside [a-z0-9]
// collapsed to a single hyphen and leading/trailing hyphens trimmed. The result
// matches the manifest-key grammar ^[a-z0-9]+(-[a-z0-9]+)*$ for any input that
// contains at least one alphanumeric character.
func normalizeID(folder string) string {
	var b strings.Builder
	b.Grow(len(folder))
	prevHyphen := false
	for _, r := range strings.ToLower(folder) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// humanizeName derives a human-readable display name from a folder name when the
// SKILL.md omits one (FR-007): word separators become spaces and each word is
// title-cased.
func humanizeName(folder string) string {
	fields := strings.FieldsFunc(folder, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, w := range fields {
		if w == "" {
			continue
		}
		fields[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(fields, " ")
}
