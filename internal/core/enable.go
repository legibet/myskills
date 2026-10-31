package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/git"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/store"
)

// Action is what an operation did to one placement.
type Action string

const (
	Linked    Action = "linked"
	Copied    Action = "copied"
	Refreshed Action = "refreshed"
	Removed   Action = "removed"
	Unchanged Action = "unchanged"
)

// Change is one placement touched by an operation.
type Change struct {
	Path    string
	Agents  []string // every agent whose skill directory holds the placement
	Action  Action
	Mode    Mode
	Warning string // e.g. a modified copy was not refreshed
}

// Result is the outcome of enabling or disabling one skill.
type Result struct {
	Skill    string // library path, or the entry name for placements without one
	Changes  []Change
	Warnings []string // e.g. git exclude could not be updated
}

// EnableOptions controls Enable.
type EnableOptions struct {
	Agents []string // explicit agent ids; nil uses the configured defaults
	Mode   Mode     // "" keeps existing placements' mode (link when new); Link or Copy converts
	Force  bool     // allow deleting modified copies
}

// Enable enables a library skill in the scope (design §6). It fails with
// ErrConflict when a target path is occupied by something unmanaged, and with
// ErrModified when it would delete a modified copy without Force.
//
// Every target is checked before anything is changed, so a failing target
// leaves the scope as it was.
func (s *Service) Enable(sc Scope, sk Skill, opts EnableOptions) (Result, error) {
	res := Result{Skill: sk.Path}
	if !sk.Valid() {
		return res, fmt.Errorf("%s can't be enabled: %s", sk.Path, sk.Issue)
	}
	if opts.Mode != "" && opts.Mode != Link && opts.Mode != Copy {
		return res, fmt.Errorf("invalid mode %q: use %s or %s", opts.Mode, Link, Copy)
	}
	err := s.mutate(func(st *state) error {
		// The library may have changed since the caller read it.
		if _, err := skill.Read(sk.Dir); err != nil {
			return fmt.Errorf("%s can't be enabled: %w", sk.Path, err)
		}
		tgts, err := targets(st, sc, opts.Agents)
		if err != nil {
			return err
		}
		placements, err := s.scan(st, sc)
		if err != nil {
			return err
		}
		mode := opts.Mode
		if mode == "" {
			// New placements follow the existing ones; mixed or none → link.
			mine := slices.DeleteFunc(placements, func(p Placement) bool {
				return p.Skill != sk.Path || (p.Kind != KindLink && p.Kind != KindCopy)
			})
			mode = Link
			if modeOf(mine) == Copy {
				mode = Copy
			}
		}
		repo, warnings := repoRoot(sc)
		res.Warnings = warnings
		steps, err := s.planEnable(st, sk, tgts, mode, opts, repo)
		if err != nil {
			return err
		}
		res.Changes, err = s.applySteps(st, sk, steps)
		addProject(st.lock, sc)
		if !sc.IsGlobal() {
			res.Warnings = append(res.Warnings, s.syncExclude(st, sc.Root)...)
		}
		return err
	})
	return res, err
}

// target is an agent skill directory chosen for an enablement.
type target struct {
	dir  *agentDir
	path string // the directory as configured for the first chosen agent
}

