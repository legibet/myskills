# AGENTS.md

MySkills is a Go CLI + TUI, and a desktop app, that manages agent skills: a private library at `~/.myskills/skills`, from which skills are enabled per project or globally as links (personal, git-excluded) or copies (committed with the project). Linux and macOS are first-class; Windows is best effort.

`docs/design.md` is the source of truth for behavior: data formats, command semantics, TUI keys, the desktop app (§10) and edge cases. Read the relevant section before changing behavior, and update it in the same change when behavior changes.

## Commands

- Build: `make build` (binary `./myskills`)
- Test: `make test` (`go test -race ./...`)
- Desktop app: `make app` (`go tool mygo build app`, into `dist/app`)
- Lint: `make lint` (`go vet`, `go tool mygo vet ./app`, `golangci-lint`; gofumpt/goimports formatting)

## Release

Releases are semver tags `vX.Y.Z`. `.github/workflows/release.yml` builds every pushed tag with goreleaser (`.goreleaser.yaml`) and mygo (`app/mygo.json`), and publishes the GitHub release once all assets are built. To release:

1. Pick the version from the commits since the last tag: while 0.x, a minor bump for new features or changed behavior, a patch bump for fixes only.
2. Write the release notes to a temporary file: a short Markdown list of what changed for users, in their terms, not the commit log.
3. Run `make release VERSION=x.y.z NOTES=<file>`. It refuses unless HEAD is a clean, pushed `main` whose Test run passed; it sets the version in `app/mygo.json`, commits it, tags it with the notes as the message, and pushes.
4. The release is done when the Release run of the tag passed and `gh release view vX.Y.Z` lists its assets.

## Code

- Packages under `internal/` split by responsibility; `core` is the only domain API, and `cli`, `tui` and the desktop app `app/` are thin frontends over it.
- `app/` uses mygo native UI (package `ui`), pinned at the version in `go.mod`: read its source in the module cache (`ui/api_gen.go`, `website/docs-demo`) rather than guessing an API. The view is immediate mode: keep state, `ui.ListState` and `ui.Handle` in the app, never `ui.Element`s; work runs in goroutines and comes back through `a.post`.
- Tests isolate state with temp `HOME` (and `USERPROFILE`, which Windows reads instead) and `MYSKILLS_HOME`, and use local git repos over `file://` instead of the network. CI runs them on Linux, macOS and Windows.
