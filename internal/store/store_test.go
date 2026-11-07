package store

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/legibet/myskills/internal/agent"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertOnlyFiles checks that dir holds exactly the given names (no temp files).
func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %v, want %v", dir, got, want)
	}
}

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	tests := []struct {
		env  string
		want string
	}{
		{"", filepath.Join(home, ".myskills")},
		{"~/data/myskills", filepath.Join(home, "data", "myskills")},
		{"~", home},
		{filepath.Join(cwd, "abs") + "/", filepath.Join(cwd, "abs")},
		{"rel", filepath.Join(cwd, "rel")},
	}
	for _, tt := range tests {
		t.Setenv("MYSKILLS_HOME", tt.env)
		p, err := DefaultPaths()
		if err != nil {
			t.Fatal(err)
		}
		if p != NewPaths(tt.want) {
			t.Errorf("MYSKILLS_HOME=%q: got %+v, want root %q", tt.env, p, tt.want)
		}
	}
}

func TestNewPaths(t *testing.T) {
	root := filepath.FromSlash("/x/.myskills")
	want := Paths{
		Root:   root,
		Skills: filepath.Join(root, "skills"),
		Config: filepath.Join(root, "config.json"),
		Lock:   filepath.Join(root, "lock.json"),
		Mutex:  filepath.Join(root, ".lock"),
	}
	if got := NewPaths(root); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestConfigMissing(t *testing.T) {
	c, err := LoadConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Config{}) {
		t.Errorf("got %+v", c)
	}
	if got := c.DefaultAgents(); !slices.Equal(got, agent.DefaultIDs) {
		t.Errorf("DefaultAgents: got %v", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	want := Config{
		Agents: []string{"pi", "claude-code"},
		CustomAgents: []agent.Agent{
			{ID: "pi", Name: "Pi", Global: "~/.pi/agent/skills"},
			{ID: "grok", Name: "Grok", Project: ".grok/skills", Global: "~/.grok/skills"},
		},
	}
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := got.DefaultAgents(); !slices.Equal(got, want.Agents) {
		t.Errorf("DefaultAgents: got %v", got)
	}
	assertOnlyFiles(t, filepath.Dir(path), "config.json")

	// An empty config is written as an empty object.
	if err := SaveConfig(path, Config{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}\n" {
		t.Errorf("empty config: %q", data)
	}
}

func TestConfigInvalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"unknown field", `{"agnets": ["universal"]}`, `json: unknown field "agnets"`},
		{"unknown agent field", `{"customAgents": [{"id": "pi", "global": "~/x", "globalEnv": "X"}]}`, `json: unknown field "globalEnv"`},
		{"invalid json", `{"agents": [}`, "invalid character"},
		{"wrong type", `{"agents": "universal"}`, "cannot unmarshal"},
		{"empty file", "", "empty file"},
		{"trailing data", `{} {}`, "unexpected data after the JSON object"},
		{"invalid custom agent", `{"customAgents": [{"id": "pi", "global": "relative"}]}`, `agent "pi": global directory "relative" must be absolute`},
		{"builtin id", `{"customAgents": [{"id": "universal", "project": "x"}]}`, `agent id "universal" is a built-in agent`},
		{"duplicate custom", `{"customAgents": [{"id": "pi", "project": "x"}, {"id": "pi", "project": "y"}]}`, `duplicate custom agent "pi"`},
		{"unknown default agent", `{"agents": ["universal", "nope"]}`, `unknown agent "nope" in agents`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.content)
			_, err := LoadConfig(path)
			if err == nil || !strings.HasPrefix(err.Error(), path+": ") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %v, want %q with the path", err, tt.wantErr)
			}
		})
	}
}

func TestSaveConfigInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := SaveConfig(path, Config{Agents: []string{"nope"}}); err == nil {
		t.Error("no error")
	}
	assertOnlyFiles(t, dir)
}

