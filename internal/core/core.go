// Package core is the domain service shared by every frontend: the library,
// enablement and updates (design §5–§7). All mutations take the store's
// inter-process lock; a Service is safe for concurrent use.
package core

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/store"
)

// Sentinel errors, wrapped with user-facing detail. Test with errors.Is.
var (
	ErrNotFound  = errors.New("not found")
	ErrAmbiguous = errors.New("ambiguous")
	ErrConflict  = errors.New("conflict")
	ErrModified  = errors.New("locally modified")
	ErrNoProject = errors.New("not a project")
)

// kindError is a user-facing message that matches one of the sentinel errors.
type kindError struct {
	kind error
	msg  string
}

func (e *kindError) Error() string { return e.msg }
func (e *kindError) Unwrap() error { return e.kind }

// errorf returns an error with a formatted message that matches kind.
func errorf(kind error, format string, args ...any) error {
	return &kindError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// Service is the myskills domain API.
type Service struct {
	paths    store.Paths
	SkillsSH *skillssh.Client
}

// New returns a service over the files at paths.
func New(paths store.Paths) *Service {
	return &Service{paths: paths, SkillsSH: skillssh.New()}
}

// Open returns a service over the default paths.
func Open() (*Service, error) {
	paths, err := store.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return New(paths), nil
}

// Paths returns the files the service works on.
func (s *Service) Paths() store.Paths {
	return s.paths
}

// state is config.json and lock.json as loaded for one operation.
type state struct {
	cfg    store.Config
	lock   *store.Lock
	agents []agent.Agent // built-in and custom
}

func (s *Service) load() (*state, error) {
	cfg, err := store.LoadConfig(s.paths.Config)
	if err != nil {
		return nil, err
	}
	lock, err := store.LoadLock(s.paths.Lock)
	if err != nil {
		return nil, err
	}
	return &state{cfg: cfg, lock: lock, agents: agent.All(cfg.CustomAgents)}, nil
}

// mutate runs fn under the inter-process mutation lock with freshly loaded
// state, then saves lock.json. It saves even when fn fails part way, so the
// records match the filesystem changes that were applied.
func (s *Service) mutate(fn func(st *state) error) (err error) {
	release, err := store.Acquire(s.paths.Mutex)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	st, err := s.load()
	if err != nil {
		return err
	}
	err = fn(st)
	if saveErr := store.SaveLock(s.paths.Lock, st.lock); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("could not save %s: %w", s.paths.Lock, saveErr))
	}
	return err
}

// now is the timestamp written to records.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// hidden reports whether a directory entry is skipped by scans: dot entries
// include myskills' own staging dirs and .git.
func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

// tilde shortens a path inside the home directory to "~/…" for messages.
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, ok := within(home, p); ok {
		return filepath.Join("~", rel)
	}
	return p
}

// within returns p relative to dir when p lies strictly inside dir, compared
// lexically.
func within(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}

// realPath resolves the symlinks in p. When p does not exist, its parent is
// resolved and the rest appended, following dangling symlinks to where their
// target would be created: a missing agent dir below a symlinked parent (say
// .claude → .agents before .agents exists) maps to where it will be created.
func realPath(p string) string {
	links := 40 // like the system limit, against symlink cycles
	return followPath(p, &links)
}

func followPath(p string, links *int) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	full := filepath.Join(followPath(parent, links), filepath.Base(p))
	target, err := os.Readlink(full)
	if err != nil || *links == 0 {
		return full
	}
	*links--
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(full), target)
	}
	return followPath(target, links)
}

// lstat is os.Lstat with the identity of the file read now, for os.SameFile
// to tell later whether the path still holds the same file: Windows reads it
// lazily, from whatever is at the path by then.
func lstat(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err == nil {
		os.SameFile(info, info)
	}
	return info, err
}

// rename, removeAll and copySkill are os.Rename, os.RemoveAll and skill.Copy
// as used to replace and remove entries; tests replace them to fail a swap or
// to change files mid-operation.
var (
	rename    = os.Rename
	removeAll = os.RemoveAll
	copySkill = skill.Copy
)

// swapIn moves the prepared entry stage to dest. Whatever is at dest is first
// moved aside to a hidden ".old-*" sibling, restored if stage cannot take its
// place, and removed afterwards.
func swapIn(stage, dest string) error {
	old := ""
	if _, err := os.Lstat(dest); err == nil {
		old = filepath.Join(filepath.Dir(dest), ".old-"+strings.ToLower(rand.Text()))
		if err := os.Rename(dest, old); err != nil {
			return err
		}
	}
	if err := rename(stage, dest); err != nil {
		if old != "" {
			if rerr := os.Rename(old, dest); rerr != nil {
				return fmt.Errorf("%w; the previous version was kept at %s", err, old)
			}
		}
		return err
	}
	if old != "" {
		// A leftover hidden dir is harmless: scans skip it.
		_ = os.RemoveAll(old)
	}
	return nil
}
