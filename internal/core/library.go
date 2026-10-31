package core

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

// Skill is a skill in the library.
type Skill struct {
	Path        string // library path, e.g. "tdd" or "agent-skills/review"
	Name        string // frontmatter name; the directory name when invalid
	Description string
	Dir         string // absolute directory through the library root
	Entry       string // top-level library entry that owns the skill
	Linked      bool   // Entry is a symlink to a folder outside the library
	LinkTarget  string // the folder a linked Entry points to
	Issue       string // why the skill is unusable; "" when valid
	Record      *store.SkillRecord
	Modified    bool // content differs from Record.Hash
}

// Valid reports whether the skill can be enabled.
func (s Skill) Valid() bool {
	return s.Issue == ""
}

// Updatable reports whether the skill has a source to update from.
func (s Skill) Updatable() bool {
	return s.Record != nil && !s.Linked
}

// SourceLabel is a short description of where the skill came from:
// "owner/repo" for git sources, the folder of a linked entry
// ("~/agent-skills"), or "local".
func (s Skill) SourceLabel() string {
	switch {
	case s.Linked:
		return tilde(s.LinkTarget)
	case s.Record != nil && s.Record.Source.Type == "git":
		return source.Label(s.Record.Source.URL)
	}
	return "local"
}

// DuplicateNames returns the names that more than one library skill has.
// Frontends show a skill's library path only next to these names.
func DuplicateNames(lib []Skill) map[string]bool {
	seen, dup := map[string]bool{}, map[string]bool{}
	for _, sk := range lib {
		if seen[sk.Name] {
			dup[sk.Name] = true
		}
		seen[sk.Name] = true
	}
	return dup
}

// Library scans the library and returns its skills sorted by name, then path.
// Invalid skills and missing linked folders are returned with Issue set.
func (s *Service) Library() ([]Skill, error) {
	lock, err := store.LoadLock(s.paths.Lock)
	if err != nil {
		return nil, err
	}
	lib, err := s.scanLibrary(lock)
	if err != nil {
		return nil, err
	}
	for i := range lib {
		sk := &lib[i]
		if sk.Record != nil {
			// A hash failure counts as modified so that nothing overwrites it.
			h, err := skill.Hash(sk.Dir)
			sk.Modified = err != nil || h != sk.Record.Hash
		}
	}
	return lib, nil
}

// scanLibrary finds the skills in the library, without checking their
// content against the records (see Library).
func (s *Service) scanLibrary(lock *store.Lock) ([]Skill, error) {
	entries, err := os.ReadDir(s.paths.Skills)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lib []Skill
	for _, e := range entries {
		if !hidden(e.Name()) {
			lib = append(lib, s.scanEntry(e.Name(), lock)...)
		}
	}
	slices.SortFunc(lib, func(a, b Skill) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Path, b.Path))
	})
	return lib, nil
}

// scanEntry returns the skills of one top-level library entry. An entry in
// which no skill is found is returned as one skill with the reason as Issue.
func (s *Service) scanEntry(name string, lock *store.Lock) []Skill {
	entry := Skill{Path: name, Name: name, Dir: filepath.Join(s.paths.Skills, name), Entry: name}
	info, err := os.Lstat(entry.Dir)
	if err != nil {
		entry.Issue = err.Error()
		return []Skill{entry}
	}
	if isLink(info) {
		entry.Linked = true
		entry.LinkTarget, _ = os.Readlink(entry.Dir)
		if info, err = os.Stat(entry.Dir); err != nil {
			entry.Issue = "linked folder is missing: " + entry.LinkTarget
			return []Skill{entry}
		}
	}
	if !info.IsDir() {
		if !entry.Linked {
			return nil // a stray file, not an entry
		}
		entry.Issue = "linked path is not a folder: " + entry.LinkTarget
		return []Skill{entry}
	}

	found := findSkills(entry.Dir, name, map[string]bool{}, nil)
	if len(found) == 0 {
		entry.Issue = "no " + skill.FileName + " found in this folder"
		return []Skill{entry}
	}
	skills := make([]Skill, 0, len(found))
	for _, libPath := range found {
		sk := entry
		sk.Path = libPath
		sk.Dir = filepath.Join(s.paths.Skills, filepath.FromSlash(libPath))
		sk.Name = path.Base(libPath)
		if meta, err := skill.Read(sk.Dir); err != nil {
			sk.Issue = err.Error()
		} else {
			sk.Name, sk.Description = meta.Name, meta.Description
		}
		if rec, ok := lock.Skills[libPath]; ok && !sk.Linked {
			sk.Record = &rec
		}
		skills = append(skills, sk)
	}
	return skills
}

// findSkills appends the library paths of the skill directories at or below
// dir. A directory with SKILL.md is a skill and is not descended into. Hidden
// children are skipped; symlinked children are followed, and seen (resolved
// paths) guards against cycles.
func findSkills(dir, libPath string, seen map[string]bool, found []string) []string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		if seen[real] {
			return found
		}
		seen[real] = true
	}
	if _, err := os.Stat(filepath.Join(dir, skill.FileName)); err == nil {
		return append(found, libPath)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return found
	}
	for _, e := range entries {
		if hidden(e.Name()) {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if info, err := os.Stat(child); err != nil || !info.IsDir() {
			continue
		}
		found = findSkills(child, libPath+"/"+e.Name(), seen, found)
	}
	return found
}

// Resolve finds a library skill by library path or by name. A name shared by
// several skills is ErrAmbiguous (the message lists their paths).
func (s *Service) Resolve(ref string) (Skill, error) {
	lib, err := s.Library()
	if err != nil {
		return Skill{}, err
	}
	return resolve(lib, ref)
}

func resolve(lib []Skill, ref string) (Skill, error) {
	ref = strings.Trim(filepath.ToSlash(ref), "/")
	if i := slices.IndexFunc(lib, func(sk Skill) bool { return sk.Path == ref }); i >= 0 {
		return lib[i], nil
	}
	var paths []string
	for _, sk := range lib {
		if sk.Name == ref {
			paths = append(paths, sk.Path)
		}
	}
	switch len(paths) {
	case 0:
		return Skill{}, errorf(ErrNotFound, "no skill %q in the library", ref)
	case 1:
		i := slices.IndexFunc(lib, func(sk Skill) bool { return sk.Path == paths[0] })
		return lib[i], nil
	}
	return Skill{}, errorf(ErrAmbiguous, "%q matches several skills: %s; use the library path to choose one",
		ref, strings.Join(paths, ", "))
}

// Installed returns the library skill installed from the git url with the
// given name, if any. lib is the result of Library.
func Installed(lib []Skill, url, name string) (Skill, bool) {
	for _, sk := range lib {
		if sk.Record != nil && sk.Record.Source.Type == "git" && sk.Name == name && sameURL(sk.Record.Source.URL, url) {
			return sk, true
		}
	}
	return Skill{}, false
}

// sameURL reports whether two git URLs name the same repository. GitHub https
// URLs, the form every GitHub source is given (see source.Parse), compare
// ignoring case, a trailing slash and ".git", as GitHub does; any other URL
// must match exactly, since those can matter elsewhere (file paths, other hosts).
func sameURL(a, b string) bool {
	const github = "https://github.com/"
	isGitHub := func(u string) bool {
		return len(u) > len(github) && strings.EqualFold(u[:len(github)], github)
	}
	if !isGitHub(a) || !isGitHub(b) {
		return a == b
	}
	norm := func(u string) string {
		return strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(u), "/"), ".git")
	}
	return norm(a) == norm(b)
}

// underPath reports whether the library path p is prefix or lies below it.
func underPath(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}
