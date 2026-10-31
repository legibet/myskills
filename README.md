# myskills

myskills manages agent skills (folders with a `SKILL.md`) from one private library, and enables
them per project or globally only where you need them. It is a command line tool with an
interactive terminal app, for Linux and macOS (Windows is best effort).

- **Library**: skills live in `~/.myskills/skills`, added from GitHub, any git remote, a local
  folder, or [skills.sh](https://skills.sh). Adding never enables anything.
- **Link** (default): a personal symlink into the library. In a git project, it is kept out of
  git through the repository's `info/exclude`, so nothing shows up in `git status`.
- **Copy**: real files in the project, meant to be committed. myskills records what it copied,
  so it can tell when the library has changed (`outdated`) or the copy was edited (`modified`),
  and never deletes edited copies without asking.
- **Agents**: Universal (`.agents/skills`, read by Codex, Cursor, Gemini CLI and others), Claude
  Code (`.claude/skills`) and Antigravity are built in; others can be added. Skills are enabled
  for the default agents unless you choose.

## Install

Go 1.27 or later and git are required.

```sh
git clone https://github.com/legibet/MySkills.git
cd MySkills
make install   # go install .
```

## Quick start

```sh
myskills                      # interactive app: browse, enable, find and add skills
myskills find pdf             # search skills.sh
myskills add anthropics/skills -s pdf
myskills enable pdf           # link it into the current project
myskills enable pdf --copy    # or copy it, to commit with the project
myskills enable -g pdf        # or enable it globally
myskills status               # what is enabled here and globally
myskills update               # update library skills and refresh their copies
```

`myskills --help` and `myskills <command> --help` describe every command and flag. In the
interactive app, `?` shows all keys.

## Files

| Path | Contents |
|---|---|
| `~/.myskills/skills/` | the library |
| `~/.myskills/config.json` | default agents and custom agents (hand-editable) |
| `~/.myskills/lock.json` | where each skill came from, recorded copies, known projects |

Set `MYSKILLS_HOME` to use another location. [`docs/design.md`](docs/design.md) describes the
behavior in detail.

## Development

```sh
make build   # ./myskills
make test    # go test -race ./...
make lint    # go vet + golangci-lint
```

The macOS app that preceded this tool is archived at the `macos-app` tag.

## License

[MIT](LICENSE)
