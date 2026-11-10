# Desktop app

`app/` is a native app built with [mygo](https://github.com/egoist/mygo): native UI drawn by mygo (package `ui`), no webview. It gives what the TUI, bound to one project, can't: the overview and management of every place skills are enabled. It calls `core` only, and follows the same rules as the CLI and the TUI.

## Code

- mygo is pinned at the version in `go.mod`. Read its source in the module cache (`ui/api_gen.go`, `website/docs-demo`) rather than guessing an API.
- The view is immediate mode. The app keeps its state, `ui.ListState`s and `ui.Handle`s; it never keeps `ui.Element`s.
- Work runs in goroutines and comes back to the main thread through `a.post`.
- One file per page; `placements.go` holds the placement rows that Skills and Projects both show. The look (colors, sizes, spacing) is defined once in `app/style.go`.
- Tests drive the window headless with `ui.Tester` over the real core in a temp home (clicks, menus, alerts, typing), running the work the app posts to the main thread in order; skills.sh is an `httptest` server. `go tool mygo vet ./app` checks the native UI lifetimes.

## Window

- One window: a sidebar with Skills, Projects, Find and Agents beside the page; `⌘1`–`⌘4` choose a page and `⌘F` focuses its search field. On macOS the title bar is hidden and the window controls sit over the sidebar, which shows the sidebar material.
- Skills' sidebar badge counts the updates the last check found; Projects' counts the places that need attention (outdated or modified copies, broken links).
- A page is a list beside a detail; Agents is one column. The list's bar, as tall as the title bar, names the page with a count line and holds the page's actions. The detail's bar holds the actions on what it shows: icon buttons with tooltips, then the primary action. Groups of rows are cards with hairline separators, and menus are native.
- The list's selection is the accent while the list has the keyboard, and gray otherwise, in place of a focus ring. In an inactive window, the accent of selections, the primary action and the chosen page's icon fades to gray, as in native apps.
- Light and dark mode, and the accent, follow the system. Text sizes are steps from the system's body size times the desktop's text scale (GNOME's and Windows's text size), and controls and rows grow with their text.

## Data

The pages show what is read in the background: the library, the places (global, and every known project that still exists), the status of each, and the agents. It is read again when the app becomes active and after each change, and the pages keep showing the last read meanwhile. A project left with nothing enabled leaves the list, as in the CLI (`projects` in `storage.md`).

## Skills

- The list holds every library skill, filtered by a search field (name, description, source) and by `All`, `Enabled` (anywhere) or `Unused` (nowhere). A row shows the name (with its folder when names repeat, `library.md`), badges (`Invalid`, `Update`, `Outdated`, `Modified`), the source label and where the skill is enabled (`Global, 2 projects`, `Not enabled`).
- The list's bar has `Check for Updates`, which checks every updatable skill with progress, and `Update All` while the check found updates.
- The detail shows the name, description, source (a link to the repository page for git sources) and library path. Its bar has Reveal in Finder, Open SKILL.md and Remove from Library… as icon buttons, then `Update` for a skill that can update or has copies; `Update` is the primary action while the last check found an update. Notes say why the skill is invalid, that the library copy has local changes, that it was removed upstream, or that the check failed.
- `Enabled in` has a row for Global, always, and one per project that has the skill, then `Enable in Project…`: a menu of the known projects that don't have it, and `Choose Folder…`, resolved as a project scope (`enabling.md`).
- Then SKILL.md: the optional frontmatter fields that are set and the rendered body, which is selectable and whose links open in the browser.
- `Update` is the TUI's `u`: check, update the library, then refresh the skill's outdated copies in every place, each step asking before it discards local changes. `Update All` applies every update found, then refreshes the outdated copies of the updated skills.

## Placement rows

A skill's `Enabled in` and a place's skill list on Projects share one row:

- The place (or, on Projects, the skill), the state (`Outdated` with `Refresh`, `Modified`), a mode menu (`Link`, `Copy`; `Mixed` shows as is), an agents popover with a check box per agent skill directory (agents sharing one are one item), and `Disable in <place>`. The controls wrap under the place when the pane is narrow.
- Global without the skill shows `Not enabled` and `Enable`, which links it for the default agents.
- An action that deletes copies (disabling, switching to links, unchecking an agent) asks first, naming them; for modified copies it says their changes are lost.

## Projects

- The list holds Global and the known projects, each with its name, path, number of skills, and a badge when one needs attention.
- The detail shows the place's name and path, Reveal in Finder (Show in Folder off macOS), `Add Skills…` (a searchable checklist of the library skills it doesn't have, a mode, and `Enable`), and `Refresh Copies` while some copies are outdated and unmodified.
- Then its skills as placement rows (the name opens the skill on Skills), its broken links with `Remove`, and the entries MySkills doesn't manage, listed and never touched.

## Find

- The search field takes a skills.sh query, `owner/repo`, a git URL or a folder path, beside `Choose Folder…`. It follows the TUI's Find rules: a search term is searched as it is typed; an explicit source is named under the field with `Fetch`, and is never fetched while typing; its skills replace the results. `Choose Folder…` fetches the chosen folder at once, which also covers paths with spaces that a typed source can't express.
- The results sit beside the preview of the selected one: SKILL.md from skills.sh for a search result, from the files for a fetched source. A row shows the name, source and installs (the description, for a fetched source), and `Add` or its state: `In Library`, `Added`, `Conflict` (the library has a skill of that name from another source), `Invalid`.
- Adding asks nothing, since the button says what it does and the preview where from, and it never enables. The preview of a skill in the library offers `Show in Skills`, where it can be enabled. A fetched folder also offers `Link Folder` (`--link` in `library.md`), which shows `Linked` once the library has it.
- While a new search runs, the last results stay, dimmed, and can't be added.
- A fetched checkout is removed when the query changes (once the adds from it are done) and on quit.

## Agents

Every agent with its project and global directories. A switch makes an agent a default; the last default stays on. `Add Agent…` adds a custom agent; core validates it, and what core refuses shows in the dialog, which stays open. A custom agent's `Remove…` asks first: its link placements are removed and its copies stay.

## Feedback and work

- A change shows in place. Errors open an alert naming what failed. Warnings (such as the git exclude) and outcomes where nothing changed ("tdd is up to date") show in a toast.
- A skill or a place takes one change at a time, and the change holds until the read after it completes, so no control ever shows the state from before the change; a switch or check box shows what it was set to meanwhile. Work running past 300 ms disables those controls with a spinner; sooner, the spinner would only flicker.
- Quitting cancels network work and waits for the changes in flight.
