# Reuse skills across projects

With the global store, installing the same skill version in a second project
downloads nothing and stores nothing new.

## Steps

1. Install in the first project as usual:

   ```sh
   cd ~/dev/repo1
   gskill add github.com/example/skills --skill argocd
   ```

2. Install in the second project (same locked content):

   ```sh
   cd ~/dev/repo2
   gskill install
   ```

   The summary reports the reuse:

   ```text
   global store: 2 reused, no downloads (network not required)
   ```

3. Confirm the sharing:

   ```sh
   gskill store list      # one object, Projects column counts both repos
   gskill projects list   # both projects registered
   ```

Both projects' `.agents/skills/argocd` links resolve to the same
`~/.gskill/store/sha256/<hash>/content`. Each project still controls its own
version through its own `skills-lock.json` — see
[use different skill versions](use-different-skill-versions.md).
