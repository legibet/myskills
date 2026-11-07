package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/legibet/myskills/internal/git"
)

// The managed block in .git/info/exclude (design §6 Git exclude).
const (
	excludeBegin = "# >>> myskills (managed, do not edit)"
	excludeEnd   = "# <<< myskills"
)

// syncExclude regenerates the managed exclude block of the repository holding
// the project root. Failures are returned as warnings: a stale exclude block
// must not fail the operation that placed the links.
func (s *Service) syncExclude(st *state, root string) []string {
	if err := s.writeExclude(st, root); err != nil {
		return []string{"could not update the git exclude file: " + err.Error()}
	}
	return nil
}

func (s *Service) writeExclude(st *state, root string) error {
	file, err := git.GitPath(root, "info/exclude")
	if errors.Is(err, git.ErrNotRepo) || errors.Is(err, git.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// The exclude file is shared by every worktree of the repository.
	worktrees, err := git.Worktrees(root)
	if err != nil {
		return err
	}
	var patterns []string
	for _, wt := range worktrees {
		wtReal := realPath(wt)
		for _, d := range agentDirs(st.agents, Scope{Root: wt}) {
			placements, err := s.scanDir(st.lock, d)
			if err != nil {
				return err
			}
			for _, p := range placements {
				if p.Kind != KindLink && p.Kind != KindBroken {
					continue
				}
				// Where the link physically is, and every configured spelling
				// of its dir: Git for Windows walks into junctions, so with
				// .claude → .agents it sees the link under .claude too.
				var rels []string
				if rel, ok := within(wtReal, filepath.Join(d.real, p.Name)); ok {
					rels = append(rels, rel)
				}
				for _, alias := range d.aliases {
					if rel, ok := within(wt, filepath.Join(alias, p.Name)); ok {
						rels = append(rels, rel)
					}
				}
				for _, rel := range rels {
					if !strings.ContainsAny(rel, "\r\n") {
						patterns = append(patterns, "/"+escapePattern(filepath.ToSlash(rel)))
					}
				}
			}
		}
	}
	slices.Sort(patterns)
	patterns = slices.Compact(patterns)

	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	updated := replaceBlock(string(old), patterns)
	if updated == string(old) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(file, []byte(updated))
}

// escapePattern escapes the gitignore metacharacters in a path so it matches
// literally. A leading "!" or "#" needs no escape: patterns start with "/".
func escapePattern(p string) string {
	var b strings.Builder
	for _, r := range p {
		if strings.ContainsRune(`\*?[ `, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// replaceBlock replaces the managed block in content with one listing
// patterns, or removes it when there are none. Everything outside the block is
// kept as is; a new block is appended at the end.
func replaceBlock(content string, patterns []string) string {
	var block string
	if len(patterns) > 0 {
		block = excludeBegin + "\n" + strings.Join(patterns, "\n") + "\n" + excludeEnd + "\n"
	}
	lines := strings.SplitAfter(content, "\n")
	begin, end := -1, -1
	for i, line := range lines {
		switch strings.TrimRight(line, "\r\n") {
		case excludeBegin:
			begin = i // the last begin before the end, should one be left dangling
		case excludeEnd:
			if begin >= 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		if block == "" {
			return content
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + block
	}
	return strings.Join(lines[:begin], "") + block + strings.Join(lines[end+1:], "")
}

// writeFileAtomic replaces path with data through a temp file in the same
// directory, so git never reads a partial file.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
