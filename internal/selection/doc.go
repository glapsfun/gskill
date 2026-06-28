// Package selection resolves user-provided skill selectors (by name, the "all"
// wildcard, or a name@path disambiguator) against the skills discovered in a
// source. Resolution is a pure function of its inputs: it prefers exact matches,
// refuses to guess between duplicates in non-interactive use, and surfaces close
// matches when a name does not resolve.
package selection
