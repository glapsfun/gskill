# Manage the global store

## Inspect

```sh
gskill store status                 # objects, size, projects, unused, corrupted
gskill store list                   # per-object hash, skill, version, size, projects
gskill store inspect sha256:<hash>  # integrity (full re-hash), origins, used-by
gskill projects list                # projects known to use the store
```

## Verify and repair

```sh
gskill store verify                 # full-store scan: hashes, metadata, layout,
                                    # permissions, abandoned staging
gskill store repair sha256:<hash>   # re-fetch the recorded exact commit and
                                    # atomically replace the object
```

Repair fails — leaving the object untouched — when no recorded origin carries
an exact commit or the re-fetched content hashes differently: an object is
never silently replaced with different content.

## Pin

```sh
gskill store pin sha256:<hash>      # exempt from garbage collection
gskill store unpin sha256:<hash>
gskill store pins
```

## Registry maintenance

```sh
gskill projects inspect <project-id>
gskill projects prune               # drop entries for deleted projects
gskill projects refresh             # re-derive entries from their lockfiles
```

The registry is advisory: pruning removes registry entries only, and deleting
`~/.gskill/projects/` entirely never breaks a project.
