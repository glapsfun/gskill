# Use different versions of one skill in different projects

Projects are independent: each `skills-lock.json` pins its own version, and
each repository commits its own copy of the content at
`.agents/skills/<name>/`.

```sh
cd ~/dev/repo1
gskill add github.com/example/skills --skill argocd --version 1.4.0

cd ~/dev/repo2
gskill add github.com/example/skills --skill argocd --version 2.0.0
```

Each repo's committed copy holds its own version, and the clone cache keeps a
commit-keyed entry for each, so neither project re-downloads what the machine
already fetched.

## Updating one project

```sh
cd ~/dev/repo1
gskill update argocd
```

Only repo1's lockfile, committed copy, and links change; the switch is atomic
(the project never observes a missing skill), and repo2 is untouched. The
older commit's cache entry stays available until you run
`gskill cache clean`.
