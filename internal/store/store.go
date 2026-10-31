// Package store owns the files under ~/.myskills: paths, config.json,
// lock.json and the inter-process mutation lock (design §3).
package store

import (
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
	panic("not implemented")
}

// NewPaths returns the paths under root.
func NewPaths(root string) Paths {
	panic("not implemented")
}

// Config is config.json.
type Config struct {
	Agents       []string      `json:"agents,omitempty"`
	CustomAgents []agent.Agent `json:"customAgents,omitempty"`
}

// DefaultAgents returns Agents, or agent.DefaultIDs when unset.
func (c Config) DefaultAgents() []string {
	panic("not implemented")
}

// LoadConfig reads config.json. A missing file is an empty Config. Invalid JSON,
// invalid custom agents or unknown default agent ids are errors.
func LoadConfig(path string) (Config, error) {
	panic("not implemented")
}

// SaveConfig writes config.json atomically.
func SaveConfig(path string, c Config) error {
	panic("not implemented")
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
	Skill    string    `json:"skill"` // library path
	Hash     string    `json:"hash"`
	CopiedAt time.Time `json:"copiedAt"`
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
	panic("not implemented")
}

// SaveLock writes lock.json atomically (temp file in the same directory, then
// rename), with sorted keys and indentation.
func SaveLock(path string, l *Lock) error {
	panic("not implemented")
}

// Acquire takes the exclusive inter-process lock at path, creating the file
// and its directory if needed, and blocks until it is available. The returned
// function releases it.
func Acquire(path string) (release func() error, err error) {
	panic("not implemented")
}
