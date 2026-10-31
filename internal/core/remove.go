package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RemoveResult describes a library removal.
type RemoveResult struct {
	Entry    string   // library entry that was removed (a skill dir or a linked entry)
	Skills   []string // library paths that left the library
	Unlinked []string // link placements removed, in all known scopes
	Released []string // copy placements left in place and no longer managed
	Warnings []string // e.g. a git exclude file could not be updated
}

// Remove removes a skill from the library: the skill directory, or the whole
// linked entry when the skill lives in one (never files behind the link).
// Link placements of the removed skills are removed in every known scope.
func (s *Service) Remove(sk Skill) (RemoveResult, error) {
	var res RemoveResult
	err := s.mutate(func(st *state) error {
		lib, err := s.scanLibrary(st.lock)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(lib, func(l Skill) bool { return l.Path == sk.Path }) {
			return errorf(ErrNotFound, "no skill %q in the library", sk.Path)
		}
		if res.Entry, err = s.removeFromLibrary(sk.Path); err != nil {
			return err
		}
		for _, l := range lib {
			if underPath(l.Path, res.Entry) {
				res.Skills = append(res.Skills, l.Path)
			}
		}
		for p := range st.lock.Skills {
			if underPath(p, res.Entry) {
				delete(st.lock.Skills, p)
			}
		}
		for p, rec := range st.lock.Copies {
			if underPath(rec.Skill, res.Entry) {
				delete(st.lock.Copies, p)
				res.Released = append(res.Released, p)
			}
		}

		var errs []error
		for _, sc := range knownScopes(st.lock) {
			placements, err := s.scan(st, sc)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			unlinked := false
			for _, p := range placements {
				if (p.Kind == KindLink || p.Kind == KindBroken) && underPath(p.Skill, res.Entry) {
					if err := removePlacement(st.lock, p); err != nil {
						errs = append(errs, err)
						continue
					}
					res.Unlinked = append(res.Unlinked, p.Path)
					unlinked = true
				}
			}
			if unlinked && !sc.IsGlobal() {
				res.Warnings = append(res.Warnings, s.syncExclude(st, sc.Root)...)
			}
		}
		s.pruneProjects(st)
		slices.Sort(res.Released)
		return errors.Join(errs...)
	})
	return res, err
}

// removeFromLibrary deletes the library entry owning the skill at libPath and
// returns its library path. When a symlink lies on the way to the skill (a
// linked entry, or a link inside a collection), that link is removed and never
// the files behind it. Otherwise the skill directory is deleted, then its
// parents up to the library root while they are empty.
func (s *Service) removeFromLibrary(libPath string) (string, error) {
	parts := strings.Split(libPath, "/")
	for i := range parts {
		rel := strings.Join(parts[:i+1], "/")
		p := filepath.Join(s.paths.Skills, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if isLink(info) {
			return rel, os.Remove(p)
		}
	}
	dir := filepath.Join(s.paths.Skills, filepath.FromSlash(libPath))
	if _, ok := within(s.paths.Skills, dir); !ok {
		return "", fmt.Errorf("refusing to delete %s: it is not inside the library", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	for p := filepath.Dir(dir); p != s.paths.Skills && len(p) > len(s.paths.Skills); p = filepath.Dir(p) {
		if os.Remove(p) != nil {
			break // not empty
		}
	}
	return libPath, nil
}
