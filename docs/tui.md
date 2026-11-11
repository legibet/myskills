# TUI

`internal/tui`, built with Bubble Tea v2, Lip Gloss v2, Bubbles v2 and Glamour v2. It runs in the alternate screen, and on exit prints a summary of the changes it made to the normal screen. It works in one project (or global scope) at a time; the desktop app gives the overview across projects.

## Layout

- A header, a body and a footer, separated by blank lines. The header makes the active scope prominent (the project's path, or `global`) and summarizes the other scope (`global 3`).
- The body is a list beside a preview at 100 columns or more, and the list alone when narrower. It resizes live.
- The reader opens the highlighted row's preview full screen, in both layouts, and keeps the row actions that leave the skill in view. It closes when its row leaves the list (for example, a skill disabled on the Enabled tab).
- The preview shows the name, the source line, the state in the active scope, notes (invalid, broken, conflicts, local changes, outdated copies, the update check), `Enabled in <scopes>`, then SKILL.md: the description, the optional frontmatter fields that are set, and the rendered body. Find and the add picker preview SKILL.md the same way.
- The footer's help bar shows the keys that work for the highlighted row, in priority order, as many as fit, always ending with `? help` (except in text inputs). `?` shows the full help.
- Status lines and preview notes state what happened or what is; keys are left to the help bar.

## Skills view

Rows are every library skill: a glyph, the name, the source label, flags (`outdated`, `modified`, `update`, `invalid`), and a right column saying how the skill is enabled in the active scope: `link`, `copy`, `mixed`, or `global` for a project that gets it only from global scope; `broken`, `conflict` or `unmanaged` when something is wrong. The glyph is `●` link, `◆` copy, `◐` mixed or partial, `○` enabled only globally, `✗` broken or conflict, `◇` an unmanaged entry, and none when off, so that what is enabled stands out.

- `space`: enables a skill that is off as a link for the default agents, and disables an enabled one.
- `c`: enables a skill that is off as a copy, converts a link or mixed enablement to copies, and converts copies to links.
- `a`: the skill's agents in the active scope, toggling a placement per agent directory. `A`: the default agents (written to config.json).
- `u`: updates the skill: the library update, then the copy refresh in each scope. Each step asks before discarding its own local changes, and confirming forces only that step. `U`: checks every updatable skill in the background.
- `r` reloads the library, the scopes and changed SKILL.md files; this also happens when the terminal regains focus.
- `d`: removes the skill from the library, after a confirm. `e`: edits SKILL.md in `$VISUAL` or `$EDITOR`. `s`: switches scope, when a project is available.
- `/` filters fuzzily; tabs switch between All, Enabled and Issues. Issues lists invalid skills, broken and conflicting placements, modified copies, and unmanaged (`other`) entries; `space` there removes a broken link, and unmanaged entries are never touched.
- Removing a copy, and anything that would hit `ErrModified`, opens a confirm first.
- An empty library shows an empty state pointing to Find and `myskills add`.

## Find view

`f` opens it from Skills, and `myskills find` opens on it (with the query submitted when one is given). It has levels: the query input, the result list, and the reader. `enter` goes one level in, `esc` one level out.

- The input has the focus when Find opens and takes every key except those that move between levels. A paste always goes to the input; pasted while the list has the focus, it replaces the query.
- A search term runs a debounced skills.sh search (about 250 ms, at least 2 characters); `enter` searches at once. Stale responses are dropped by request id.
- An explicit source (a URL, `owner/repo` with a slash, a path) is named under the input (the git URL or the folder), and `enter` fetches it. Fetching clones, so it never happens while typing. The list then holds the source's skills, previewed from the local files. Going back to the input keeps them; editing the query drops them.
- Errors show in place of the list, and can be retried.
- A row shows the name, `owner/repo`, the state (`✓ in library`, `✓ added`, `✗ conflict`, `✗ invalid`) and the installs.
- `space` asks `Add <skill> to the library?`, naming the git URL it downloads from (or the local folder), then adds it. A skill already in the library, or whose name the library has from another source, is reported instead. Adding never enables.
- `o` opens the repository page; a search result doesn't say where in the repository the skill is.

## Add picker

`myskills add <source>` with several candidates shows a checklist with a preview, to pick the skills to install.

## Behavior

- Mutations run as Bubble Tea commands. While one runs for a skill, that skill's keys are ignored, so the same work never runs twice. Network requests are cancellable and never block input.
- Untrusted text is sanitized as in `cli.md`: names and paths show on one line, and every frame of the app and of the add picker keeps only SGR styling.
- Quitting waits for the mutations in flight, drops queued ones whose command never ran, and removes the checkouts of browsed sources.

## Colors

The palette follows the terminal's. The accent (blue) and success, warning and error (green, yellow, red) are its ANSI colors. Neutral tones (secondary and faint text, rules, the highlighted row, inline code) blend the foreground and background colors the terminal reports; at 256 colors those blends are grays of the palette. When the terminal doesn't report its colors, has 16 colors or fewer, or `NO_COLOR` is set, faint text and reverse video take their place.

SKILL.md previews use the same colors: headings without `#` markers (H1 and H2 in the accent, lower ones in the text color), tinted inline code, and code blocks indented and not highlighted. No Nerd Font glyphs.
