package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/legibet/myskills/internal/git"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/store"
)

// UpdateState is the result of checking one skill for updates.
type UpdateState string

const (
	UpToDate        UpdateState = "up-to-date"
	Available       UpdateState = "available"
	RemovedUpstream UpdateState = "removed-upstream"
	NotUpdatable    UpdateState = "not-updatable"
	CheckFailed     UpdateState = "failed"
)

// UpdateCheck is the check result for one skill.
type UpdateCheck struct {
	Skill  Skill
	State  UpdateState
	Commit string // upstream commit that was checked (git sources)
	Err    error  // set when State is CheckFailed

	staged string // the upstream content, copied with the copy rules
}

// Updates holds check results and the checkouts they were computed from, so
// that Apply installs exactly what was checked. Close removes the checkouts.
type Updates struct {
	Checks  []UpdateCheck
	cleanup func() error
}

// Close removes temporary checkouts.
func (u *Updates) Close() error {
	if u.cleanup == nil {
		return nil
	}
	return u.cleanup()
}

// checkParallelism bounds the sources checked at once.
const checkParallelism = 4

// CheckUpdates checks the skills for upstream changes (design §7). progress,
// if not nil, is called after each source is checked.
//
// Upstream content is compared after copying it with the same rules as an
// install, so a skill whose symlinks are dereferenced on install is not
// reported as changed. Git skills found unchanged at a new commit get that
// commit recorded, so the next check needs no clone.
func (s *Service) CheckUpdates(ctx context.Context, skills []Skill, progress func(done, total int)) (*Updates, error) {
	u := &Updates{Checks: make([]UpdateCheck, len(skills))}
	type gitSource struct{ url, ref string }
	groups := map[gitSource][]int{}
	var order []gitSource
	var locals []int
	for i, sk := range skills {
		u.Checks[i] = UpdateCheck{Skill: sk, State: NotUpdatable}
		if !sk.Updatable() {
			continue
		}
		switch src := sk.Record.Source; src.Type {
		case "git":
			key := gitSource{src.URL, src.Ref}
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], i)
		case "local":
			locals = append(locals, i)
		}
	}

	var (
		mu    sync.Mutex
		temps []string
		done  int
	)
	total := len(order) + len(locals)
	tempDir := func() (string, error) {
		dir, err := os.MkdirTemp("", "myskills-update-")
		if err == nil {
			mu.Lock()
			temps = append(temps, dir)
			mu.Unlock()
		}
		return dir, err
	}
	u.cleanup = func() error {
		var errs []error
		for _, dir := range temps {
			errs = append(errs, os.RemoveAll(dir))
		}
		return errors.Join(errs...)
	}
	finished := func() {
		mu.Lock()
		defer mu.Unlock()
		done++
		if progress != nil {
			progress(done, total)
		}
	}

	sem := make(chan struct{}, checkParallelism)
	var wg sync.WaitGroup
	run := func(check func()) {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() == nil {
				check()
			}
			finished()
		})
	}
	for _, key := range order {
		run(func() { checkGit(ctx, key.url, key.ref, groups[key], u.Checks, tempDir) })
	}
	for _, i := range locals {
		run(func() { checkLocal(&u.Checks[i], tempDir) })
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		_ = u.Close()
		return nil, err
	}

	if err := s.recordVerifiedCommits(u.Checks); err != nil {
		_ = u.Close()
		return nil, err
	}
	return u, nil
}

// checkGit checks the skills at indexes idx of checks, which share one git
// source: one ls-remote, and one clone when a recorded commit differs.
func checkGit(ctx context.Context, url, ref string, idx []int, checks []UpdateCheck, tempDir func() (string, error)) {
	fail := func(indexes []int, err error) {
		for _, i := range indexes {
			checks[i].State, checks[i].Err = CheckFailed, err
		}
	}
	commit, err := git.RemoteCommit(ctx, url, ref)
	if err != nil {
		fail(idx, err)
		return
	}
	var stale []int
	for _, i := range idx {
		c := &checks[i]
		c.Commit = commit
		if c.Skill.Record.Commit == commit {
			c.State = UpToDate
		} else {
			stale = append(stale, i)
		}
	}
	if len(stale) == 0 {
		return
	}
	tmp, err := tempDir()
	if err != nil {
		fail(stale, err)
		return
	}
	repo := filepath.Join(tmp, "repo")
	if commit, err = git.Clone(ctx, url, ref, repo); err != nil {
		fail(stale, err)
		return
	}
	for n, i := range stale {
		c := &checks[i]
		c.Commit = commit
		dir := filepath.Join(repo, filepath.FromSlash(c.Skill.Record.Source.Path))
		compare(c, dir, repo, filepath.Join(tmp, fmt.Sprint(n)))
	}
}

// checkLocal checks a skill installed from a local folder.
func checkLocal(c *UpdateCheck, tempDir func() (string, error)) {
	tmp, err := tempDir()
	if err != nil {
		c.State, c.Err = CheckFailed, err
		return
	}
	dir := c.Skill.Record.Source.Path
	compare(c, dir, dir, tmp)
}

