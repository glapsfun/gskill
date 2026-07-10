# Copy vs symlink

Choose how installed skill content lands in an agent's directory: a **symlink** into GSKILL's
content-addressed store (the default), or a real **copy**.

## Before you start

- A project ready to `gskill add` or `gskill install`.

## Steps

```bash
gskill add ./skill --symlink     # link into the store (default)
gskill add ./skill --copy        # write a real copy into the agent dir
```

## When to use which

| Mode | Use when |
| --- | --- |
| `--symlink` (default) | You want minimal disk use and instant restores; the agent and filesystem support symlinks. |
| `--copy` | The agent or filesystem doesn't handle symlinks well (some Windows setups), or you need a standalone copy. |

## Expected result

- With `--symlink`, the agent's `skills/<name>` entry points into the store; verification still detects
  tampering because writes go through to the store content.
- With `--copy`, a full copy is written into the agent directory.
- Either way, `skills-lock.json` records the install mode so restores are reproducible.

## See also

- [The store and the cache](../explanation/store-and-cache.md)
- [Supported agents](../reference/agents.md)