// targets resolves the agent skill directories for an enablement (design §6
// Target resolution): the agents ids, or the configured defaults when ids is
// nil. Directories are deduplicated by resolved path; the first agent wins.
func targets(st *state, sc Scope, ids []string) ([]target, error) {
	explicit := ids != nil
	if !explicit {
		ids = st.cfg.DefaultAgents()
	}
	if len(ids) == 0 {
		return nil, errors.New("no agents chosen")
	}
	dirs := agentDirs(st.agents, sc)
	var out []target
	for _, id := range ids {
		a, ok := agent.Find(st.agents, id)
		if !ok {
			return nil, errorf(ErrNotFound, "unknown agent %q", id)
		}
		p, ok := dirFor(a, sc)
		if !ok {
			if explicit {
				return nil, fmt.Errorf("%s has no %s skill directory", a.Name, scopeKind(sc))
			}
			continue
		}
		d := dirs[slices.IndexFunc(dirs, func(d *agentDir) bool { return slices.Contains(d.agents, id) })]
		if !slices.ContainsFunc(out, func(t target) bool { return t.dir == d }) {
			out = append(out, target{dir: d, path: p})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("none of the agents %s has a %s skill directory; choose agents that have one",
			strings.Join(ids, ", "), scopeKind(sc))
	}
	return out, nil
}

// checkAgents checks agent ids given to limit an operation in the scope.
func checkAgents(st *state, sc Scope, ids []string) error {
	for _, id := range ids {
		a, ok := agent.Find(st.agents, id)
		if !ok {
			return errorf(ErrNotFound, "unknown agent %q", id)
		}
		if _, ok := dirFor(a, sc); !ok {
			return fmt.Errorf("%s has no %s skill directory", a.Name, scopeKind(sc))
		}
	}
	return nil
}

func scopeKind(sc Scope) string {
	if sc.IsGlobal() {
		return "global"
	}
	return "project"
}

// step is the planned change of one placement.
type step struct {
	target
	dest    string // placement path
	action  Action
	mode    Mode
	old     *Placement // managed entry being replaced, if any
	warning string
	stage   string // the new entry, prepared by applySteps
	hash    string // content hash of a prepared copy
}

// planEnable decides what happens at each target and fails on the first
// conflict, modified copy or tracked path, before anything is changed.
func (s *Service) planEnable(st *state, sk Skill, tgts []target, mode Mode, opts EnableOptions, repo string) ([]step, error) {
	created := Linked
	if mode == Copy {
		created = Copied
	}
	var steps []step
	for _, t := range tgts {
		stp := step{target: t, dest: filepath.Join(t.path, sk.Name), action: created, mode: mode}
		_, err := os.Lstat(stp.dest)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			p, err := s.classify(st.lock, t.dir, sk.Name)
			if err != nil {
				return nil, err
			}
			// Existing placements of the skill change only when a mode is
			// asked for: Link or Copy converts, Copy also refreshes.
			switch {
			case p.Kind == KindBroken && p.Skill == sk.Path:
				stp.old = &p
			case p.Kind == KindBroken:
				return nil, errorf(ErrConflict, "%s is a broken link to library skill %s; disable %s or move it away to enable %s there", tilde(stp.dest), p.Skill, p.Skill, sk.Path)
			case p.Kind == KindLink && p.Skill == sk.Path:
				if opts.Mode == Copy {
					stp.old = &p
				} else {
					stp.action, stp.mode = Unchanged, Link
				}
			case p.Kind == KindCopy && p.Skill == sk.Path:
				s.checkCopy(st.lock, &p)
				switch {
				case opts.Mode == Link && p.Modified && !opts.Force:
					return nil, errorf(ErrModified, "%s has local changes that converting it to a link would delete; force discards them", tilde(p.Path))
				case opts.Mode == Link:
					stp.old = &p
				case opts.Mode == Copy && p.Outdated && p.Modified && !opts.Force:
					stp.action, stp.mode = Unchanged, Copy
					stp.warning = "modified locally, so not refreshed from the library; force overwrites it"
				case opts.Mode == Copy && p.Outdated:
					stp.action, stp.old = Refreshed, &p
				default:
					stp.action, stp.mode = Unchanged, Copy
				}
			default:
				return nil, errorf(ErrConflict, "%s already exists (%s); move it away to enable %s there", tilde(stp.dest), occupant(p), sk.Path)
			}
		}
		if stp.action != Unchanged && mode == Link {
			if err := checkUntracked(repo, filepath.Join(t.dir.real, sk.Name)); err != nil {
				return nil, err
			}
		}
		steps = append(steps, stp)
	}
	return steps, nil
}

// occupant describes what is in the way at a placement path.
func occupant(p Placement) string {
	switch p.Kind {
	case KindLink:
		return "a link to library skill " + p.Skill
	case KindCopy:
		return "a copy of library skill " + p.Skill
	default:
		return "not managed by myskills"
	}
}

// repoRoot returns the resolved root of the git work tree of a project scope,
// or "" when the scope is global or not in git. Problems running git are
// returned as warnings, since the git checks are then skipped.
func repoRoot(sc Scope) (string, []string) {
	if sc.IsGlobal() {
		return "", nil
	}
	root, err := git.Toplevel(sc.Root)
	switch {
	case err == nil:
		return realPath(root), nil
	case errors.Is(err, git.ErrNotRepo):
		return "", nil
	case errors.Is(err, git.ErrNotFound):
		return "", []string{"git is not installed, so git checks were skipped"}
	default:
		return "", []string{"git checks were skipped: " + err.Error()}
	}
}

// checkUntracked refuses a link where git tracks files (design §6 link): a
// personal link must not replace committed content. entry is resolved.
func checkUntracked(repo, entry string) error {
	if repo == "" {
		return nil
	}
	rel, ok := within(repo, entry)
	if !ok {
		return nil
	}
	rel = filepath.ToSlash(rel)
	tracked, err := git.Tracked(repo, entry)
	if err != nil {
		return fmt.Errorf("could not check whether %s is tracked by git: %w", rel, err)
	}
	if tracked {
		return fmt.Errorf("%s is tracked by git, and a personal link must not replace committed files; enable it as a copy, or untrack it first (git rm -r --cached %s) and commit", rel, rel)
	}
	return nil
}

