package core

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/git"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/store"
)

// Scope is where skills are enabled: a project root, or global when Root is "".
type Scope struct {
	Root string
}

// Global is the global scope.
var Global = Scope{}

// IsGlobal reports whether the scope is global.
func (sc Scope) IsGlobal() bool {
	return sc.Root == ""
}

// String returns "global" or the project root with the home directory as "~".
func (sc Scope) String() string {
	if sc.IsGlobal() {
		return "global"
	}
	return tilde(sc.Root)
}

// ProjectScope resolves the project containing dir (docs/enabling.md): its git
// work tree root, or dir itself outside git. The home directory and the
// filesystem root are ErrNoProject.
func (s *Service) ProjectScope(dir string) (Scope, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Scope{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Scope{}, err
	}
	if !info.IsDir() {
		return Scope{}, fmt.Errorf("%s is not a folder", abs)
	}
	root, err := git.Toplevel(abs)
	switch {
	case err == nil:
	case errors.Is(err, git.ErrNotRepo), errors.Is(err, git.ErrNotFound):
		// Resolved like git's toplevel, so a project has one spelling.
		root = realPath(abs)
	default:
		return Scope{}, err
	}
	if home, err := os.UserHomeDir(); err == nil && realPath(root) == realPath(home) {
		return Scope{}, errorf(ErrNoProject, "%s is the home directory, not a project; run in a project folder or use the global scope", root)
	}
	if filepath.Dir(root) == root {
		return Scope{}, errorf(ErrNoProject, "%s is the filesystem root, not a project; run in a project folder or use the global scope", root)
	}
	return Scope{Root: root}, nil
}

// Mode is how a placement provides a skill.
type Mode string

const (
	Link  Mode = "link"
	Copy  Mode = "copy"
	Mixed Mode = "mixed" // an enablement whose placements use both modes
)

// Kind classifies an entry found in an agent skill directory.
type Kind string

const (
	KindLink   Kind = "link"   // symlink into the library
	KindCopy   Kind = "copy"   // recorded copy
	KindBroken Kind = "broken" // symlink into the library whose target is gone
	KindOther  Kind = "other"  // not managed by myskills
)

// Placement is an entry in an agent skill directory.
type Placement struct {
	Path     string   // absolute entry path
	Name     string   // entry name
	Agents   []string // agents whose skill directory holds the entry
	Kind     Kind
	Skill    string // library path for link, copy and broken placements
	Modified bool   // copy content differs from its record
	Outdated bool   // the library content changed since the copy was made

	// What the scan found, to recheck it before replacing or removing it.
	target string      // a link's target as read
	info   fs.FileInfo // a copy's directory
}

// managed reports whether myskills made the placement.
func (p Placement) managed() bool {
	return p.Kind != KindOther
}

// mode is the enable mode of a managed placement.
func (p Placement) mode() Mode {
	if p.Kind == KindCopy {
		return Copy
	}
	return Link
}

// Enablement is a library skill's placements in one scope.
type Enablement struct {
	Skill      string // library path
	Name       string
	Mode       Mode
	Placements []Placement
	Partial    bool // a default agent that supports the scope has no placement
}

// Status is everything found in a scope's agent skill directories.
type Status struct {
	Scope       Scope
	Enablements []Enablement // managed and healthy, sorted by name
	Broken      []Placement
	Others      []Placement
}

// Status scans the skill directories of every known agent in the scope
// (docs/enabling.md).
func (s *Service) Status(sc Scope) (Status, error) {
	st, err := s.load()
	if err != nil {
		return Status{}, err
	}
	placements, err := s.scan(st, sc)
	if err != nil {
		return Status{}, err
	}

	status := Status{Scope: sc}
	var enabled []*Enablement
	for _, p := range placements {
		switch p.Kind {
		case KindLink, KindCopy:
			if p.Kind == KindCopy {
				s.checkCopy(st.lock, &p)
			}
			i := slices.IndexFunc(enabled, func(e *Enablement) bool { return e.Skill == p.Skill })
			if i < 0 {
				i = len(enabled)
				enabled = append(enabled, &Enablement{Skill: p.Skill, Name: path.Base(p.Skill)})
			}
			enabled[i].Placements = append(enabled[i].Placements, p)
		case KindBroken:
			status.Broken = append(status.Broken, p)
		default:
			status.Others = append(status.Others, p)
		}
	}
	for _, e := range enabled {
		e.Mode = modeOf(e.Placements)
		e.Partial = partial(st, sc, e.Placements)
		status.Enablements = append(status.Enablements, *e)
	}
	slices.SortFunc(status.Enablements, func(a, b Enablement) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Skill, b.Skill))
	})
	return status, nil
}

