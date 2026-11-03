# myskills

myskills manages agent skills from a private library, and enables them per project or globally as needed: a command line tool with an interactive terminal app, and a desktop app for macOS and Linux that shows and manages every place skills are enabled.

- **Library**: skills live in `~/.myskills/skills`, added from GitHub, any git remote, a local
  folder, or [skills.sh](https://skills.sh).
- **Link** (default): a personal symlink into the library. In a git project, it is kept out of
  git through the repository's `info/exclude`, so nothing shows up in `git status`.
- **Copy**: real files in the project, meant to be committed. myskills records what it copied,
  so it can tell when the library has changed (`outdated`) or the copy was edited (`modified`).
- **Agents**: Built-in supported agents include Universal (`.agents/skills`, read by Codex, Cursor, Gemini CLI and others), Claude (`.claude/skills`) and Antigravity. You can add other custom agents in `~/.myskills/config.json`.

## Install

Download the rolling [`latest` release](https://github.com/legibet/myskills/releases/latest) and put `myskills` on your `PATH`.

Or install from source with Go 1.27 or later:

```sh
go install github.com/legibet/myskills@latest
```

The desktop app is in the `latest` release too:

- **macOS**: open `MySkills.dmg` and drag MySkills to Applications. The app is signed ad hoc, not
  notarized, so clear the quarantine once before opening it:
  `xattr -dr com.apple.quarantine /Applications/MySkills.app`.
- **Linux**: install `MySkills-linux-<arch>.deb`, or install the archive for your user with
  `sh MySkills-install.sh MySkills-linux-<arch>.tar.gz`.

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

| Path                      | Contents                                                    |
| ------------------------- | ----------------------------------------------------------- |
| `~/.myskills/skills/`     | the library                                                 |
| `~/.myskills/config.json` | default agents and custom agents (hand-editable)            |
| `~/.myskills/lock.json`   | where each skill came from, recorded copies, known projects |

Set `MYSKILLS_HOME` to use another location. [`docs/design.md`](docs/design.md) describes the
behavior in detail.

## Development

```sh
make build   # ./myskills
make app     # the desktop app for this Mac, in dist/app
make test    # go test -race ./...
make lint    # go vet, mygo vet, golangci-lint
make publish # replace the latest release with a build of HEAD (goreleaser, mygo, gh)
```

The desktop app (`app/`) is built with [mygo](https://github.com/egoist/mygo). The Swift macOS app that
preceded it is archived on the `macos-app` branch.

## License

[MIT](LICENSE)