// applySteps carries out planned steps in two phases. Each new entry (a link
// or a full copy) is first prepared in a hidden work dir inside its agent dir,
// so a copy that fails leaves every placement untouched; then each one is
// rechecked, swapped into place and recorded.
func (s *Service) applySteps(st *state, sk Skill, steps []step) ([]Change, error) {
	var works []string
	defer func() {
		for _, w := range works {
			_ = os.RemoveAll(w)
		}
	}()
	src := realPath(sk.Dir)
	boundary := realPath(filepath.Join(s.paths.Skills, sk.Entry))
	libHash := "" // the library content the copies are made from
	for i := range steps {
		stp := &steps[i]
		if stp.action == Unchanged {
			continue
		}
		// Created at the resolved location: MkdirAll fails below a dangling
		// symlinked parent.
		if err := os.MkdirAll(stp.dir.real, 0o755); err != nil {
			return nil, err
		}
		work, err := os.MkdirTemp(stp.dir.real, ".myskills-")
		if err != nil {
			return nil, err
		}
		works = append(works, work)
		stp.stage = filepath.Join(work, filepath.Base(stp.dest))
		if stp.mode == Link {
			if err := symlinkDir(sk.Dir, stp.stage); err != nil {
				return nil, err
			}
			continue
		}
		// Hashed before copying, so that a library change made meanwhile
		// shows the copy as outdated.
		if libHash == "" {
			if libHash, err = skill.CopyHash(src, boundary); err != nil {
				return nil, err
			}
		}
		if err := copySkill(src, stp.stage, boundary); err != nil {
			return nil, err
		}
		if stp.hash, err = skill.Hash(stp.stage); err != nil {
			return nil, err
		}
	}

	var changes []Change
	for _, stp := range steps {
		if stp.action != Unchanged {
			if err := recheck(st.lock, stp); err != nil {
				return changes, err
			}
			if err := swapIn(stp.stage, stp.dest); err != nil {
				return changes, err
			}
			if stp.old != nil && stp.old.Kind == KindCopy {
				delete(st.lock.Copies, stp.old.Path)
			}
			if stp.mode == Copy {
				st.lock.Copies[stp.dest] = store.CopyRecord{Skill: sk.Path, Hash: stp.hash, LibraryHash: libHash, CopiedAt: now()}
			}
		}
		changes = append(changes, Change{Path: stp.dest, Agents: stp.dir.agents, Action: stp.action, Mode: stp.mode, Warning: stp.warning})
	}
	return changes, nil
}

