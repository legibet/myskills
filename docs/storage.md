# Storage

```
~/.myskills/          $MYSKILLS_HOME overrides
  skills/             the library
  config.json         preferences, hand-editable
  lock.json           records MySkills maintains
  .lock               the inter-process mutation lock
```

None of these files has to exist; a missing file means the defaults.

## config.json

```json
{
  "agents": ["universal", "claude-code"],
  "customAgents": [
    { "id": "pi", "name": "Pi", "global": "~/.pi/agent/skills" },
    { "id": "grok", "name": "Grok", "project": ".grok/skills", "global": "~/.grok/skills" }
  ]
}
```

- `agents` are the default agents for new enablements. When it is missing, the defaults are `universal` and `claude-code`.
- A custom agent's id matches `[a-z0-9-]+` and must not collide with a built-in. It needs at least one directory: `project` is relative and has no `..`; `global` is absolute or starts with `~/`.
- An invalid config is an error, and nothing is written.

## lock.json

```json
{
  "version": 1,
  "skills": {
    "swiftui-expert-skill": {
      "source": { "type": "git", "url": "https://github.com/avdlee/swiftui-agent-skill.git",
                  "ref": "", "path": "swiftui-expert-skill" },
      "commit": "3f2c…",
      "hash": "v1:…",
      "installedAt": "2026-10-31T10:00:00Z",
      "updatedAt": "2026-10-31T10:00:00Z"
    },
    "my-tool": {
      "source": { "type": "local", "path": "/Users/me/dev/my-tool" },
      "hash": "v1:…", "installedAt": "…", "updatedAt": "…"
    }
  },
  "copies": {
    "/Users/me/proj/.agents/skills/tdd": { "skill": "tdd", "hash": "v1:…", "libraryHash": "v1:…",
                                           "copiedAt": "…" }
  },
  "projects": ["/Users/me/proj"]
}
```

- `skills` is keyed by library path and records where each skill came from, so that it can be updated. A skill without a record (made by hand, or inside a linked entry) is not updatable. `source.path` is the skill's directory inside the repository, or for a local source the skill directory itself.
- A skill's `hash` is the content hash of what was installed, which is the upstream content. A library skill whose current hash differs has local changes. `commit` is the last upstream commit verified to have that content.
- `copies` is keyed by the absolute placement path. `hash` is the content of the copy as written, and `libraryHash` the content of the library skill it was copied from, hashed as a copy (see the copy rules in `library.md`). The two can differ, because copying normalizes symlinks and the umask can drop permission bits, so each is compared only with its own side: `hash` with the copy (`modified`), `libraryHash` with the library skill (`outdated`).
- `projects` lists the project roots where MySkills placed something. It answers where a skill is enabled, and tells agent removal where to clean up. A root is dropped only when it exists and holds no managed placement, so a root on an unmounted disk is kept.

## Mutations

- Every mutation holds an exclusive lock on `.lock` (flock; LockFileEx on Windows) while it loads lock.json, changes the filesystem and saves lock.json through a temp file and a rename. Network work happens before the lock is taken, and its results are revalidated under the lock.
- A lock.json or config.json that cannot be read, or that has a newer version, stops every mutation with an error. MySkills never resets it.
- If the filesystem changes succeed but saving lock.json fails, the error is reported. Links stay correct, because status derives them from the filesystem; a copy that lost its record shows as `other`.