// compare stages the upstream skill dir (copied with boundary as the source
// root) into work and sets the check state by comparing its hash with the
// record.
func compare(c *UpdateCheck, dir, boundary, work string) {
	if info, err := os.Stat(filepath.Join(dir, skill.FileName)); err != nil || !info.Mode().IsRegular() {
		c.State = RemovedUpstream
		return
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		c.State, c.Err = CheckFailed, err
		return
	}
	staged := filepath.Join(work, filepath.Base(c.Skill.Dir))
	hash, err := "", skill.Copy(dir, staged, boundary)
	if err == nil {
		hash, err = skill.Hash(staged)
	}
	switch {
	case err != nil:
		c.State, c.Err = CheckFailed, err
	case hash == c.Skill.Record.Hash:
		c.State = UpToDate
	default:
		c.State, c.staged = Available, staged
	}
}

// recordVerifiedCommits records the upstream commit of git skills whose
// content was found unchanged at a commit other than the recorded one.
func (s *Service) recordVerifiedCommits(checks []UpdateCheck) error {
	var verified []UpdateCheck
	for _, c := range checks {
		if c.State == UpToDate && c.Skill.Record.Source.Type == "git" && c.Commit != c.Skill.Record.Commit {
			verified = append(verified, c)
		}
	}
	if len(verified) == 0 {
		return nil
	}
	return s.mutate(func(st *state) error {
		for _, c := range verified {
			// Skip records that changed since the check.
			rec, ok := st.lock.Skills[c.Skill.Path]
			if ok && rec.Source == c.Skill.Record.Source && rec.Hash == c.Skill.Record.Hash {
				rec.Commit = c.Commit
				st.lock.Skills[c.Skill.Path] = rec
			}
		}
		return nil
	})
}

// UpdateResult is the outcome of updating one skill.
type UpdateResult struct {
	Skill   string // library path
	Updated bool
	Err     error // ErrModified when skipped for local changes, or a failure
}

// ApplyUpdates installs every Available check among paths (all when nil).
// Locally modified skills are skipped with ErrModified unless force.
func (s *Service) ApplyUpdates(u *Updates, paths []string, force bool) []UpdateResult {
	var picks []UpdateCheck
	for _, c := range u.Checks {
		if c.State == Available && (paths == nil || slices.Contains(paths, c.Skill.Path)) {
			picks = append(picks, c)
		}
	}
	results := make([]UpdateResult, len(picks))
	err := s.mutate(func(st *state) error {
		for i, c := range picks {
			results[i] = UpdateResult{Skill: c.Skill.Path}
			results[i].Err = s.applyUpdate(st.lock, c, force)
			results[i].Updated = results[i].Err == nil
		}
		return nil
	})
	if err != nil {
		for i := range results {
			if results[i].Err == nil {
				results[i].Skill = picks[i].Skill.Path
				results[i].Err = err
			}
		}
	}
	return results
}

// applyUpdate replaces one library skill with its checked upstream content.
func (s *Service) applyUpdate(lock *store.Lock, c UpdateCheck, force bool) error {
	// Another process may have updated it since: the check is then stale.
	rec, ok := lock.Skills[c.Skill.Path]
	old := c.Skill.Record
	if !ok || rec.Source != old.Source || rec.Hash != old.Hash || rec.Commit != old.Commit {
		return fmt.Errorf("%s changed since it was checked; check again", c.Skill.Path)
	}
	// A symlink anywhere on the way would make the update write outside the
	// library.
	dest := filepath.Join(s.paths.Skills, filepath.FromSlash(c.Skill.Path))
	info, err := lstat(dest)
	if err != nil {
		return err
	}
	if !info.IsDir() || realPath(dest) != filepath.Join(realPath(s.paths.Skills), filepath.FromSlash(c.Skill.Path)) {
		return fmt.Errorf("%s is not a folder in the library; not updated", c.Skill.Path)
	}
	work, err := os.MkdirTemp(filepath.Dir(dest), ".incoming-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	stage := filepath.Join(work, filepath.Base(dest))
	if err := copySkill(c.staged, stage, c.staged); err != nil {
		return err
	}
	if _, err := skill.Read(stage); err != nil {
		return fmt.Errorf("%s was not updated: the new version is invalid (%w); if it was renamed upstream, remove it and add it again", c.Skill.Path, err)
	}
	hash, err := skill.Hash(stage)
	if err != nil {
		return err
	}
	// Checked right before the swap: the folder must still be the one found
	// above, and edits made while the new version was prepared are not lost.
	// Force waives only the second.
	if now, err := os.Lstat(dest); err != nil || !os.SameFile(info, now) {
		return errorf(ErrConflict, "%s changed while myskills was working; not updated", c.Skill.Path)
	}
	if h, err := skill.Hash(dest); (err != nil || h != rec.Hash) && !force {
		return errorf(ErrModified, "%s has local changes, so it was not updated; force overwrites them", c.Skill.Path)
	}
	if err := swapIn(stage, dest); err != nil {
		return err
	}
	rec.Hash, rec.UpdatedAt = hash, now()
	if rec.Source.Type == "git" {
		rec.Commit = c.Commit
	}
	lock.Skills[c.Skill.Path] = rec
	return nil
}
