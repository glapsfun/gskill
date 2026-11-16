# Provider validation record

The automated tests cover GitHub, GitLab, and Bitbucket with local stand-ins (see
`test/integration/provider_matrix_test.go` and `test/integration/private_http_test.go`). This page
records the checks run by hand against the real services. Add a row whenever a check is run again,
and keep the old rows.

Build used: `gskill v0.12.1-0.20261114175541-3176f4d9e705+dirty`, a development build of branch
`035-provider-private-sources`. Each run used an empty `GSKILL_HOME` and no `GIT_ASKPASS` or
`SSH_ASKPASS`.

## Public repositories

| Date | Provider | Check | Outcome |
| --- | --- | --- | --- |
| 2026-11-14 | GitHub.com | `gskill add https://github.com/glapsfun/gskill.git --list` | Pass: skills listed over HTTPS. |
| 2026-11-14 | GitHub.com | `gskill add git@github.com:glapsfun/gskill.git --list` | Pass: 24 skills listed over SSH with the maintainer's key. |
| 2026-11-14 | GitHub.com | `gskill search release --owner github.com/glapsfun` | Pass: 11 repositories listed, including `.github`; hits name `github` and `github.com/glapsfun/<repo>`; public-only warning shown. |
| 2026-11-14 | GitHub.com | `gskill add https://github.com/glapsfun/definitely-not-a-repo-035.git --list` | Pass: exit `5`, "repository glapsfun/definitely-not-a-repo-035 not found or not accessible with the current credentials". |
| 2026-11-14 | GitLab.com | `gskill add https://gitlab.com/gitlab-com/create-stage/source-code-agent-skills.git --list` | Pass: the 3 skills of a project in a nested group listed. |
| 2026-11-14 | GitLab.com | `gskill add` of `fe-performance-review` from that project, then `gskill install --frozen-lockfile` in a fresh copy with a fresh `GSKILL_HOME` | Pass: lock records `sourceOwner` `gitlab-com/create-stage`; the frozen install fetched the same commit and left the lockfile byte-identical. |
| 2026-11-14 | GitLab.com | `gskill add gitlab.com/gitlab-com/create-stage/source-code-agent-skills --list` | Pass: exit `2` before any network access, with the `.git` and `//` spellings. |
| 2026-11-14 | GitLab.com | `gskill add git@gitlab.com:gitlab-com/create-stage/source-code-agent-skills.git --list` on a machine without a `gitlab.com` host key | Pass: exit `11`, "the SSH host key for gitlab.com is unknown or has changed"; no prompt. |
| 2026-11-14 | GitLab.com | `gskill add https://gitlab.com/glapsfun/definitely-not-a-repo-035.git --list` without credentials | Pass: exit `11` without a prompt, naming `gitlab.com` and the repository. GitLab asks for credentials rather than answering "not found". |
| 2026-11-14 | GitLab.com | Discovery of `gitlab.com/gitlab-com/create-stage` | Pass: 16 projects across the group and its subgroups; public-only warning shown. |
| 2026-11-14 | Bitbucket Cloud | `gskill add https://bitbucket.org/atlassian/aws-s3-deploy.git --list` | Pass: resolved and fetched over HTTPS; the repository has no skills, so the list is empty with exit `0`. |
| 2026-11-14 | Bitbucket Cloud | `gskill add git@bitbucket.org:atlassian/aws-s3-deploy.git --list` on a machine without a `bitbucket.org` host key | Pass: exit `11`, "the SSH host key for bitbucket.org is unknown or has changed"; no prompt. |
| 2026-11-14 | Bitbucket Cloud | `gskill add https://bitbucket.org/glapsfun/definitely-not-a-repo-035.git --list` without credentials | Pass: exit `11` without a prompt. |
| 2026-11-14 | Bitbucket Cloud | Discovery of `bitbucket.org/atlassian` | Pass: 406 repositories across 5 pages. Bitbucket reports SSH addresses on `ssh.bitbucket.org`; GSKILL treats that host as Bitbucket's. |

The first discovery run against Bitbucket found that every repository was skipped, because its SSH
address names `ssh.bitbucket.org`, and that GitHub's `.github` repository was rejected. Both were
fixed and the rows above are from the run after the fix.

## Private repositories

| Provider | Status | Reason |
| --- | --- | --- |
| GitHub.com | Not run | Needs a maintainer with a private repository and a token or SSH key for it. |
| GitLab.com | Not run | Needs a maintainer with a private project in a group and a token or SSH key for it. |
| Bitbucket Cloud | Not run | Needs a maintainer with a private repository and an API token or SSH key for it. |

To run them, follow these steps for each provider:

1. `gskill add git@<host>:<path>.git --ref main --agent claude` with the SSH key in the agent.
2. `gskill add https://<host>/<path>.git --ref main --agent claude` with a credential helper.
3. `gskill install --frozen-lockfile` in a fresh clone of the project.
4. The same frozen install with `GIT_SSH_COMMAND='ssh -i /nonexistent -o IdentitiesOnly=yes'`:
   expect exit `11` and no prompt.
5. `gskill search '' --owner <host>/<scope>` with the provider's token set: expect the private
   repositories in the results.
