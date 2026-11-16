# Use private and non-GitHub repositories

Add skills from private repositories on GitHub, GitLab, or Bitbucket, on your machine and in CI.
GSKILL runs your system `git` for every fetch, so it uses the access Git already has: SSH keys, the
SSH agent, `~/.ssh/config`, and HTTPS credential helpers. GSKILL never stores a credential.

## Before you start

- You can clone the repository with plain `git` on this machine. If `git ls-remote <address>`
  works, GSKILL works with the same address.
- A project with an agent marker (`add` initializes the rest).
- The spelling rules for sources are in the [sources reference](../reference/sources.md).

## Steps

### 1. Give Git access

Pick SSH or HTTPS. GSKILL keeps whichever you use in the source.

**SSH** (all three providers): add your public key to your account, then check it.

| Provider | Where to add the key | Check |
| --- | --- | --- |
| GitHub | Settings, SSH and GPG keys | `ssh -T git@github.com` |
| GitLab | Preferences, SSH Keys | `ssh -T git@gitlab.com` |
| Bitbucket Cloud | Personal settings, SSH keys | `ssh -T git@bitbucket.org` |

**HTTPS**: configure a Git credential helper (Git Credential Manager, `osxkeychain` on macOS, or
`libsecret` on Linux), then run `git ls-remote https://<host>/<path>.git` once and enter:

| Provider | User name | Password |
| --- | --- | --- |
| GitHub | your GitHub user name | a token with read access to the repository contents |
| GitLab | your GitLab user name | a personal, group, or project access token with `read_repository` |
| Bitbucket Cloud | your Bitbucket user name (case-sensitive) or `x-bitbucket-api-token-auth` | an API token with `read:repository:bitbucket` |

The helper stores the credential and Git reuses it. Use Bitbucket API tokens or access tokens; do
not set up new access with Bitbucket app passwords.

Signing in to a provider's CLI or website does not by itself configure Git or GSKILL. With the
GitHub CLI, `gh auth setup-git` makes Git use your `gh` login for HTTPS.

### 2. Add the skill

```bash
# SSH
gskill add git@gitlab.com:acme/platform/skills.git --skill lint --agent claude

# HTTPS
gskill add https://bitbucket.org/acme/skills.git --skill lint --agent claude

# A folder inside a nested GitLab project
gskill add gitlab.com/acme/platform/skills//tools/lint --agent claude
```

The lockfile records the address as you wrote it, without any secret. Commit `skills.toml`,
`skills-lock.json`, and the installed content as usual.

### 3. Set up CI

CI has no terminal, and GSKILL never waits for a password, passphrase, or browser login. A
missing credential fails the command right away. Give the job access in one of two ways.

**A read-only deploy key** (SSH). Store the private key and the provider's published SSH host
keys as CI secrets, then:

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
printf '%s\n' "$SKILLS_DEPLOY_KEY" > ~/.ssh/skills_key && chmod 600 ~/.ssh/skills_key
printf '%s\n' "$SKILLS_KNOWN_HOSTS" >> ~/.ssh/known_hosts
export GIT_SSH_COMMAND='ssh -i ~/.ssh/skills_key -o IdentitiesOnly=yes -o BatchMode=yes'
gskill --no-interactive install --frozen-lockfile
```

**A read-only token** (HTTPS). Put it in the CI machine's Git config, never in a source:

```bash
git config --global url."https://x-access-token:${SKILLS_TOKEN}@github.com/".insteadOf "https://github.com/"
git config --global url."https://oauth2:${SKILLS_TOKEN}@gitlab.com/".insteadOf "https://gitlab.com/"
git config --global url."https://x-bitbucket-api-token-auth:${SKILLS_TOKEN}@bitbucket.org/".insteadOf "https://bitbucket.org/"
gskill --no-interactive install --frozen-lockfile
```

Keep only the line for your provider. Git rewrites the address at fetch time, so the lockfile keeps
the plain address and GSKILL's output hides the token.

### 4. Discover skills in a private scope (optional)

`search --owner` lists repositories through the provider's API, which needs its own token
in the environment. Git credentials are not used for it.

```bash
GITLAB_TOKEN=... gskill search deploy --owner gitlab.com/acme/platform
BITBUCKET_TOKEN=... gskill search deploy --owner bitbucket.org/acme --transport ssh
```

The variables and minimum permissions per provider are in the
[sources reference](../reference/sources.md#tokens). Without a token, discovery lists public
repositories only and says so.

## Expected result

- `add` exits `0`, installs the skill, and records the clone address in the transport you
  used.
- `gskill install --frozen-lockfile` on another machine with the same access installs identical
  content without contacting any provider API.

## Troubleshooting

| Message | Exit | What to do |
| --- | --- | --- |
| `credentials for <host> are missing, expired, or not allowed to read <repository>` | `11` | Check that `git ls-remote <address>` works in the same shell. Renew the token or add the SSH key. |
| `repository <repository> not found or not accessible with the current credentials` | `5` | Check the spelling, then check that your account can see the repository. Providers hide private repositories from accounts without access. |
| `the SSH host key for <host> is unknown or has changed` | `11` | Compare the key with the provider's published fingerprints, then add it to `~/.ssh/known_hosts`. GSKILL never accepts an unknown key on its own. |
| `cannot reach <host>` | `5` | Check the network, proxy, and host name. |
| `ambiguous source: ... could be a repository in a nested GitLab group or a folder inside a repository` | `2` | Add `.git` after the repository or `//` before the folder, as the message shows. |
| `credential in source: the source address contains a password or token` | `2` | Remove the secret from the source and use a credential helper or SSH key. |
| `<provider> rate limit reached` | `5` | Wait until the reset time, or set the provider's token to raise the limit. |

GSKILL turns Git prompts into failures by setting `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=never`,
and, unless you configured SSH yourself, `GIT_SSH_COMMAND='ssh -o BatchMode=yes'`. Each applies
only when you have not set it. Two limits remain:

- An askpass program you configured (`GIT_ASKPASS`, `core.askPass`, or `SSH_ASKPASS`) still runs.
- A `core.sshCommand` set only inside an `includeIf "hasconfig:remote.*.url:..."` block is not
  seen; set `GIT_SSH_COMMAND` instead.

## See also

- [Sources reference](../reference/sources.md)
- [Add a skill from Git](add-a-git-skill.md)
- [Reproduce exactly with `--frozen-lockfile`](reproduce-with-frozen-lockfile.md)
- [Exit codes](../reference/exit-codes.md)
