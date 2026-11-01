# myskills — design

Cross-platform Go CLI + TUI for managing agent skills (folders with `SKILL.md`).
Linux and macOS are first-class; Windows is best effort. Replaces the macOS SwiftUI app
(archived on the git branch `macos-app`).

## 1. Goals and non-goals

Goals
- A private skill library at `~/.myskills/skills`. Skills enter the library first (from git,
  GitHub, a skills.sh search, or a local folder), then get enabled where needed.
- Enable/disable library skills per project or globally, for the agents the user uses.
- Two enable modes:
  - `link` (default): a symlink into the agent skill dir. Personal; kept out of git.
  - `copy`: real files in the agent skill dir. Owned by the project; meant to be committed.
- A polished TUI for the frequent flows (browse the library, toggle enablement, search and
  add) and complete non-interactive commands for scripts and coding agents.
- Single binary; works without any config file.
- Later, a native cross-platform GUI built with [mygo](https://github.com/egoist/mygo), as
  another frontend over `internal/core`.

Non-goals
- Team sharing: project manifests, project lockfiles, restore/sync commands.
- Driving agents' own enable/disable settings.
- Background update checks, telemetry.
- Migration code from the macOS app inside the tool (done once, out of band).

## 2. Concepts

- **Library**: `$MYSKILLS_HOME/skills` (default `~/.myskills/skills`). Each top-level entry is
  a directory or a symlink to a directory elsewhere (a *linked entry*: a skill under
  development or a whole collection). Collections may nest.
- **Skill**: a directory with `SKILL.md`, identified by its **library path** (relative to the
  library root, `/`-separated: `tdd`, `agent-skills/review`). Its **name** is the frontmatter
  `name` and must equal the directory name. Names may repeat across collections.
- **Owning entry**: the top-level library entry that contains a skill. Removing a skill inside
  a linked entry means removing that entry (the link), never files behind it.
- **Agent**: id, display name, project-relative skill dir, global skill dir (either optional).
- **Scope**: a project root, or global.
- **Placement**: one managed entry `<agent dir>/<name>`, mode `link` or `copy`.
- **Enablement**: all placements of one skill in one scope. Modes can differ per placement
  (shown as "mixed").

## 3. Files

```
~/.myskills/              $MYSKILLS_HOME overrides
  skills/                 library
  config.json             preferences, hand-editable
  lock.json               machine-managed records
  .lock                   inter-process mutation lock
```

The macOS app's `state.json` is never read or written.

### config.json

```json
{
  "agents": ["universal", "claude-code"],
  "customAgents": [
    { "id": "pi", "name": "Pi", "global": "~/.pi/agent/skills" },
    { "id": "grok", "name": "Grok", "project": ".grok/skills", "global": "~/.grok/skills" }
  ]
}
```

- `agents`: default agents for new enablements. Missing → `["universal", "claude-code"]`.
- Custom ids are `[a-z0-9-]+` and must not collide with built-ins. `project` is relative,
  without `..`; `global` is absolute or starts with `~/`. Invalid config → error, no writes.

### lock.json

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

- `skills` keyed by library path. No record → not updatable (hand-made, or inside a linked
  entry). `source.path` is the skill dir inside the repo / the local skill dir itself.
- `hash` = content hash of what was installed (the upstream content). Local modification =
  current hash ≠ recorded hash. `commit` = last upstream commit verified to have that content.
- `copies` keyed by absolute placement path; `hash` = content of the copy as written,
  `libraryHash` = content of the library skill it was copied from, hashed as a copy (§4).
  They can differ (copying normalizes symlinks, the umask may drop permission bits), so each
  is compared with its own side (see §6 Scan).
- `projects`: project roots where myskills placed something. Used for "where is this skill
  enabled" and agent cleanup. A root is dropped only when it exists and holds no managed
  placement (an unmounted root is kept).
- Every mutation runs under an exclusive lock on `.lock` (flock / LockFileEx): load lock.json,
  apply filesystem changes, save (temp file + rename). Network work happens before taking the
  lock; results are revalidated under it. An unreadable or newer-version lock.json/config.json
  stops all mutations with an error; it is never reset.
- If filesystem changes succeed but saving lock.json fails, the error is reported; links stay
  correct (they are derived from the filesystem), an unrecorded copy shows as `other`.

## 4. Skills

Validation (https://agentskills.io/specification), using `yaml.Node` so scalar types are
checked rather than coerced:
- `SKILL.md`, UTF-8, starts with a `---` frontmatter block that is a YAML mapping.
- `name`: string, 1–64 chars, `[a-z0-9-]`, no leading/trailing `-`, no `--`; equals the
  directory name.
- `description`: non-empty string, ≤ 1024 chars.
- Optional, typed when present: `license` (string), `compatibility` (string, 1–500),
  `metadata` (map string→string), `allowed-tools` (string).
- Library scans report invalid skills with the reason; they never abort a scan.

Content hash `v1:<hex sha256>` over entries sorted by relative path (skipping `.git`,
`.DS_Store`). Each entry contributes: kind byte (`F`/`D`/`L`), length-prefixed path, for
files an executable bit and length-prefixed content, for links the length-prefixed target.

Copy rules (used for library installs and copy placements):
- Regular files keep their executable bit; directories recurse; `.git` and `.DS_Store` skipped.
- A symlink whose target stays inside the skill dir is preserved as a relative symlink.
- A symlink pointing outside the skill but inside the source root (repo / local folder) is
  dereferenced (its content copied), making the skill self-contained.
- Any other symlink (absolute outside, escaping the source root, broken) fails the operation
  with the offending path, before anything is written.
- Hashed as a copy, a library skill gives the hash its copy has, without copying: the same
  rules, with dereferenced content in place of the links. A change behind such a link thus
  shows its copies as outdated.

Discovery in a fetched repo or a local folder:
- Start dir has `SKILL.md` → the only candidate.
- Else walk up to depth 6, skipping `.git` and `node_modules`; a dir with `SKILL.md` is a
  candidate and is not descended. Hidden dirs are walked (`.claude/skills`, `.agents/skills`).
  Candidates resolving to the same real dir are deduplicated.

## 5. Adding to the library

Source spec:

| Input | Meaning |
|---|---|
| `owner/repo`, `owner/repo/sub/dir` | GitHub |
| `owner/repo@skill` | GitHub, only the skill named `skill` |
| `https://github.com/o/r[/tree/<ref>[/sub]]` | GitHub URL (first segment after `tree` = ref; use `--ref` for refs with `/`) |
| `https://gitlab.com/g/…/r[/-/tree/<ref>[/sub]]` | GitLab URL |
| `git@host:…`, `ssh://…`, `https://….git`, `file://…` | any git remote |
| `<remote>#<ref>` | branch or tag |
| `./x`, `../x`, `/x`, `~/x`, `C:\x`, `\\server\x` | local folder (explicit path forms only) |

Flags: `-s/--skill <name>` (repeatable), `--all`, `--ref <ref>`, `--link` (local only).
`add` never asks for confirmation; `--all` adds the valid candidates and reports invalid ones
as failed (exit 1).

Remote fetch: `git clone --depth 1 --single-branch [--branch <ref>] <url> <tmp>`, then
`git rev-parse HEAD`. Uses the user's git credentials; temp dirs always removed.

skills.sh is used for search and preview only:
- `GET https://skills.sh/api/search?q=<q>&limit=<n>` → `{skills:[{id, source, skillId, name,
  installs}]}`; `source` = GitHub `owner/repo`, `skillId` = skill name, `name` may be a
  display name.
- `GET https://skills.sh/api/download/<owner>/<repo>/<skillId>` → `{files:[{path, contents}],
  hash}`; the TUI previews its `SKILL.md`.
- Adding a result = adding `<source>@<skillId>` through git, so every remote skill has the
  same record shape and update path. Base URL is injectable for tests.

Install of a candidate:
- Validate (name must be valid; destination dir name = name). Destination `skills/<name>`.
- Copy into `skills/.incoming-<rand>` with the copy rules, hash, rename into place, write the
  record — all under the mutation lock.
- `skills/<name>` exists: same source (url + ref + path, or local path) → "already in
  library" (suggest `update`), else name conflict error. Nothing is changed.
- `-s name` matching several candidates → ambiguity error listing their paths.
- Several candidates without `-s`/`--all`: TTY → picker; non-TTY → error listing them.

Local folders: default copies each chosen candidate, record `{type: local, path: <candidate
dir>}`. `--link` creates `skills/<basename>` → absolute folder (a skill or a collection); no
record. A local path inside the library is rejected.

## 6. Enabling

### Agents

| id | name | project dir | global dir |
|---|---|---|---|
| `universal` | Universal | `.agents/skills` | `~/.agents/skills` |
| `claude-code` | Claude Code | `.claude/skills` | `$CLAUDE_CONFIG_DIR/skills`, default `~/.claude/skills` |
| `antigravity` | Antigravity | `.agents/skills` | `~/.gemini/config/skills` |

`.agents/skills` is read by Codex (official docs), Antigravity (official docs), and per their
docs Cursor, Gemini CLI, Copilot, Amp, Goose, OpenCode. Claude Code reads only
`.claude/skills` (official docs). Other agents are custom.

Target resolution: agents = `-a` (repeatable, comma-separated allowed) or `config.agents`.
A `-a` agent without a dir for the scope → error. Config agents without one are skipped.
Dirs are deduplicated by resolved path (parent symlinks resolved, e.g. `.claude/skills` →
`.agents/skills`), first wins. An empty target list → error.

A placement belongs to every known agent whose dir resolves to its dir. Results of enable,
disable and copy refreshes list all of them, in agent order like status: a link made in
`.agents/skills` for Universal is reported for Universal and Antigravity.

### Scope
- `-g/--global` → global.
- Else: start dir = `-C <dir>` or cwd; project root = `git rev-parse --show-toplevel` from the
  start dir, or the start dir if not in a git work tree. A root equal to the home directory
  (or `/`) is not a project: commands that need a project error with a hint to use `-g`; the
  TUI opens in global scope with project unavailable.

### link
- `<dir>/<name>` → symlink to `<library root>/<library path>` (absolute, not resolved through
  linked entries). Agent dir created if missing. Windows: directory junction.
- Refused if the placement path is tracked by git (`git ls-files`): a personal link must not
  replace committed content; the error explains how to untrack it.

### copy
- Each target dir gets its own full copy (copy rules, §4), recorded in `lock.copies`.

### Git exclude (project scope in a git work tree)
- Managed block in the file given by `git rev-parse --git-path info/exclude` (shared by all
  worktrees of the repository):
  ```
  # >>> myskills (managed, do not edit)
  /.agents/skills/tdd
  /.claude/skills/tdd
  # <<< myskills
  ```
- After any project mutation the block is **regenerated**: for every worktree
  (`git worktree list --porcelain`), every link placement under every known agent project
  dir, as a root-anchored path with glob metacharacters escaped. Content outside the block is
  preserved; an empty block is removed. Failures are reported as warnings.

### Mode changes and idempotence
- `enable <skill>` (no mode flag) when already enabled: ensure placements for missing target
  agents in the current mode; never converts. New enablement uses `link`.
- `--link` / `--copy` converts existing placements to that mode.
- `--copy` on an existing unmodified copy whose library content changed → refresh.
- Deleting a modified copy (current hash ≠ record) → `ErrModified` unless `--force`.
- A placement path occupied by anything myskills doesn't manage → `ErrConflict` naming the
  occupant (and its library path when it is a link to another library skill). A broken link
  to the same library path is repaired; one to another library path is a conflict (disable it
  by that path first). `--force` never overrides a conflict.
- Every target is checked before anything changes. Right before each placement is swapped in
  or removed (enable, disable, refresh), it is checked again: still absent, the same link, or
  the same copy directory with its recorded content. An edit made meanwhile fails that
  placement instead of being lost. `--force` waives only the content check of a copy found
  modified, never the check that it is still the same directory.

### Disable
- Removes the skill's placements in the scope (all agents, or the `-a` ones), copy records,
  regenerates the exclude block. Unmodified copies are removed without asking in the CLI (the
  command is explicit); the TUI confirms copy deletion.
- `disable <name>` also matches broken placements whose library skill no longer exists. A ref
  that is the missing library path of a broken placement names that placement, even when it
  is also the name of one or more library skills.

### Scan / status
Classify every entry of every known agent dir (built-in + custom) in the scope:
- `link`: symlink whose literal target (absolute) lies under the library root (lexical check
  against the root path and its resolved path) and exists → library path.
- `broken`: such a link whose target is missing.
- `copy`: real dir recorded in `lock.copies`; flags `modified` (current hash ≠ `hash`),
  `outdated` (the library skill hashed as a copy ≠ `libraryHash`).
- `other`: anything else; shown, never touched.
Per skill: placements, agents, mode (`link`/`copy`/`mixed`), `partial` when a default agent
that supports the scope lacks a placement.

## 7. Updates

- `check [skill…]`: git skills grouped by (url, ref). Remote commit via `git ls-remote`:
  no ref → `HEAD`; else `refs/heads/<ref>`, then `refs/tags/<ref>` (peeled `^{}` when present).
  Same as recorded commit → up to date. Otherwise shallow-clone once per group, hash each
  recorded `path`: equal → up to date (record the new commit); different → update available;
  missing → removed upstream. Local skills: hash the source dir if it exists. Linked /
  record-less skills: not updatable. Parallelism 4. `--json` supported.
- `update [skill…]`: run check; install updates from the same clone that was checked
  (stage, then swap: rename old → `.old-<rand>`, new → dest, remove old; restore on failure).
  A skill whose record changed since the check (another process updated it) is skipped,
  even with `--force`. Right before the swap the skill folder is checked again: a locally
  modified one is skipped unless `--force`, one replaced by another folder meanwhile even
  with it. Then refresh outdated, unmodified copies of the selected skills (all library
  skills, updatable or not, when none are named) in the current project (if any) and global
  scope. Skips are reported; exit code 1 if anything was skipped or failed. "Removed
  upstream" is a warning.

## 8. Commands

```
myskills                         TTY: TUI. Non-TTY: help.
myskills find [query]            TTY: TUI Find view. Non-TTY: results (`--json`).
myskills add <source>            §5
myskills list | ls               library: name, source, where enabled, issues (--json)
myskills show <skill>            metadata + rendered SKILL.md
myskills path <skill>            absolute library path (exact bytes when piped)
myskills remove | rm <skill>…    remove owning entries; disables their link placements in all
                                 known scopes; copies stay as unmanaged files (records dropped).
                                 Confirms in TTY; non-TTY requires -y.
myskills enable [skill…]         -g, -C dir, -a agent, --link | --copy, --force
myskills disable [skill…]        -g, -C dir, -a agent, --force
                                 enable/disable without args: TUI (TTY) or error (non-TTY)
myskills status                  current project and global placements (--json)
myskills check [skill…]          (--json)
myskills update [skill…]         --force
myskills agents                  list agents, defaults, dirs
myskills agents use <id>…        set default agents
myskills agents add <id> [--name N] [--project P] [--global G]
myskills agents remove <id>      removes its link placements (global + known projects) first;
                                 its copies stay as unmanaged files. Placements in a dir it
                                 shares with another agent stay managed.
myskills completion <shell>      dynamic completion of skills and agents
myskills --version
```

- `<skill>` = library path or name; an ambiguous name errors with the candidates.
- `list`, `status` and `show` name a skill by its name, followed (dim) by its library path
  only when several library skills share that name. Source labels: `owner/repo` for git, the
  linked folder (`~/agent-skills`) for skills in a linked entry, `local` otherwise; `show`
  prints the folder of a local source instead of `local`.
- Output: one line per item, `✓` / `!` / `✗` + dim details; colors follow the terminal
  (none when piped, `NO_COLOR` respected). `✓`/`!` lines to stdout, `✗` lines and hints to
  stderr. Exit 1 if any item failed or was skipped for local changes; warnings (e.g. `disable`
  of a skill that is not enabled) keep exit 0. Lines state what was done or found; next steps
  are suggested only with errors, skipped items and an empty library.
- Untrusted text (skill metadata and markdown, skills.sh results, entry names found on disk,
  errors quoting them) never reaches the terminal as escape sequences: every printed line,
  spinner message and rendered markdown after Glamour (which decodes entities like `&#27;`)
  keeps only SGR styling. JSON output, and `path` when stdout is not a terminal, are
  unchanged data.
- Ctrl-C cancels network work and a pending prompt, and stops before the next item of a
  multi-item command; a second Ctrl-C exits immediately.

## 9. TUI

Bubble Tea v2, Lip Gloss v2, Bubbles v2, Glamour v2. Alt screen; on exit a summary of the
changes made is printed to the normal screen.

```
 myskills   Skills · Find                     ▸ project ~/projects/foo     global: 3
 ──────────────────────────────────────────────────────────────────────────────────
 All 4   Enabled 2   Issues 0  / filter    │ swiftui-expert-skill
▌● swiftui-expert-skill  avdlee/…    link  │ avdlee/swiftui-agent-skill
 ◆ tdd  mattpocock/skills  outdated  copy  │ ● link · Universal, Antigravity, Claude Code
 ○ write  tw93/Waza                global  │ ────────────────────────────────────────────
 ○ review  ~/agent-skills                  │ Enabled in ~/projects/foo, ~/opcode
 …                                         │ Write, review, or improve SwiftUI…
                                           │ (frontmatter fields, rendered SKILL.md)
 ──────────────────────────────────────────────────────────────────────────────────
 ✓ Enabled tdd (copy) in Universal, Antigravity, Claude Code
 space link  c copy  enter read  / filter  f find  u update  a agents  s scope  ? help
```

- Layout: list + preview at ≥ 100 columns; narrower: list only. Resizes live.
- Reader: `enter`/`l`/`→` opens the highlighted row's preview full screen (in both layouts),
  `esc`/`h`/`←` returns. It scrolls with `↑↓`/`jk`, `ctrl+d`/`ctrl+u` (half page),
  `ctrl+f`/`ctrl+b` or `pgdn`/`pgup` (page), `g`/`G`, and keeps the row actions that leave
  the skill in view: `space`, `c`, `a`, `u`, `e` in Skills; `space`, `o` in Find. It closes
  when its row leaves the list (e.g. disabled on the Enabled tab).
- Header: the active scope is prominent; the other scope is summarized (e.g. `global: 3`).
  `s` switches scope (only when a project is available).
- Preview: name, source (`show`'s source line, §8), state in the active scope; notes (invalid,
  broken, conflicts, local changes, outdated copies, update check); `Enabled in <scopes>`;
  then SKILL.md: the description and the optional frontmatter fields that are set (license,
  compatibility, allowed-tools, metadata entries), and the rendered body. Find and the add
  picker preview SKILL.md the same way.
- Help bar: the keys that work for the highlighted row, in priority order, as many as fit the
  width, always ending with `? help` (except in text inputs). `?` shows the full help, in fewer
  columns when narrow; `?` or `esc` closes it.
- Status lines and preview notes state what happened or what is; keys are left to the help bar.

Skills view
- Rows: every library skill: glyph, name, source label (§8; the library path only for a name
  several skills share), flags (`outdated`, `modified`, `update`, `invalid`) and a right
  column saying how the skill is enabled in the active scope: `link`, `copy`, `mixed`, or
  `global` for a project that gets it only from the global scope; `broken`, `conflict` or
  `unmanaged` when something is wrong. Glyph for the active scope: `●` link, `◆` copy, `◐`
  mixed or partial, `○` off, `✗` broken/conflict.
- `space`: off → enable as link (default agents); enabled → disable.
- `c`: off → enable as copy; link or mixed → convert to copies; copy → convert to links.
- `a`: per-skill agent checklist for the active scope (toggle placements per agent). Agents
  sharing a skill directory are one item, since they share its placement.
- `A`: default agents preference (writes config).
- `u`: update the highlighted skill: the library update, then the copy refresh of each scope.
  Each step asks before discarding its own local changes, and confirming forces only that
  step. `U`: check every updatable skill for updates (async; header spinner).
- `r`: reload the library, the scopes and changed SKILL.md files from disk; also done when the
  terminal regains focus.
- `d`/`x`: remove from library (modal confirm). `e`: edit SKILL.md in `$VISUAL`/`$EDITOR`.
- `/`: fuzzy filter; `tab`/`shift+tab` cycle All/Enabled/Issues. Issues lists invalid skills,
  broken and conflicting placements, modified copies, and unmanaged (`other`) entries; `space`
  removes a broken link, unmanaged entries are never touched.
- `ctrl+d` / `ctrl+u`: scroll preview.
- Empty library: a friendly empty state pointing to Find and `myskills add`.
- Destructive actions (removing a copy, `ErrModified`) open a confirm modal.

Find view (`f` from Skills; `myskills find` opens here)
- Levels: Skills → query input → result list → reader. `enter` goes one level in, `esc`
  one level out (`h`/`←` too, except in the input, where they edit).
- The query input has the focus when Find opens, and takes every key but `enter` (submit and
  go to the list), `↓` (to the list) and `esc` (back to Skills). `myskills find <query>`
  opens with the query submitted.
- The list shows what the query gives:
  - A search term: a debounced skills.sh search (≈250 ms, ≥ 2 chars); `enter` searches
    without waiting. Stale responses are dropped by request id.
  - An explicit source (URL, `owner/repo` with a slash, a path): the line under the input
    names it (the git URL or the folder), and `enter` fetches it (a clone, so never while
    typing); the list then holds its skills (preview from local files). Going back to the
    input keeps them; editing the query drops them.
  - Errors show in place of the list; `enter` in the input or `r` in the list retries.
- The list: `↑↓`/`jk` move, `enter`/`l`/`→` read, `space` add, `o` open in the browser,
  `r` retry; `esc`/`h`/`←`, `/` and `↑` on the first row go back to the input.
- A paste goes to the input; pasted while the list has the focus, it replaces the query.
- Rows: name, `owner/repo`, state (`✓ in library`, `✓ added`, `✗ conflict`, `✗ invalid`) and
  the installs (`12.3K`) at the right edge, where they line up. A short row shortens the name
  and source first; when that leaves too little room, the installs are dropped before the state.
- `space` asks `Add <skill> to the library?` with the git URL it downloads from (or the local
  folder), then adds it (row spinner → `✓ added`). A skill already in the library, or whose
  name the library has from another source, is reported instead. Adding never enables.
  `o` opens the repository page (a search result carries no path within it).

Add picker (`myskills add <source>` with several candidates): checklist + preview,
`space` toggle, `ctrl+a` all, `enter` install, `esc` cancel.

Behavior rules
- Mutations run as commands; while one runs for a skill, its keys are ignored (no duplicate
  work). Network requests are cancellable and never block input.
- Untrusted text is sanitized as in §8 (`internal/sanitize`): names and paths show on one
  line, and every frame of the app and the add picker keeps only SGR.
- Quitting waits for the mutations in flight; one whose command never ran is dropped, and
  checkouts of browsed sources are removed.
- One palette with light/dark variants chosen from the terminal background, one accent color,
  muted secondary text, semantic success/warning/error. No Nerd Font glyphs.

## 10. Architecture

```
main.go                cli.Execute()
internal/
  skill/   validate, hash, discover, copy tree — filesystem only, no state
  source/  source spec parsing, labels and web URLs
  git/     clone, ls-remote, rev-parse, ls-files, worktrees
  skillssh/ skills.sh search and download client
  sanitize/ untrusted text made safe for the terminal (keeps only SGR styling)
  agent/   Agent, built-ins, custom agent validation, dir per scope
  store/   paths, config.json, lock.json, mutation lock, atomic writes
  core/    the domain service shared by all frontends: library (scan, resolve, add, remove),
           enablement (scope, enable, disable, scan, exclude block), updates (check, update).
           context.Context on network work; typed results; sentinel errors
           (ErrConflict, ErrModified, ErrAmbiguous, ErrNotFound, ErrNoProject).
  cli/     cobra commands, plain + JSON output
  tui/     Bubble Tea app: skills view, find view, picker, modals, theme, keys
```

Dependencies: `github.com/spf13/cobra`; `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`,
`charm.land/bubbles/v2`, `charm.land/glamour/v2`; `go.yaml.in/yaml/v3`;
`github.com/sahilm/fuzzy`; `golang.org/x/sys` (locks on Windows). `git` is required for remote
sources and git-aware behavior (missing git: remote add fails clearly; exclude/tracking checks
are skipped with a warning).

## 11. Platforms
- Linux, macOS: symlinks, flock.
- Windows: junctions for links, LockFileEx, drive/UNC local paths; names that are invalid
  Windows filenames fail clearly. Junctions are made with `mklink /J` through `cmd`, which
  expands `%VAR%` in paths. Cross-compiled (`GOOS=windows go build ./...`), not tested on
  Windows yet.

## 12. Testing
- Unit: validation (typed YAML), hash (ambiguity, exec bit), discovery, copy rules (symlink
  policy), source spec parsing, ls-remote resolution, agent resolution, exclude block
  rendering/escaping, lock/config round-trip and refusal on bad files.
- Core integration with temp `HOME`/`MYSKILLS_HOME` and local git repos via `file://`:
  enable/disable both modes, conversions, conflicts, modified copies, tracked-path refusal,
  linked entries and collections, parent-symlinked agent dirs, worktree exclude regeneration,
  update with interrupted swap, concurrent mutations (lock).
- skills.sh client against `httptest`.
- CLI: in-process with temp env; output, JSON, exit codes, non-TTY errors.
- TUI: model tests driving key messages for the main flows.
- `go vet`, `golangci-lint run`, `go test -race ./...` on macOS and Linux.
