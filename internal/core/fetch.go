package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/legibet/myskills/internal/git"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

// Candidate is a skill found in a fetched source.
type Candidate struct {
	Name        string
	Description string
	Dir         string // absolute directory in the checkout or local folder
	RelPath     string // slash-separated path from the source root; "" for the root
	Issue       string // validation problem; such candidates cannot be installed
	Installed   string // library path when this source's skill is already installed
	Conflict    string // library path occupying skills/<Name>, from another source
}

// Fetched is a cloned repository or a local folder with its candidates.
// Close removes any temporary checkout.
type Fetched struct {
	Spec       source.Spec
	Root       string
	Commit     string // git sources only
	Candidates []Candidate
	cleanup    func() error
}

// Close removes the temporary checkout, if any.
func (f *Fetched) Close() error {
	if f.cleanup == nil {
		return nil
	}
	return f.cleanup()
}

// Fetch clones a git source (or reads a local folder) and discovers its
// candidates (design §5), applying Spec.Subdir and Spec.Skill. A Skill filter
// matching several candidates is ErrAmbiguous; matching none is ErrNotFound.
func (s *Service) Fetch(ctx context.Context, spec source.Spec) (*Fetched, error) {
	f := &Fetched{Spec: spec}
	switch spec.Kind {
	case source.Git:
		tmp, err := os.MkdirTemp("", "myskills-fetch-")
		if err != nil {
			return nil, err
		}
		f.cleanup = func() error { return os.RemoveAll(tmp) }
		f.Root = filepath.Join(tmp, "repo")
		if f.Commit, err = git.Clone(ctx, spec.URL, spec.Ref, f.Root); err != nil {
			_ = f.Close()
			return nil, err
		}
	case source.Local:
		if err := s.checkLocalDir(spec.Path); err != nil {
			return nil, err
		}
		f.Root = spec.Path
	default:
		return nil, fmt.Errorf("unknown source kind %d", spec.Kind)
	}
	if err := s.discover(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// checkLocalDir checks that a local source is a folder outside the library.
func (s *Service) checkLocalDir(dir string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return errorf(ErrNotFound, "%s does not exist", dir)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	// Check both spellings: a path through a linked entry resolves outside.
	for _, pair := range [][2]string{{s.paths.Skills, filepath.Clean(dir)}, {realPath(s.paths.Skills), realPath(dir)}} {
		if _, ok := within(pair[0], pair[1]); ok || pair[0] == pair[1] {
			return fmt.Errorf("%s is inside the library; choose a folder outside %s", dir, tilde(s.paths.Skills))
		}
	}
	return nil
}

// discover fills f.Candidates.
func (s *Service) discover(f *Fetched) error {
	start := f.Root
	if f.Spec.Subdir != "" {
		start = filepath.Join(f.Root, filepath.FromSlash(f.Spec.Subdir))
		if info, err := os.Stat(start); err != nil || !info.IsDir() {
			return errorf(ErrNotFound, "%s has no folder %q", f.Spec.Input, f.Spec.Subdir)
		}
	}
	dirs, err := skill.Discover(start)
	if err != nil {
		return err
	}
	rootReal := realPath(f.Root)
	var cands []Candidate
	for _, dir := range dirs {
		c := Candidate{Name: filepath.Base(dir), Dir: dir}
		if rel, err := filepath.Rel(rootReal, dir); err == nil && filepath.IsLocal(rel) {
			if rel != "." {
				c.RelPath = filepath.ToSlash(rel)
			}
		} else {
			c.Issue = "the skill folder is outside the source"
		}
		if meta, err := skill.ReadFile(filepath.Join(dir, skill.FileName)); err != nil {
			c.Issue = cmp.Or(c.Issue, err.Error())
		} else {
			c.Name, c.Description = meta.Name, meta.Description
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return errorf(ErrNotFound, "no skills found in %s", f.Spec.Input)
	}

	if want := f.Spec.Skill; want != "" {
		var matched []Candidate
		for _, c := range cands {
			if strings.EqualFold(c.Name, want) || strings.EqualFold(filepath.Base(c.Dir), want) {
				matched = append(matched, c)
			}
		}
		switch len(matched) {
		case 0:
			return errorf(ErrNotFound, "no skill %q in %s; it has: %s", want, f.Spec.Input, strings.Join(candidateNames(cands), ", "))
		case 1:
		default:
			return errorf(ErrAmbiguous, "several skills in %s match %q: %s", f.Spec.Input, want, strings.Join(candidatePaths(matched), ", "))
		}
		cands = matched
	}

	lock, err := store.LoadLock(s.paths.Lock)
	if err != nil {
		return err
	}
	for i := range cands {
		c := &cands[i]
		if p, ok := s.installedFrom(lock, f, *c); ok {
			c.Installed = p
		} else if _, err := os.Lstat(filepath.Join(s.paths.Skills, c.Name)); err == nil {
			c.Conflict = c.Name
		}
	}
	f.Candidates = cands
	return nil
}

func candidateNames(cands []Candidate) []string {
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = c.Name
	}
	return names
}

func candidatePaths(cands []Candidate) []string {
	paths := make([]string, len(cands))
	for i, c := range cands {
		paths[i] = cmp.Or(c.RelPath, ".")
	}
	return paths
}

// installedFrom returns the library path of the skill installed from the same
// source as c: the same git URL, ref and path, or the same local folder.
func (s *Service) installedFrom(lock *store.Lock, f *Fetched, c Candidate) (string, bool) {
	for _, p := range slices.Sorted(maps.Keys(lock.Skills)) {
		src := lock.Skills[p].Source
		same := false
		switch f.Spec.Kind {
		case source.Git:
			same = src.Type == "git" && sameURL(src.URL, f.Spec.URL) && src.Ref == f.Spec.Ref && src.Path == c.RelPath
		case source.Local:
			same = src.Type == "local" && filepath.Clean(src.Path) == filepath.Clean(c.Dir)
		}
		if !same {
			continue
		}
		// A record whose folder was deleted by hand does not count.
		if _, err := os.Lstat(filepath.Join(s.paths.Skills, filepath.FromSlash(p))); err == nil {
			return p, true
		}
	}
	return "", false
}

// InstallResult is the outcome for one candidate.
type InstallResult struct {
	Candidate Candidate
	Path      string // library path when installed or already present
	Already   bool   // the same source was already in the library; nothing changed
	Err       error  // ErrConflict, a validation or I/O error
}

// Install copies the picked candidates of f into the library and records
// their source (design §5).
func (s *Service) Install(f *Fetched, picks []Candidate) []InstallResult {
	results := make([]InstallResult, len(picks))
	for i, c := range picks {
		results[i].Candidate = c
	}
	err := s.mutate(func(st *state) error {
		for i := range results {
			r := &results[i]
			r.Path, r.Already, r.Err = s.install(st.lock, f, r.Candidate)
		}
		return nil
	})
	if err != nil {
		// The lock could not be taken, or the records of the installed skills
		// could not be saved.
		for i := range results {
			if r := &results[i]; r.Err == nil && !r.Already {
				r.Err = err
			}
		}
	}
	return results
}

// install copies one candidate into the library under the mutation lock.
func (s *Service) install(lock *store.Lock, f *Fetched, c Candidate) (string, bool, error) {
	if c.Issue != "" {
		return "", false, fmt.Errorf("%s can't be installed: %s", c.Name, c.Issue)
	}
	if p, ok := s.installedFrom(lock, f, c); ok {
		return p, true, nil
	}
	dest := filepath.Join(s.paths.Skills, c.Name)
	if _, err := os.Lstat(dest); err == nil {
		return "", false, errorf(ErrConflict, "the library already has a skill %q from another source; remove it first to install this one", c.Name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}

	if err := os.MkdirAll(s.paths.Skills, 0o755); err != nil {
		return "", false, err
	}
	work, err := os.MkdirTemp(s.paths.Skills, ".incoming-")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	stage := filepath.Join(work, c.Name)
	if err := skill.Copy(c.Dir, stage, f.Root); err != nil {
		return "", false, err
	}
	// The source may have changed since it was fetched.
	if _, err := skill.Read(stage); err != nil {
		return "", false, fmt.Errorf("%s can't be installed: %w", c.Name, err)
	}
	hash, err := skill.Hash(stage)
	if err != nil {
		return "", false, err
	}
	if err := os.Rename(stage, dest); err != nil {
		return "", false, err
	}

	rec := store.SkillRecord{Hash: hash, InstalledAt: now()}
	rec.UpdatedAt = rec.InstalledAt
	switch f.Spec.Kind {
	case source.Git:
		rec.Source = store.Source{Type: "git", URL: f.Spec.URL, Ref: f.Spec.Ref, Path: c.RelPath}
		rec.Commit = f.Commit
	case source.Local:
		rec.Source = store.Source{Type: "local", Path: c.Dir}
	}
	lock.Skills[c.Name] = rec
	return c.Name, false, nil
}

// LinkLocal adds a local folder (a skill or a collection) to the library as a
// linked entry named after the folder and returns the entry name.
func (s *Service) LinkLocal(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := s.checkLocalDir(abs); err != nil {
		return "", err
	}
	name := filepath.Base(abs)
	if filepath.Dir(abs) == abs {
		return "", fmt.Errorf("%s can't be linked: choose a skill or collection folder", abs)
	}
	if hidden(name) {
		return "", fmt.Errorf("%s can't be linked: the library skips folders whose name starts with a dot", abs)
	}
	found, err := skill.Discover(abs)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", errorf(ErrNotFound, "no skills found in %s", abs)
	}
	err = s.mutate(func(*state) error {
		entry := filepath.Join(s.paths.Skills, name)
		if _, err := os.Lstat(entry); err == nil {
			return errorf(ErrConflict, "the library already has an entry %q; remove or rename it first", name)
		}
		if err := os.MkdirAll(s.paths.Skills, 0o755); err != nil {
			return err
		}
		return symlinkDir(abs, entry)
	})
	if err != nil {
		return "", err
	}
	return name, nil
}
