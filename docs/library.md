# Library

How skills are validated, hashed and copied, and how they enter, update in and leave the library.

## Validation

Skills follow https://agentskills.io/specification. The frontmatter is read as a `yaml.Node`, so scalar types are checked rather than coerced.

- `SKILL.md` is UTF-8 and starts with a `---` frontmatter block that is a YAML mapping.
- `name`: a string of 1–64 characters from `[a-z0-9-]`, with no leading or trailing `-` and no `--`. It equals the directory name.
- `description`: a non-empty string of at most 1024 characters.
- Optional, and type-checked when present: `license` (string), `compatibility` (string, 1–500 characters), `allowed-tools` (string), `metadata` (mapping).
- `metadata` entries whose key and value are scalars are kept for display, as written (`version: 1.0` shows `1.0`). Nested and null values are skipped rather than rejected: clients keep their own data there (`metadata.openclaw: {…}`), and agents load such skills.

A library scan reports each invalid skill with the reason, and goes on.

## Content hash

`v1:<hex sha256>` over the entries below the skill directory, sorted by relative path, skipping `.git` and `.DS_Store`. Each entry contributes a kind byte (`F`, `D` or `L`) and its length-prefixed path; a file adds its executable bit and length-prefixed content, a symlink its length-prefixed target. Windows keeps no permission bits, so hashes there see no executable files.

## Copy rules

The same rules apply to installing into the library and to copy placements.

- Regular files keep their executable bit, directories recurse, and `.git` and `.DS_Store` are skipped.
- A symlink whose target stays inside the skill is kept as a relative symlink.
- A symlink that points outside the skill but inside its root is dereferenced, so the copied skill is self-contained. The root is the repository of a git source and the owning entry of a library skill. A skill from a local folder has none: only its own folder is recorded, and an update check reads nothing else.
- Any other symlink (absolute and outside, escaping the root, or broken) fails the operation with its path, before anything is written.
- A library skill _hashed as a copy_ gives the hash its copy would have, without copying: the same rules, with dereferenced content in place of those links. A change behind such a link therefore shows the skill's copies as outdated.
- On Windows, copying a skill that contains a symlink needs Developer Mode.

## Discovery

Discovery finds the candidate skills in a fetched repository or a local folder.

- A start directory with `SKILL.md` is the only candidate.
- Otherwise the walk goes down to depth 6, skipping `.git` and `node_modules`. A directory with `SKILL.md` is a candidate and is not descended. Hidden directories are walked, since skills often live in `.claude/skills` or `.agents/skills`.
- When looking for named skills (`@skill`, `-s`, a skills.sh result), the walk has no depth limit: the name is known, and skills.sh lists skills at any depth without their path.
- Candidates that resolve to the same real directory count once.
- Candidates that share a name are offered at the most preferred place only: under `skills/`, then `.agents/skills/`, then another `.<agent>/skills/`, then anywhere else. A repository serving several agents keeps a copy of its skill in each agent's folder, and the universal `.agents/skills` copy is the one that suits a single library copy enabled for every agent. Several at that place are all offered.

## Sources

| Input | Meaning |
|---|---|
| `owner/repo`, `owner/repo/sub/dir` | GitHub |
| `owner/repo@skill` | GitHub, only the skill named `skill` |
| `https://github.com/o/r[/tree/<ref>[/sub]]` | GitHub URL; the first segment after `tree` is the ref, and `--ref` covers refs containing `/` |
| `https://gitlab.com/g/…/r[/-/tree/<ref>[/sub]]` | GitLab URL |
| `git@host:…`, `ssh://…`, `https://….git`, `file://…` | any git remote |
| `<remote>#<ref>` | a branch or tag |
| `./x`, `../x`, `/x`, `~/x`, `C:\x`, `\\server\x` | a local folder; only these explicit path forms |

## Fetching a git source

A fetch downloads only what it reads, so adding one skill from a large repository is quick.

- `git clone --depth 1 --single-branch --filter=blob:none --no-checkout [--branch <ref>]` downloads the commit and its file list; `git rev-parse HEAD` gives the commit.
- A non-cone sparse checkout, with one exact pattern per path, checks out files as they are needed, each batch in one download. Discovery first checks out every symlink and every `SKILL.md`; installing then checks out the chosen skill folders. A symlink inside a checked-out path pulls in what it points to elsewhere in the repository, which the copy rules then dereference.
- A server without partial clone support sends everything, with the same result.
- The user's git credentials are used. An HTTP transfer slower than 1 KB/s for 30 s fails rather than hangs. Temp directories are always removed.
- The checkout folder is named after the repository (the last URL path segment without `.git`). A root skill whose `SKILL.md` is invalid is therefore listed, and matched by `-s`, under the repository name.

