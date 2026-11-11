# AGENTS.md

MySkills manages agent skills (folders with a `SKILL.md`). Skills live in a private library and are enabled per project or globally, for the agents the user chooses, as links (personal, kept out of git) or copies (committed with the project). It has three frontends over one Go core: a CLI, a TUI and a native desktop app. Linux and macOS are first-class; Windows is best effort.

## Concepts

- **Library**: `$MYSKILLS_HOME/skills` (default `~/.myskills/skills`). A top-level entry is a skill, a collection of skills (collections nest), or a symlink to either elsewhere, called a _linked entry_ (a skill under development, a whole collection).
- **Skill**: a directory with `SKILL.md`, identified by its **library path**, relative to the library root with `/` separators (`tdd`, `agent-skills/review`). Its name is the frontmatter `name` and equals the directory name. Names can repeat across collections, so a name the user types can be ambiguous.
- **Owning entry**: the top-level entry that contains a skill. Removing a skill deletes its folder and the collection folders that this leaves empty. When the skill is behind a symlink in the library (a linked entry), removing it removes that symlink and every skill behind it, and leaves the files the symlink points to.
- **Agent**: an id, a display name, and a project-relative skill directory, a global one, or both. Several agents can share one directory (`.agents/skills`).
- **Scope**: a project root, or global.
- **Placement**: one entry `<agent dir>/<name>` that MySkills manages, in mode `link` (a symlink into the library) or `copy` (real files, recorded in lock.json).
- **Enablement**: all placements of one skill in one scope. Their modes can differ (`mixed`).

## Code

- `internal/core` is the domain service behind the three frontends: `internal/cli` (cobra), `internal/tui` (Bubble Tea) and `app/` (mygo). The frontends read and change state only through core, and use the packages below it for the types in core's signatures, for display and for the skills.sh client. They follow the same rules for modes, confirmations, updates and copy refreshes, and adding a skill never enables it.
- Below core: `skill` (validation, hashing, discovery, copying; filesystem only), `source` (source specs, labels, web URLs), `git`, `skillssh` (the skills.sh client), `agent` (built-in and custom agents), `store` (paths, config.json, lock.json, the mutation lock) and `sanitize` (untrusted text made safe for the terminal).
- Remote sources and the git-aware checks need `git`. Without it, adding a remote source fails with a clear error, and the git exclude and tracked-path checks are skipped; enabling a skill warns about it.
- `go.mod` sets `godebug winsymlink=0`. Since Go 1.23, Windows directory junctions report as irregular files and `filepath.EvalSymlinks` doesn't follow them; the setting makes them symlinks again, so path resolution, walks and cycle checks need no Windows-specific code. Recheck this when Go drops the setting.
- Some Windows limits are accepted and surface as the system's error: names Windows cannot have fail, and a file or folder another program holds open cannot be replaced or removed (a swap restores the previous entry).

## Commands

- Build: `make build` (binary `./myskills`)
- Test: `make test` (`go test -race ./...`)
- Desktop app: `make app` (`go tool mygo build app`, into `dist/app`)
- Lint: `make lint` (`go vet`, `go tool mygo vet ./app`, `golangci-lint`; gofumpt/goimports formatting)

## Tests

- Tests isolate state with a temp `HOME` (and `USERPROFILE`, which Windows reads instead) and `MYSKILLS_HOME`.
- Git sources are local repositories over `file://`. The core and git tests serve them as partial clones (`uploadpack.allowFilter` in the temp home's `.gitconfig`); the tests of the frontends get full clones, with the same result. skills.sh is an `httptest` server; its base URL is injectable.
- CI runs the tests on Linux, macOS and Windows. Core and the CLI are tested on Windows; the TUI and the desktop app ship there but have not been run. Tests of permission bits, or of names Windows cannot have, skip on Windows.

## Docs

Before changing behavior in an area, read its doc; when the behavior changes, update the doc in the same change. The docs record the model, the rules and the reasons behind them. Exact flags, keys and labels live in the code, `--help` and the TUI's `?` help.

- `docs/storage.md`: the `~/.myskills` layout, config.json and lock.json, and how a mutation is locked and committed.
- `docs/library.md`: skill validation, the content hash, the copy rules, adding from sources and skills.sh, updates, removal, and how skills are named in output.
- `docs/enabling.md`: agents and their directories, scope, link and copy placements, the git exclude block, conflicts and concurrent edits, and how status classifies entries.
- `docs/cli.md`: command semantics beyond `--help`, output, exit codes, terminal safety and cancellation.
- `docs/tui.md`: the TUI's views, actions and visual rules.
- `docs/app.md`: the desktop app, and the mygo conventions for code in `app/`.
- `docs/release.md`: cutting a release and what it publishes.
