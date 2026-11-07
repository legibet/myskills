package agent

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProjectDir(t *testing.T) {
	root := filepath.FromSlash("/work/proj")
	tests := []struct {
		project string
		want    string
		ok      bool
	}{
		{".claude/skills", filepath.FromSlash("/work/proj/.claude/skills"), true},
		{".grok/skills/", filepath.FromSlash("/work/proj/.grok/skills"), true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := Agent{ID: "x", Project: tt.project}.ProjectDir(root)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%q: got %q, %v; want %q, %v", tt.project, got, ok, tt.want, tt.ok)
		}
	}
}

func TestGlobalDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("MYSKILLS_TEST_ENV", "")

	tests := []struct {
		name   string
		agent  Agent
		env    string
		want   string
		wantOK bool
	}{
		{"home relative", Agent{Global: "~/.agents/skills"}, "", filepath.Join(home, ".agents", "skills"), true},
		{"trailing slash", Agent{Global: "~/.grok/skills/"}, "", filepath.Join(home, ".grok", "skills"), true},
		{"home itself", Agent{Global: "~"}, "", home, true},
		{"absolute", Agent{Global: "/opt/skills/"}, "", filepath.Clean("/opt/skills"), true},
		{"none", Agent{Project: ".x/skills"}, "", "", false},
		{"env set", Agent{Global: "~/.claude/skills", GlobalEnv: "MYSKILLS_TEST_ENV"}, filepath.FromSlash("/cfg/claude"), filepath.FromSlash("/cfg/claude/skills"), true},
		{"env trailing slash", Agent{Global: "~/.claude/skills", GlobalEnv: "MYSKILLS_TEST_ENV"}, "/cfg/claude/", filepath.FromSlash("/cfg/claude/skills"), true},
		{"env empty", Agent{Global: "~/.claude/skills", GlobalEnv: "MYSKILLS_TEST_ENV"}, "", filepath.Join(home, ".claude", "skills"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MYSKILLS_TEST_ENV", tt.env)
			got, ok := tt.agent.GlobalDir()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestGlobalDirClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	claude, _ := Find(Builtins, "claude-code")

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got, _ := claude.GlobalDir(); got != filepath.Join(home, ".claude", "skills") {
		t.Errorf("default: got %q", got)
	}
	cfg := filepath.Join(home, "cfg")
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if got, _ := claude.GlobalDir(); got != filepath.Join(cfg, "skills") {
		t.Errorf("CLAUDE_CONFIG_DIR: got %q", got)
	}
	// Only the agent that names the variable is affected.
	universal, _ := Find(Builtins, "universal")
	if got, _ := universal.GlobalDir(); got != filepath.Join(home, ".agents", "skills") {
		t.Errorf("universal: got %q", got)
	}
}

func TestGlobalDirNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "") // Plan 9
	if got, ok := (Agent{Global: "~/.agents/skills"}).GlobalDir(); ok {
		t.Errorf("got %q, want no dir", got)
	}
}

func TestAllAndFind(t *testing.T) {
	custom := []Agent{{ID: "pi", Name: "Pi", Global: "~/.pi/agent/skills"}}
	all := All(custom)
	var ids []string
	for _, a := range all {
		ids = append(ids, a.ID)
	}
	if want := []string{"universal", "claude-code", "antigravity", "pi"}; !slices.Equal(ids, want) {
		t.Errorf("ids %v, want %v", ids, want)
	}
	// The result does not share Builtins' backing array.
	all[0].ID = "changed"
	if Builtins[0].ID != "universal" {
		t.Error("All aliases Builtins")
	}

	if a, ok := Find(All(custom), "pi"); !ok || a.Name != "Pi" {
		t.Errorf("Find pi: %+v, %v", a, ok)
	}
	if _, ok := Find(All(nil), "pi"); ok {
		t.Error("Find pi without custom agents: found")
	}
	for _, id := range DefaultIDs {
		if _, ok := Find(Builtins, id); !ok {
			t.Errorf("default agent %q is not a built-in", id)
		}
	}
}

func TestValidateCustom(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{"global only", Agent{ID: "pi", Name: "Pi", Global: "~/.pi/agent/skills"}, ""},
		{"project only", Agent{ID: "grok", Project: ".grok/skills"}, ""},
		{"both", Agent{ID: "grok-2", Project: ".grok/skills", Global: "~/.grok/skills/"}, ""},
		{"absolute global", Agent{ID: "x", Global: t.TempDir()}, ""},
		{"digit first", Agent{ID: "9x", Project: "skills"}, ""},
		{"backslash project", Agent{ID: "x", Project: `.x\skills`}, ""},
		{"dotted segment", Agent{ID: "x", Project: "./skills/..x"}, ""},

		{"empty id", Agent{Project: "skills"}, `agent id "" must be lowercase letters, digits and hyphens`},
		{"uppercase id", Agent{ID: "Pi", Project: "skills"}, `agent id "Pi" must be`},
		{"leading hyphen", Agent{ID: "-pi", Project: "skills"}, `agent id "-pi" must be`},
		{"underscore", Agent{ID: "p_i", Project: "skills"}, `agent id "p_i" must be`},
		{"builtin", Agent{ID: "claude-code", Project: "skills"}, `agent id "claude-code" is a built-in agent`},
		{"no dirs", Agent{ID: "pi"}, `agent "pi" needs a project or global skill directory`},
		{"absolute project", Agent{ID: "x", Project: "/skills"}, `agent "x": project directory "/skills" must be relative to the project root`},
		{"backslash rooted project", Agent{ID: "x", Project: `\skills`}, "must be relative to the project root"},
		{"home project", Agent{ID: "x", Project: "~/skills"}, "must be relative to the project root"},
		{"drive project", Agent{ID: "x", Project: "C:/skills"}, "must be relative to the project root"},
		{"drive relative project", Agent{ID: "x", Project: "c:skills"}, "must be relative to the project root"},
		{"dotdot project", Agent{ID: "x", Project: "../skills"}, `agent "x": project directory "../skills" must not contain ".."`},
		{"inner dotdot project", Agent{ID: "x", Project: "a/../../skills"}, `must not contain ".."`},
		{"backslash dotdot project", Agent{ID: "x", Project: `a\..\..\skills`}, `must not contain ".."`},
		{"relative global", Agent{ID: "x", Global: ".x/skills"}, `agent "x": global directory ".x/skills" must be absolute or start with "~/"`},
		{"tilde user global", Agent{ID: "x", Global: "~bob/skills"}, `must be absolute or start with "~/"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCustom(tt.agent)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %v, want %q", err, tt.wantErr)
			}
		})
	}
}
