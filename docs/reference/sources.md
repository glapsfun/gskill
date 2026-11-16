# Sources

A source tells GSKILL where a skill lives: a Git repository on GitHub, GitLab, Bitbucket, or any
other Git host, or a folder on disk. This page lists every spelling GSKILL accepts for a Git
source, how it finds where the repository ends, and how `search --owner` discovers
repositories. For setting up access to private repositories, see
[Use private and non-GitHub repositories](../how-to/use-private-repositories.md).

## Spellings

| You write | Repository | Folder | Clone address recorded |
| --- | --- | --- | --- |
| `acme/skills` | `github.com/acme/skills` | | `https://github.com/acme/skills.git` |
| `acme/skills/tools/lint` | `github.com/acme/skills` | `tools/lint` | `https://github.com/acme/skills.git` |
| `https://github.com/acme/skills.git` | `github.com/acme/skills` | | same |
| `git@github.com:acme/skills.git` | `github.com/acme/skills` | | same |
| `git@github.com:acme/skills.git//tools/lint` | `github.com/acme/skills` | `tools/lint` | `git@github.com:acme/skills.git` |
| `ssh://git@github.com/acme/skills.git` | `github.com/acme/skills` | | same |
| `https://gitlab.com/acme/platform/skills.git` | `gitlab.com/acme/platform/skills` | | same |
| `git@gitlab.com:acme/platform/skills.git` | `gitlab.com/acme/platform/skills` | | same |
| `gitlab.com/acme/platform/skills//tools/lint` | `gitlab.com/acme/platform/skills` | `tools/lint` | `https://gitlab.com/acme/platform/skills.git` |
| `ssh://git@gitlab.com:2222/acme/platform/skills.git` | `gitlab.com/acme/platform/skills` | | same |
| `https://bitbucket.org/acme/skills.git` | `bitbucket.org/acme/skills` | | same |
| `https://ci@bitbucket.org/acme/skills.git` | `bitbucket.org/acme/skills` | | same, user name kept |
| `git@bitbucket.org:acme/skills.git` | `bitbucket.org/acme/skills` | | same |
| `deploy@git.example.com:team/sub/skills.git` | `git.example.com/team/sub/skills` | | same |
| `git@github-work:acme/skills.git` | `github-work/acme/skills` | | same (an SSH config alias) |

GSKILL keeps the transport you wrote. An SSH source is fetched over SSH and an HTTPS source over
HTTPS by every later command, including `gskill install --frozen-lockfile`. A user name and a port
in the address are kept. Plain `http://` and `git://` addresses are upgraded to HTTPS.

## Where the repository ends

GSKILL decides where the repository ends and the skill's folder begins in this order:

1. **`//`** splits the two explicitly, on every host and transport:
   `gitlab.com/acme/platform/skills//tools/lint`.
2. **A segment ending in `.git`** is the last part of the repository:
   `gitlab.com/acme/platform/skills.git/tools/lint`.
3. **The host's default:**
   - GitHub.com and Bitbucket Cloud repositories are always two levels deep, so the first two
     segments are the repository and the rest is the folder. Bare `owner/repo` means GitHub.
   - GitLab.com groups can nest, so a GitLab path longer than two segments with no `//` or `.git`
     is ambiguous. GSKILL refuses it with exit code `2` before contacting the network, and
     the message shows both fixes:

     ```text
     gitlab.com/acme/platform/skills.git     the repository acme/platform/skills
     gitlab.com/acme/platform//skills        the folder skills in acme/platform
     ```

   - Any other host keeps the two-segment default. Mark a deeper repository with `.git` or `//`.

A project that added an unmarked GitLab path such as `gitlab.com/acme/skills/tools/lint` before
this rule keeps working: when the lockfile already records that exact source, GSKILL uses the
repository and folder the lockfile recorded.

`--repo` values (for `update --list --repo`) name a repository, never a folder, so an
unmarked GitLab path there is always the whole repository.

## Secrets in addresses

GSKILL refuses a source that carries a password or token, such as
`https://oauth2:<token>@gitlab.com/acme/skills.git`, with exit code `2`, and never prints the
secret. The address would otherwise be written to `skills.toml` and `skills-lock.json`. Let a Git
credential helper or an SSH key supply access instead.

## Repository identity

GSKILL treats two sources as the same repository when the host and the full repository path
match, whatever the transport, the `.git` suffix, the user name, the port, or the letter case of
the host. The same path on two hosts, such as `github.com/acme/skills` and
`gitlab.com/acme/skills`, is two repositories. Progress lines name a repository as
`acme/skills` on GitHub and as `gitlab.com/acme/platform/skills` elsewhere.

## Discovery

`search --owner` lists a scope's repositories through the provider's API and searches
each one.

| `--owner` value | Scope |
| --- | --- |
| `acme` or `github.com/acme` | a GitHub user or organization |
| `gitlab.com/acme` or `gitlab.com/acme/platform` | a GitLab group, including its subgroups, or a user |
| `bitbucket.org/acme` | a Bitbucket Cloud workspace |

```bash
gskill search deploy --owner gitlab.com/acme/platform
gskill search deploy --owner bitbucket.org/acme --transport ssh
```

- Discovery reads every page, up to 1000 repositories per scope, and warns when it stops there.
- `--transport ssh` scans and reports each repository's SSH address; the default is HTTPS.
- Each result names its provider and repository (`provider` and `repository` in `--json`).
- A scope that cannot be listed fails the search with its own exit code, rather than reporting no
  matches.

### Tokens

Discovery reads a token only from the environment, and sends it only to that provider's API.
Without a token it lists public repositories and says so.

| Provider | Variables, in order | Minimum access |
| --- | --- | --- |
| GitHub | `GITHUB_TOKEN`, then `GH_TOKEN` | a fine-grained token with read access to the repositories (Metadata: Read-only), or a classic token with `repo` for private repositories |
| GitLab | `GITLAB_TOKEN` | a personal, group, or project access token with `read_api` |
| Bitbucket | `BITBUCKET_TOKEN`, sent as a bearer token; with `BITBUCKET_EMAIL` also set, sent as your Atlassian account email and API token | an API token or access token with `read:repository:bitbucket` |

Adding, installing, updating, and verifying skills never use these tokens or any provider API.
They use your Git credentials only.

## Support matrix

| Host | Git over HTTPS | Git over SSH | Nested namespaces | Discovery |
| --- | --- | --- | --- | --- |
| GitHub.com | yes | yes | not applicable | yes |
| GitLab.com | yes | yes | yes | yes, groups include subgroups |
| Bitbucket Cloud | yes | yes | not applicable | yes, workspaces |
| GitHub Enterprise Server | yes, as a generic host | yes, as a generic host | not applicable | no |
| Self-managed GitLab | yes, as a generic host | yes, as a generic host | with `.git` or `//` | no |
| Bitbucket Data Center | yes, as a generic host | yes, as a generic host | with `.git` or `//` | no |

Discovery on any other host fails with exit code `2` and names the supported providers.

## Mixed GSKILL versions

Older GSKILL releases restore nested GitLab and `//` sources exactly from the lockfile with
`gskill install --frozen-lockfile`, because they read the recorded clone address. They re-read
the source spelling for `update` and `upgrade` with their own rules, which do not know nested
groups or `//`. Teams that use those spellings should upgrade GSKILL together.

## See also

- [Use private and non-GitHub repositories](../how-to/use-private-repositories.md)
- [Add a skill from Git](../how-to/add-a-git-skill.md)
- [`skills-lock.json` schema](lockfile-schema.md)
- [Exit codes](exit-codes.md)
