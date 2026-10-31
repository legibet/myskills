// Package agent describes the coding agents whose skill directories myskills
// manages, and resolves those directories for a scope.
package agent

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

// Builtins are the agents myskills knows without configuration (design §6).
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
	panic("not implemented")
}

// GlobalDir returns the agent's absolute global skill directory.
func (a Agent) GlobalDir() (string, bool) {
	panic("not implemented")
}

// All returns the built-ins followed by the custom agents.
func All(custom []Agent) []Agent {
	panic("not implemented")
}

// Find returns the agent with the given id.
func Find(agents []Agent, id string) (Agent, bool) {
	panic("not implemented")
}

// ValidateCustom checks a user-defined agent (design §3): id format, no
// collision with built-ins, at least one directory, relative project dir
// without "..", absolute or "~/" global dir.
func ValidateCustom(a Agent) error {
	panic("not implemented")
}