// recheck checks that the placement path of a step still holds what planning
// found there, right before it is replaced.
func recheck(lock *store.Lock, stp step) error {
	if stp.old != nil {
		return unchanged(lock, *stp.old)
	}
	_, err := os.Lstat(stp.dest)
	if err == nil {
		return errorf(ErrConflict, "%s was created while myskills was working; left in place", tilde(stp.dest))
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// unchanged checks that a managed placement still is what the scan found,
// right before it is replaced or removed: preparing copies and removing other
// placements take time, in which an editor or agent may have changed it. Force
// lets a copy found modified go, which waives the content check but never the
// check that it is still the same directory.
func unchanged(lock *store.Lock, p Placement) error {
	switch p.Kind {
	case KindLink, KindBroken:
		if target, err := os.Readlink(p.Path); err != nil || target != p.target {
			return errorf(ErrConflict, "%s changed while myskills was working; left in place", tilde(p.Path))
		}
	case KindCopy:
		rec, ok := lock.Copies[p.Path]
		info, err := os.Lstat(p.Path)
		if !ok || err != nil || !os.SameFile(info, p.info) {
			return errorf(ErrConflict, "%s changed while myskills was working; left in place", tilde(p.Path))
		}
		if !p.Modified {
			if h, err := skill.Hash(p.Path); err != nil || h != rec.Hash {
				return errorf(ErrModified, "%s changed while myskills was working; left in place", tilde(p.Path))
			}
		}
	default:
		return fmt.Errorf("%s is not managed by myskills; left in place", p.Path)
	}
	return nil
}

// DisableOptions controls Disable.
type DisableOptions struct {
	Agents []string // limit to these agents; nil removes every placement
	Force  bool     // allow deleting modified copies
}

// Disable removes the placements of a skill in the scope. ref is a library
// path or name; a name that no library skill has matches broken placements
// with that entry name.
func (s *Service) Disable(sc Scope, ref string, opts DisableOptions) (Result, error) {
	res := Result{Skill: ref}
	err := s.mutate(func(st *state) error {
		lib, err := s.scanLibrary(st.lock)
		if err != nil {
			return err
		}
		placements, err := s.scan(st, sc)
		if err != nil {
			return err
		}
		sk, resolveErr := resolve(lib, ref)
		var match func(Placement) bool
		switch {
		case slices.ContainsFunc(placements, func(p Placement) bool { return p.Kind == KindBroken && p.Skill == ref }):
			// Like resolve, a path wins over a name: the missing library path
			// of broken links names them, even when ref is also a skill name.
			resolveErr = errorf(ErrNotFound, "no skill %q in the library", ref)
			match = func(p Placement) bool { return p.Kind == KindBroken && p.Skill == ref }
		case resolveErr == nil:
			res.Skill = sk.Path
			match = func(p Placement) bool { return p.managed() && p.Skill == sk.Path }
		case errors.Is(resolveErr, ErrNotFound):
			match = func(p Placement) bool { return p.Kind == KindBroken && (p.Name == ref || p.Skill == ref) }
		default:
			return resolveErr
		}
		if opts.Agents != nil {
			if err := checkAgents(st, sc, opts.Agents); err != nil {
				return err
			}
		}
		var selected []Placement
		for _, p := range placements {
			if match(p) && (opts.Agents == nil || slices.ContainsFunc(p.Agents, func(id string) bool { return slices.Contains(opts.Agents, id) })) {
				selected = append(selected, p)
			}
		}
		if len(selected) == 0 && resolveErr != nil {
			return errorf(ErrNotFound, "no skill or broken placement %q in the library or in %s", ref, sc)
		}
		for i := range selected {
			p := &selected[i]
			if p.Kind != KindCopy {
				continue
			}
			if s.checkCopy(st.lock, p); p.Modified && !opts.Force {
				return errorf(ErrModified, "%s has local changes; force deletes them", tilde(p.Path))
			}
		}

		var errs []error
		for _, p := range selected {
			if err := removePlacement(st.lock, p); err != nil {
				errs = append(errs, err)
				continue
			}
			res.Changes = append(res.Changes, Change{Path: p.Path, Agents: p.Agents, Action: Removed, Mode: p.mode()})
		}
		if !sc.IsGlobal() && len(res.Changes) > 0 {
			s.pruneProjects(st, sc.Root)
			res.Warnings = append(res.Warnings, s.syncExclude(st, sc.Root)...)
		}
		return errors.Join(errs...)
	})
	return res, err
}

// removePlacement deletes a managed placement after checking it still is
// what the scan found: a link is unlinked (never followed), a recorded copy is
// deleted along with its record.
func removePlacement(lock *store.Lock, p Placement) error {
	if err := unchanged(lock, p); err != nil {
		return err
	}
	if p.Kind != KindCopy {
		return os.Remove(p.Path)
	}
	if err := removeAll(p.Path); err != nil {
		return err
	}
	delete(lock.Copies, p.Path)
	return nil
}

// RefreshCopies re-copies outdated, unmodified copy placements of the given
// library skills in the scope (all skills when paths is nil). Modified copies
// are reported with ErrModified unless force.
func (s *Service) RefreshCopies(sc Scope, paths []string, force bool) ([]Result, error) {
	var results []Result
	err := s.mutate(func(st *state) error {
		lib, err := s.scanLibrary(st.lock)
		if err != nil {
			return err
		}
		placements, err := s.scan(st, sc)
		if err != nil {
			return err
		}
		var errs []error
		steps := map[string][]step{}
		for _, p := range placements {
			if p.Kind != KindCopy || paths != nil && !slices.Contains(paths, p.Skill) {
				continue
			}
			if s.checkCopy(st.lock, &p); !p.Outdated {
				continue
			}
			i := slices.IndexFunc(results, func(r Result) bool { return r.Skill == p.Skill })
			if i < 0 {
				i = len(results)
				results = append(results, Result{Skill: p.Skill})
			}
			if p.Modified && !force {
				results[i].Changes = append(results[i].Changes, Change{
					Path: p.Path, Agents: p.Agents, Action: Unchanged, Mode: Copy,
					Warning: "modified locally, so not refreshed; force overwrites it",
				})
				errs = append(errs, errorf(ErrModified, "%s has local changes, so it was not refreshed; force overwrites them", tilde(p.Path)))
				continue
			}
			t := target{dir: &agentDir{real: realPath(filepath.Dir(p.Path)), agents: p.Agents}, path: filepath.Dir(p.Path)}
			steps[p.Skill] = append(steps[p.Skill], step{target: t, dest: p.Path, action: Refreshed, mode: Copy, old: &p})
		}
		for i := range results {
			r := &results[i]
			if len(steps[r.Skill]) == 0 {
				continue
			}
			j := slices.IndexFunc(lib, func(sk Skill) bool { return sk.Path == r.Skill })
			if j < 0 || !lib[j].Valid() {
				errs = append(errs, fmt.Errorf("copies of %s were not refreshed: the library skill is invalid", r.Skill))
				continue
			}
			changes, err := s.applySteps(st, lib[j], steps[r.Skill])
			r.Changes = append(r.Changes, changes...)
			if err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	})
	return results, err
}
