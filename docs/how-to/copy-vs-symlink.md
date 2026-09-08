# Copy vs symlink

Choose how installed skill content lands in an agent's directory: a **relative symlink** into the
committed `.agents/skills/<name>/` directory (the default), or a real **copy**.

## Before you start

- A project ready to `gskill add` or `gskill install`.

## Steps

```bash
gskill add ./skill --symlink     # relative link into .agents/skills/<name> (default)
gskill add ./skill --copy        # write a real copy into the agent dir
```

## When to use which

| Mode | Use when |
| --- | --- |
| `--symlink` (default) | You want one committed copy shared by every agent; the links are relative, so they survive `git clone` on any symlink-capable filesystem. |
| `--copy` | An agent or tool doesn't handle symlinks well, or you need a standalone copy per agent directory. |

## Expected result

- With `--symlink`, the agent's `skills/<name>` entry is a committed relative symlink (e.g.
  `../../.agents/skills/<name>`) into the committed content; verification still detects tampering
  because writes go through to the content the lockfile's checksum covers.
- With `--copy`, a full copy is written into the agent directory.
- Either way, `skills-lock.json` records the install mode so restores are reproducible.

> GSKILL requires working symlinks (macOS and Linux; Windows is unsupported). A checkout made with
> `core.symlinks=false` leaves plain files where agent links should be — `gskill project check` and
> `gskill doctor` report it; re-clone on a symlink-capable filesystem.

## See also

- [Repo-owned storage](../explanation/repo-owned-storage.md)
- [Supported agents](../reference/agents.md)
