# Enabling

How library skills are placed into agent skill directories, kept consistent, and read back as status.

## Agents

| id | name | project dir | global dir |
|---|---|---|---|
| `universal` | Universal | `.agents/skills` | `~/.agents/skills` |
| `claude-code` | Claude Code | `.claude/skills` | `$CLAUDE_CONFIG_DIR/skills`, default `~/.claude/skills` |
| `antigravity` | Antigravity | `.agents/skills` | `~/.gemini/config/skills` |

`.agents/skills` is read by Codex and Antigravity (per their official docs), and per their own docs by Cursor, Gemini CLI, Copilot, Amp, Goose and OpenCode. Claude Code reads only `.claude/skills`. Any other agent is a custom agent (`storage.md`). The order of the agents is the order of display and of deduplication.

**Targets.** The agents of an enablement are the `-a` ones or, without `-a`, the configured defaults. An `-a` agent that has no directory for the scope is an error; a default agent without one is skipped. Directories are deduplicated by resolved path, with parent symlinks resolved (`.claude/skills` → `.agents/skills`), and the first agent wins. No target directory left is an error.

A placement belongs to every known agent whose directory resolves to its directory. Results of enable, disable and copy refreshes list all of those agents, in agent order: a link made in `.agents/skills` for Universal is reported for Universal and Antigravity. For the same reason, the per-skill agent choices in the TUI and the desktop app show agents that share a directory as one item.

**Removing a custom agent** first removes its link placements, globally and in every known project. Its copies stay as unmanaged files. Placements in a directory it shares with another agent stay managed.

## Scope

- `-g` selects global scope.
- Otherwise the start directory is `-C <dir>` or the working directory, and the project root is `git rev-parse --show-toplevel` from there, or the start directory itself outside a git work tree.
- A root equal to the home directory or `/` is not a project. Commands that need a project fail with a hint to use `-g`; the TUI opens in global scope with the project unavailable; the desktop app refuses such a folder.

## Placements

**link**: `<dir>/<name>` is a symlink to `<library root>/<library path>`, absolute and not resolved through linked entries. A missing agent directory is created.

- A link is refused where git tracks the placement path (`git ls-files`): a personal link must not replace committed content. The error explains how to untrack it.
- On Windows a link is a directory junction, which needs no privileges, made with `mklink /J` through `cmd`. `cmd` expands `%VAR%` even inside quotes, so paths containing `%` are refused. A junction cannot point at a network share, so links into a library on one fail.

**copy**: each target directory gets its own full copy, made with the copy rules (`library.md`) and recorded in lock.json's `copies`.

## Git exclude

In a git work tree, link placements are kept out of git by a managed block in the file `git rev-parse --git-path info/exclude` names, which all worktrees of the repository share:

```
# >>> myskills (managed, do not edit)
/.agents/skills/tdd
/.claude/skills/tdd
# <<< myskills
```

- After every project mutation the block is regenerated from the filesystem, never patched: every link placement under every known agent's project directory, in every worktree (`git worktree list --porcelain`), as a root-anchored path with glob metacharacters escaped.
- A directory reached through a symlinked parent (`.claude/skills` → `.agents/skills`) is listed at its target and under every configured spelling. Git for Windows walks into junctions and sees the shared directory under both paths, so listing every spelling keeps both ignored there; copies show twice in its status.
- Content outside the block is kept, and an empty block is removed. Failures are reported as warnings.

## Enable and convert

- `enable` without a mode flag on a skill that is already enabled adds placements for target agents that lack one, in the current mode, and never converts. A new enablement is a link.
- `--link` or `--copy` converts existing placements to that mode.
- `--copy` on an unmodified copy whose library content changed refreshes it.
- Deleting a modified copy (current hash differs from its record) fails with `ErrModified` unless `--force`.

## Conflicts and concurrent edits

- A placement path occupied by anything MySkills doesn't manage is `ErrConflict`, naming the occupant, and its library path when it is a link to another library skill. `--force` never overrides a conflict.
- A broken link to the same library path is repaired. A broken link to another library path is a conflict; the user disables it by that path first.
- Every target is checked before anything changes. Right before each placement is swapped in or removed (enable, disable, refresh), it is checked again: it must still be absent, the same link, or the same copy directory with its recorded content. An edit made in between fails that placement instead of being lost. `--force` waives only the content check of a copy found modified, never the check that it is still the same directory.

## Disable

- `disable` removes the skill's placements in the scope (for every agent, or the `-a` ones), drops their copy records and regenerates the exclude block.
- The CLI removes unmodified copies without asking, since the command is explicit; the TUI and the desktop app confirm deleting copies.
- `disable <name>` also matches broken placements whose library skill no longer exists. A reference that is the missing library path of a broken placement names that placement, even when it is also the name of library skills.

## Status

Status classifies every entry of every known agent directory (built-in and custom) in the scope:

- `link`: a symlink whose literal target is absolute, lies under the library root (checked lexically against the root path and its resolved path), and exists. It maps to that library path.
- `broken`: such a link whose target is missing.
- `copy`: a real directory recorded in `copies`, flagged `modified` when its hash differs from `hash`, and `outdated` when the library skill hashed as a copy differs from `libraryHash`.
- `other`: anything else. It is shown and never touched.

Per skill, status gives the placements, their agents, the mode (`link`, `copy` or `mixed`), and `partial` when a default agent that supports the scope has no placement.