## skills.sh

skills.sh is used only to search and preview.

- `GET https://skills.sh/api/search?q=<q>&limit=<n>` returns `{skills:[{id, source, skillId, name, installs}]}`. `source` is a GitHub `owner/repo`, `skillId` the skill name, and `name` may be a display name. A result whose `source` is not `owner/repo`, or that has no `skillId`, is dropped.
- `GET https://skills.sh/api/download/<owner>/<repo>/<skillId>` returns `{files:[{path, contents}], hash}`; the frontends preview its `SKILL.md`.
- Adding a result adds `<source>@<skillId>` through git, so every remote skill has the same record and the same update path.

## Adding

`add` never asks for confirmation. Several candidates without `-s` or `--all` open a picker in a terminal, and are an error listing them otherwise. `--all` adds the valid candidates and reports the invalid ones as failed.

- A skill is installed at `skills/<name>`, so its name must be valid. It is copied into `skills/.incoming-<rand>` with the copy rules, hashed, renamed into place and recorded, all under the mutation lock.
- When `skills/<name>` exists: from the same source (url, ref and path, or the local path), it is "already in library" and `update` is suggested; from another source, it is a name conflict. Nothing changes either way.
- A requested name (`@skill`, each `-s`) matches a candidate's frontmatter name or folder name, ignoring case. A name that matches none is a not-found error listing the candidates. A named candidate that is invalid fails with its validation error.
- A name that matches several candidates is an ambiguity error listing their paths.
- A local folder is copied by default, with the record `{type: local, path: <candidate dir>}`. `--link` instead creates `skills/<basename>` as a symlink to the absolute folder (a skill or a collection), with no record. A local path inside the library is rejected. A collection whose skills share files through symlinks is added with `--link`.

## Updates

`check` finds updates without changing anything:

- Git skills are grouped by url and ref. `git ls-remote` gives the remote commit: `HEAD` without a ref; otherwise `refs/heads/<ref>`, then `refs/tags/<ref>` (peeled `^{}` when present).
- The recorded commit means up to date. Otherwise the group is cloned once, as `add` does, checking out only the recorded paths, and each skill is hashed: the recorded hash means up to date (and the new commit is recorded), a different hash means an update is available, and a missing path means removed upstream.
- A local skill is compared by hashing its source folder, when it still exists. Linked and record-less skills are not updatable.
- Groups are checked four at a time.

`update` runs the check, then installs each update from the same clone it checked:

- The new version is staged, then swapped in: the old folder is renamed to `.old-<rand>`, the new one moved into place, and the old one removed; a failure restores the old folder.
- A skill whose record changed since the check (another process updated it) is skipped, even with `--force`. Right before the swap the folder is checked again: a locally modified skill is skipped unless `--force`; a folder replaced by another meanwhile is skipped even with it.
- It then refreshes the outdated, unmodified copies of the selected skills (every library skill, updatable or not, when none were named) in the current project, if there is one, and in global scope.
- Skips are reported and make the exit code 1. "Removed upstream" is a warning.

## Removing

`remove` deletes the folder of each named skill and the collection folders that this leaves empty. A skill behind a symlink in the library (a linked entry, or a link inside a collection) is removed by removing that symlink, with every skill behind it; the files the symlink points to stay. The link placements of the removed skills are removed in every known scope. Their copies stay as unmanaged files, and their records are dropped. The CLI confirms in a terminal and needs `-y` otherwise.

## Names and labels

A skill is shown by its name. When several library skills share that name, the folder of its library path follows it, dimmed (`review a`); a top-level skill has no folder to show. The lists of the CLI, the TUI and the desktop app all follow this. The CLI's result lines (`added`, `linked`, `updated`, `removed` and the like) print the library path, which is the reference the next command takes.

A skill's source label is `owner/repo` for git, the linked folder (`~/agent-skills`) for a skill inside a linked entry, and `local` otherwise. `myskills show` and the TUI preview show the folder of a local source instead of `local`.