// modeOf is the mode shared by managed placements, Mixed when they differ, and
// "" when there are none.
func modeOf(placements []Placement) Mode {
	var mode Mode
	for _, p := range placements {
		switch {
		case mode == "":
			mode = p.mode()
		case mode != p.mode():
			return Mixed
		}
	}
	return mode
}

// partial reports whether a default agent with a skill directory in the scope
// has none of the placements. Agents sharing a directory share its placements.
func partial(st *state, sc Scope, placements []Placement) bool {
	for _, id := range st.cfg.DefaultAgents() {
		a, ok := agent.Find(st.agents, id)
		if !ok {
			continue
		}
		if _, ok := dirFor(a, sc); !ok {
			continue
		}
		if !slices.ContainsFunc(placements, func(p Placement) bool { return slices.Contains(p.Agents, id) }) {
			return true
		}
	}
	return false
}

// Usage maps library paths to the scopes where they are enabled: global and
// every known project that still exists.
func (s *Service) Usage() (map[string][]Scope, error) {
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	usage := map[string][]Scope{}
	for _, sc := range knownScopes(st.lock) {
		placements, err := s.scan(st, sc)
		if err != nil {
			continue // an unreadable project must not hide the others
		}
		for _, p := range placements {
			if (p.Kind == KindLink || p.Kind == KindCopy) && !slices.Contains(usage[p.Skill], sc) {
				usage[p.Skill] = append(usage[p.Skill], sc)
			}
		}
	}
	return usage, nil
}

// Scopes returns the global scope and every known project that exists.
func (s *Service) Scopes() ([]Scope, error) {
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	return knownScopes(st.lock), nil
}

// knownScopes returns the global scope and every known project that exists.
func knownScopes(lock *store.Lock) []Scope {
	scopes := []Scope{Global}
	for _, root := range lock.Projects {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			scopes = append(scopes, Scope{Root: root})
		}
	}
	return scopes
}

// agentDir is a skill directory in a scope, shared by every agent whose
// configured directory resolves to it.
type agentDir struct {
	path    string   // the directory as configured for the first agent using it
	real    string   // its resolved location (see realPath)
	agents  []string // ids of the agents using it, in agent order
	aliases []string // every configured spelling of it
}

func dirFor(a agent.Agent, sc Scope) (string, bool) {
	if sc.IsGlobal() {
		return a.GlobalDir()
	}
	return a.ProjectDir(sc.Root)
}

// agentDirs groups the skill directories of the agents in the scope by their
// resolved location, so that e.g. .claude/skills → .agents/skills is one dir.
func agentDirs(agents []agent.Agent, sc Scope) []*agentDir {
	var dirs []*agentDir
	for _, a := range agents {
		p, ok := dirFor(a, sc)
		if !ok {
			continue
		}
		resolved := realPath(p)
		i := slices.IndexFunc(dirs, func(d *agentDir) bool { return d.real == resolved })
		if i < 0 {
			i = len(dirs)
			dirs = append(dirs, &agentDir{path: p, real: resolved})
		}
		d := dirs[i]
		d.agents = append(d.agents, a.ID)
		if !slices.Contains(d.aliases, p) {
			d.aliases = append(d.aliases, p)
		}
	}
	return dirs
}

// scan classifies the entries of every agent skill directory in the scope.
// Copies are not hashed here; see checkCopy.
func (s *Service) scan(st *state, sc Scope) ([]Placement, error) {
	var all []Placement
	for _, d := range agentDirs(st.agents, sc) {
		placements, err := s.scanDir(st.lock, d)
		if err != nil {
			return nil, err
		}
		all = append(all, placements...)
	}
	return all, nil
}

