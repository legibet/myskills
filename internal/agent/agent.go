// Package agent describes the coding agents whose skill directories myskills
// manages, and resolves those directories for a scope.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Agent is a coding agent with its skill directories.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Project is the skill directory relative to a project root; empty if the
	// agent has no project skills.
	Project string `json:"project,omitempty"`
	// Global is the user-level skill directory, absolute or starting with "~/";
	// empty if the agent has no global skills.
	Global string `json:"global,omitempty"`
	// GlobalEnv names an environment variable that, when set, overrides the
	// global directory with "$GlobalEnv/skills". Built-ins only.
	GlobalEnv string `json:"-"`
}

// Builtins are the agents myskills knows without configuration (docs/enabling.md).
// Order matters: it is the order used for display and deduplication.
var Builtins = []Agent{
	{ID: "universal", Name: "Universal", Project: ".agents/skills", Global: "~/.agents/skills"},
	{ID: "claude-code", Name: "Claude Code", Project: ".claude/skills", Global: "~/.claude/skills", GlobalEnv: "CLAUDE_CONFIG_DIR"},
	{ID: "antigravity", Name: "Antigravity", Project: ".agents/skills", Global: "~/.gemini/config/skills"},
}

// DefaultIDs are the default agents when the config does not set any.
var DefaultIDs = []string{"universal", "claude-code"}

// ProjectDir returns the agent's skill directory inside the project root.
func (a Agent) ProjectDir(root string) (string, bool) {
	if a.Project == "" {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(a.Project)), true
}

// GlobalDir returns the agent's absolute global skill directory.
func (a Agent) GlobalDir() (string, bool) {
	if a.GlobalEnv != "" {
		if dir := os.Getenv(a.GlobalEnv); dir != "" {
			return filepath.Join(dir, "skills"), true
		}
	}
	dir := a.Global
	if dir == "" {
		return "", false
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		dir = filepath.Join(home, dir[1:])
	}
	return filepath.Clean(dir), true
}

// All returns the built-ins followed by the custom agents.
func All(custom []Agent) []Agent {
	return slices.Concat(Builtins, custom)
}

// Find returns the agent with the given id.
func Find(agents []Agent, id string) (Agent, bool) {
	i := slices.IndexFunc(agents, func(a Agent) bool { return a.ID == id })
	if i < 0 {
		return Agent{}, false
	}
	return agents[i], true
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateCustom checks a user-defined agent (docs/storage.md): id format, no
// collision with built-ins, at least one directory, relative project dir
// without "..", absolute or "~/" global dir.
func ValidateCustom(a Agent) error {
	if !idPattern.MatchString(a.ID) {
		return fmt.Errorf("agent id %q must be lowercase letters, digits and hyphens, starting with a letter or digit", a.ID)
	}
	if _, ok := Find(Builtins, a.ID); ok {
		return fmt.Errorf("agent id %q is a built-in agent", a.ID)
	}
	if a.Project == "" && a.Global == "" {
		return fmt.Errorf("agent %q needs a project or global skill directory", a.ID)
	}
	if p := a.Project; p != "" {
		drive := len(p) >= 2 && p[1] == ':' && ('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
		if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "~") || drive {
			return fmt.Errorf("agent %q: project directory %q must be relative to the project root", a.ID, p)
		}
		parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
		if slices.Contains(parts, "..") {
			return fmt.Errorf("agent %q: project directory %q must not contain \"..\"", a.ID, p)
		}
	}
	if g := a.Global; g != "" && !filepath.IsAbs(g) && !strings.HasPrefix(g, "~/") {
		return fmt.Errorf("agent %q: global directory %q must be absolute or start with \"~/\"", a.ID, g)
	}
	return nil
}
