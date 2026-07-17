# Use different versions of one skill in different projects

Projects are independent: each `skills-lock.json` pins its own version, and
the store holds every pinned version as a separate object.

```sh
cd ~/dev/repo1
gskill add github.com/example/skills --skill argocd --version 1.4.0

cd ~/dev/repo2
gskill add github.com/example/skills --skill argocd --version 2.0.0
```

`gskill store list` now shows two objects for `argocd`. Each repo's active
link resolves to its own version.

## Updating one project

```sh
cd ~/dev/repo1
gskill update argocd
```

Only repo1's lockfile and links change; the switch is atomic (the project
never observes a missing skill), and repo2 — and the store object it uses —
are untouched. Old objects stay in the store until `gskill store gc`
collects them once nothing references them.