// scanDir classifies the entries of one agent skill directory. A missing
// directory has none.
func (s *Service) scanDir(lock *store.Lock, d *agentDir) ([]Placement, error) {
	if info, err := os.Stat(d.path); err != nil || !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(d.path)
	if err != nil {
		return nil, err
	}
	var placements []Placement
	for _, e := range entries {
		if hidden(e.Name()) {
			continue
		}
		p, err := s.classify(lock, d, e.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed while scanning
		}
		if err != nil {
			return nil, err
		}
		placements = append(placements, p)
	}
	return placements, nil
}

// classify describes the entry name in the agent dir d (docs/enabling.md).
func (s *Service) classify(lock *store.Lock, d *agentDir, name string) (Placement, error) {
	p := Placement{Path: filepath.Join(d.path, name), Name: name, Agents: d.agents, Kind: KindOther}
	info, err := lstat(p.Path)
	if err != nil {
		return p, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(p.Path)
		if err != nil {
			return p, err
		}
		p.target = target
		if !filepath.IsAbs(target) {
			target = filepath.Join(d.real, target)
		}
		lib, ok := s.libraryPath(target)
		if !ok {
			return p, nil
		}
		p.Skill, p.Kind = lib, KindBroken
		if _, err := os.Stat(p.Path); err == nil {
			p.Kind = KindLink
		}
		return p, nil
	}
	if info.IsDir() {
		// The copy may have been recorded through another spelling of the dir.
		for _, alias := range d.aliases {
			key := filepath.Join(alias, name)
			if rec, ok := lock.Copies[key]; ok {
				p.Path, p.Kind, p.Skill, p.info = key, KindCopy, rec.Skill, info
				break
			}
		}
	}
	return p, nil
}

// libraryPath returns the library path of an absolute link target when it
// lies inside the library, checked lexically against the library root and its
// resolved form.
func (s *Service) libraryPath(target string) (string, bool) {
	target = filepath.Clean(target)
	for _, root := range []string{s.paths.Skills, realPath(s.paths.Skills)} {
		if rel, ok := within(root, target); ok {
			return filepath.ToSlash(rel), true
		}
	}
	return "", false
}

// checkCopy sets Modified and Outdated on a copy placement. Unreadable
// content counts as modified, so it is never deleted without force.
//
// Outdated compares what copying the library skill gives now with what it gave
// at copy time, not with the copy, whose files the umask may have changed.
func (s *Service) checkCopy(lock *store.Lock, p *Placement) {
	rec := lock.Copies[p.Path]
	h, err := skill.Hash(p.Path)
	p.Modified = err != nil || h != rec.Hash
	entry, _, _ := strings.Cut(rec.Skill, "/")
	h, err = skill.CopyHash(filepath.Join(s.paths.Skills, filepath.FromSlash(rec.Skill)), filepath.Join(s.paths.Skills, entry))
	p.Outdated = err == nil && h != rec.LibraryHash
}

// holdsPlacements reports whether a project root contains a placement made by
// myskills. A root that is gone (an unmounted volume) is assumed to.
func (s *Service) holdsPlacements(st *state, root string) bool {
	if _, err := os.Stat(root); err != nil {
		return true
	}
	placements, err := s.scan(st, Scope{Root: root})
	return err != nil || slices.ContainsFunc(placements, Placement.managed)
}

// pruneProjects forgets the given project roots, or all known ones when roots
// is nil, that hold no placement anymore, along with the stale copy records
// of their agent directories.
func (s *Service) pruneProjects(st *state, roots ...string) {
	st.lock.Projects = slices.DeleteFunc(st.lock.Projects, func(root string) bool {
		if roots != nil && !slices.Contains(roots, root) || s.holdsPlacements(st, root) {
			return false
		}
		// Not every record below the root: a project inside it keeps its own.
		for _, d := range agentDirs(st.agents, Scope{Root: root}) {
			for key := range st.lock.Copies {
				if slices.Contains(d.aliases, filepath.Dir(key)) {
					delete(st.lock.Copies, key)
				}
			}
		}
		return true
	})
}

// addProject records a project root where something was placed.
func addProject(lock *store.Lock, sc Scope) {
	if !sc.IsGlobal() && !slices.Contains(lock.Projects, sc.Root) {
		lock.Projects = append(lock.Projects, sc.Root)
	}
}
