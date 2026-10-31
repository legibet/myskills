# AGENTS.md

myskills is a Go CLI + TUI that manages agent skills: a private library at `~/.myskills/skills`, from which skills are enabled per project or globally as links (personal, git-excluded) or copies (committed with the project). Linux and macOS are first-class; Windows is best effort.

`docs/design.md` is the source of truth for behavior: data formats, command semantics, TUI keys and edge cases. Read the relevant section before changing behavior, and update it in the same change when behavior changes.

## Commands

- Build: `make build` (binary `./myskills`)
- Test: `make test` (`go test -race ./...`)
- Lint: `make lint` (`go vet` + `golangci-lint`, gofumpt/goimports formatting)

## Code

- Packages under `internal/` split by responsibility; `core` is the only domain API, and `cli`/`tui` are thin frontends over it.
- Tests isolate state with temp `HOME` and `MYSKILLS_HOME`, and use local git repos over `file://` instead of the network.
- New files under `~/.myskills` never reuse the name `state.json`: the archived macOS app (branch `macos-app`) owns it.