func TestLockMissing(t *testing.T) {
	l, err := LoadLock(filepath.Join(t.TempDir(), "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := &Lock{Version: LockVersion, Skills: map[string]SkillRecord{}, Copies: map[string]CopyRecord{}}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("got %+v", l)
	}
}

func TestLockRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	path := filepath.Join(dir, "lock.json")
	at := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	l := &Lock{
		Version: LockVersion,
		Skills: map[string]SkillRecord{
			"tdd": {
				Source: Source{Type: "git", URL: "https://github.com/o/r.git", Path: "skills/tdd"},
				Commit: "3f2c", Hash: "v1:aa", InstalledAt: at, UpdatedAt: at.Add(time.Hour),
			},
			"a&b": {Source: Source{Type: "local", Path: "/dev/a&b"}, Hash: "v1:bb", InstalledAt: at, UpdatedAt: at},
		},
		Copies: map[string]CopyRecord{
			"/proj/.agents/skills/tdd": {Skill: "tdd", Hash: "v1:cc", LibraryHash: "v1:dd", CopiedAt: at},
		},
		Projects: []string{"/z", "/a", "/z", "/m", "/a"},
	}
	if err := SaveLock(path, l); err != nil {
		t.Fatal(err)
	}
	// The caller's lock is not modified.
	if !slices.Equal(l.Projects, []string{"/z", "/a", "/z", "/m", "/a"}) {
		t.Errorf("SaveLock modified Projects: %v", l.Projects)
	}

	got, err := LoadLock(path)
	if err != nil {
		t.Fatal(err)
	}
	want := *l
	want.Projects = []string{"/a", "/m", "/z"}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "{\n  \"version\": 1,\n  \"skills\": {\n    \"a&b\": {") || !strings.HasSuffix(text, "]\n}\n") {
		t.Errorf("unexpected format:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 && runtime.GOOS != "windows" {
		t.Errorf("mode %v, want 0644", info.Mode().Perm())
	}
	assertOnlyFiles(t, dir, "lock.json")
}

func TestSaveLockEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	if err := SaveLock(path, &Lock{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"version\": 1,\n  \"skills\": {},\n  \"copies\": {},\n  \"projects\": []\n}\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

func TestLoadLockNullMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	writeFile(t, path, `{"version": 1, "skills": null}`)
	l, err := LoadLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.Skills == nil || l.Copies == nil {
		t.Errorf("nil maps: %+v", l)
	}
}

func TestLoadLockInvalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"invalid json", `{"version": 1,`, "unexpected end of JSON input"},
		{"newer version", `{"version": 2, "skills": []}`, "unsupported version 2 (expected 1); it was written by a different myskills version"},
		{"missing version", `{"skills": {}}`, "unsupported version 0"},
		{"wrong type", `{"version": 1, "skills": []}`, "cannot unmarshal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock.json")
			writeFile(t, path, tt.content)
			_, err := LoadLock(path)
			if err == nil || !strings.HasPrefix(err.Error(), path+": ") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %v, want %q with the path", err, tt.wantErr)
			}
		})
	}
}

func TestSaveLockFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	// Renaming onto a non-empty directory fails.
	path := filepath.Join(dir, "lock.json")
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveLock(path, &Lock{}); err == nil {
		t.Fatal("no error")
	}
	assertOnlyFiles(t, dir, "lock.json")
}

func TestAcquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home", ".lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func() error)
	go func() {
		release2, err := Acquire(path)
		if err != nil {
			t.Error(err)
			close(acquired)
			return
		}
		acquired <- release2
	}()

	select {
	case <-acquired:
		t.Fatal("second Acquire did not block")
	case <-time.After(200 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case release2 := <-acquired:
		if release2 == nil {
			t.FailNow()
		}
		if err := release2(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second Acquire still blocked after release")
	}

	// The lock can be taken again after both releases.
	release3, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := release3(); err != nil {
		t.Fatal(err)
	}
}
