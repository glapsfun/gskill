// Package selfupdate upgrades the running gskill executable from a signed
// GitHub release, or reports how its package manager upgrades it.
//
// Only a standalone install (a release archive or the install script) is ever
// replaced. Homebrew, npm, Go, container, and other managed installs are
// detected from the executable's path and build info and get the command their
// own tool upgrades with; their files are never written.
//
// Trust model: a release is installed only after its checksums.txt verifies as
// signed, keyless, by .github/workflows/release.yml of glapsfun/gskill at the
// target tag, the same identity the npm release pipeline checks. The archive
// must then match that manifest, and the extracted binary must run on this
// platform and report the target version. Renaming that workflow file breaks
// upgrades for every deployed gskill, so its path is part of the contract
// (spec 031).
package selfupdate
