# CLI

Every command works without a terminal, so scripts and coding agents can run it; `--help` lists the commands and flags. This doc covers what `--help` doesn't.

## Terminal and non-terminal

In a terminal, a command that needs a choice asks for it. Without a terminal, it never waits for input:

- `myskills` opens the TUI; without a terminal it prints help.
- `myskills find [query]` opens the TUI's Find view; without a terminal it prints the results.
- `enable` and `disable` without a skill open the TUI; without a terminal they fail.
- `add` with several candidates opens a picker; without a terminal it fails, listing them.
- `remove` confirms; without a terminal it needs `-y`.

`list`, `status`, `check` and `find` take `--json`. `path` prints the exact bytes of the path when stdout is not a terminal. Shell completion completes skills and agents dynamically.

## Skill references

A `<skill>` argument is a library path or a name. A name several library skills share is an error listing the candidates. How skills are named in output is in `library.md` (Names and labels).

## Output

- One line per item: `✓`, `!` or `✗`, then dim details. `✓` and `!` lines go to stdout; `✗` lines and hints go to stderr.
- Colors follow the terminal: none when piped, and `NO_COLOR` is respected.
- Lines state what was done or found. Next steps are suggested only with errors, skipped items and an empty library.
- The exit code is 1 when any item failed, or was skipped for local changes. Warnings, such as disabling a skill that is not enabled, keep it 0.

## Untrusted text

Skill metadata and markdown, skills.sh results, entry names found on disk, and errors quoting them never reach the terminal as escape sequences. Every printed line, spinner message and rendered markdown keeps only SGR styling (`internal/sanitize`). Markdown is sanitized after Glamour renders it, because Glamour decodes entities like `&#27;`. JSON output, and `path` when stdout is not a terminal, are data and are left unchanged.

## Cancellation

Ctrl-C cancels network work and a pending prompt, and stops a multi-item command before its next item. A second Ctrl-C exits immediately.
