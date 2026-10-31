// Package store owns the files under ~/.myskills: paths, config.json,
// lock.json and the inter-process mutation lock (design §3).
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/legibet/myskills/internal/agent"
)

// LockVersion is the lock.json format version this build reads and writes.
const LockVersion = 1

// Paths locates the myskills files.
type Paths struct {
	Root   string // ~/.myskills or $MYSKILLS_HOME
	Skills string // Root/skills, the library
	Config string // Root/config.json
	Lock   string // Root/lock.json
	Mutex  string // Root/.lock
}

// DefaultPaths returns the paths under $MYSKILLS_HOME, or ~/.myskills.
func DefaultPaths() (Paths, error) {
	root := os.Getenv("MYSKILLS_HOME")
	if root == "" {
		root = "~/.myskills"
	}
	if root == "~" || strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(home, root[1:])
	}
	// Links point into the library by absolute path, so the root must be absolute.
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	return NewPaths(root), nil
}

// NewPaths returns the paths under root.
func NewPaths(root string) Paths {
	return Paths{
		Root:   root,
		Skills: filepath.Join(root, "skills"),
		Config: filepath.Join(root, "config.json"),
		Lock:   filepath.Join(root, "lock.json"),
		Mutex:  filepath.Join(root, ".lock"),
	}
}

// Config is config.json.
type Config struct {
	Agents       []string      `json:"agents,omitempty"`
	CustomAgents []agent.Agent `json:"customAgents,omitempty"`
}

// DefaultAgents returns Agents, or agent.DefaultIDs when unset.
func (c Config) DefaultAgents() []string {
	if len(c.Agents) > 0 {
		return c.Agents
	}
	return slices.Clone(agent.DefaultIDs)
}

// LoadConfig reads config.json. A missing file is an empty Config. Invalid JSON,
// invalid custom agents or unknown default agent ids are errors.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	// The file is hand-edited: reject unknown fields so typos surface.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			err = errors.New("empty file, expected a JSON object")
		}
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: unexpected data after the JSON object", path)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// SaveConfig writes config.json atomically. An invalid config is not written,
// since every later command would refuse to load it.
func SaveConfig(path string, c Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	data, err := marshal(c)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func (c Config) validate() error {
	seen := map[string]bool{}
	for _, a := range c.CustomAgents {
		if err := agent.ValidateCustom(a); err != nil {
			return err
		}
		if seen[a.ID] {
			return fmt.Errorf("duplicate custom agent %q", a.ID)
		}
		seen[a.ID] = true
	}
	all := agent.All(c.CustomAgents)
	for _, id := range c.Agents {
		if _, ok := agent.Find(all, id); !ok {
			return fmt.Errorf("unknown agent %q in agents", id)
		}
	}
	return nil
}

// Source is where a library skill came from.
type Source struct {
	Type string `json:"type"` // "git" or "local"
	URL  string `json:"url,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Path is the skill directory inside the repository ("" for the repository
	// root) for git sources, or the absolute skill directory for local sources.
	Path string `json:"path,omitempty"`
}

// SkillRecord describes an installed library skill.
type SkillRecord struct {
	Source      Source    `json:"source"`
	Commit      string    `json:"commit,omitempty"`
	Hash        string    `json:"hash"`
	InstalledAt time.Time `json:"installedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// CopyRecord describes a copy placement.
type CopyRecord struct {
	Skill       string    `json:"skill"`       // library path
	Hash        string    `json:"hash"`        // content of the copy when made
	LibraryHash string    `json:"libraryHash"` // content of the library skill it was copied from
	CopiedAt    time.Time `json:"copiedAt"`
}

// Lock is lock.json.
type Lock struct {
	Version  int                    `json:"version"`
	Skills   map[string]SkillRecord `json:"skills"`
	Copies   map[string]CopyRecord  `json:"copies"`
	Projects []string               `json:"projects"`
}

// LoadLock reads lock.json. A missing file is an empty Lock with non-nil maps.
// Invalid JSON or a version other than LockVersion is an error.
func LoadLock(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Lock{Version: LockVersion, Skills: map[string]SkillRecord{}, Copies: map[string]CopyRecord{}}, nil
	}
	if err != nil {
		return nil, err
	}
	// Check the version before decoding the rest, whose shape may differ in
	// other versions.
	var v struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if v.Version != LockVersion {
		return nil, fmt.Errorf("%s: unsupported version %d (expected %d); it was written by a different myskills version", path, v.Version, LockVersion)
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Skills == nil {
		l.Skills = map[string]SkillRecord{}
	}
	if l.Copies == nil {
		l.Copies = map[string]CopyRecord{}
	}
	return &l, nil
}

// SaveLock writes lock.json atomically (temp file in the same directory, then
// rename), with sorted keys and indentation.
func SaveLock(path string, l *Lock) error {
	out := *l
	out.Version = LockVersion
	if out.Skills == nil {
		out.Skills = map[string]SkillRecord{}
	}
	if out.Copies == nil {
		out.Copies = map[string]CopyRecord{}
	}
	out.Projects = slices.Compact(slices.Sorted(slices.Values(l.Projects)))
	if out.Projects == nil {
		out.Projects = []string{}
	}
	data, err := marshal(out)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// marshal encodes v as indented JSON with a trailing newline. Map keys are
// sorted by encoding/json.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileAtomic replaces path with data through a temp file in the same
// directory, so readers never see a partial file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644) // CreateTemp uses 0600
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// Acquire takes the exclusive inter-process lock at path, creating the file
// and its directory if needed, and blocks until it is available. The returned
// function releases it.
func Acquire(path string) (release func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() error {
		err := unlockFile(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}, nil
}
