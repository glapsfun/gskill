# Reuse skills across projects

Each project commits its own copy of a skill, but the clone cache is shared by every project on the
machine — so installing the same skill in a second project downloads nothing.

## Steps

1. Install in the first project as usual:

   ```sh
   cd ~/dev/repo1
   gskill add github.com/example/skills --skill argocd
   ```

   This fetches the source once into the clone cache
   (`~/.gskill/cache/<commit>/`).

2. Install in the second project:

   ```sh
   cd ~/dev/repo2
   gskill add github.com/example/skills --skill argocd
   ```

   The recorded commit is already cached, so no network is needed — the
   content is restored from the cache into `repo2`'s `.agents/skills/argocd/`.

3. Confirm the cache sharing:

   ```sh
   gskill cache list      # one commit-keyed entry serves both repos
   ```

Each project owns and commits its own `.agents/skills/argocd/` copy — there is no shared store, so
removing the skill from one project never affects the other. Each project also controls its own
version through its own `skills-lock.json` — see
[use different skill versions](use-different-skill-versions.md).
