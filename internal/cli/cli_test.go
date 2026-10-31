package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/legibet/myskills/internal/core"
)

// env is an isolated home in which myskills runs in-process.
type env struct {
	t        *testing.T
	tmp      string // resolved temp dir holding home, sources and projects
	home     string
	skillsSH string // base URL of a fake skills.sh
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, "home")
	mkdirAll(t, home)
	t.Setenv("HOME", home)
	t.Setenv("MYSKILLS_HOME", filepath.Join(home, ".myskills"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	return &env{t: t, tmp: tmp, home: home}
}

type result struct {
	code     int
	out, err string
}

// run runs myskills with args in dir, with stdin, as a non-terminal.
func (e *env) run(dir, stdin string, args ...string) result {
	e.t.Helper()
	e.t.Chdir(dir)
	var out, errOut bytes.Buffer
	open := func() (*core.Service, error) {
		svc, err := core.Open()
		if err == nil && e.skillsSH != "" {
			svc.SkillsSH.BaseURL = e.skillsSH
		}
		return svc, err
	}
	s := streams{in: strings.NewReader(stdin), out: &out, err: &errOut}
	code := run(context.Background(), args, s, open)
	return result{code: code, out: out.String(), err: errOut.String()}
}

// ok runs myskills and fails the test unless it exits with 0.
func (e *env) ok(dir string, args ...string) result {
	e.t.Helper()
	r := e.run(dir, "", args...)
	if r.code != 0 {
		e.t.Fatalf("myskills %s: exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), r.code, r.out, r.err)
	}
	return r
}

// fails runs myskills and fails the test unless it exits with 1.
func (e *env) fails(dir string, args ...string) result {
	e.t.Helper()
	r := e.run(dir, "", args...)
	if r.code != 1 {
		e.t.Fatalf("myskills %s: exit %d, want 1\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), r.code, r.out, r.err)
	}
	return r
}

func (e *env) path(parts ...string) string {
	return filepath.Join(append([]string{e.tmp}, parts...)...)
}

// repo creates a git repository at tmp/src with skills (name → directory in
// the repository) and returns its directory and file:// URL.
func (e *env) repo(skills map[string]string) (dir, url string) {
	e.t.Helper()
	dir = e.path("src")
	gitT(e.t, "", "init", "-q", "-b", "main", dir)
	for skillName, rel := range skills {
		writeSkill(e.t, filepath.Join(dir, filepath.FromSlash(rel)), skillName, skillName+" body")
	}
	commitAll(e.t, dir)
	return dir, "file://" + filepath.ToSlash(dir)
}

// project creates a git repository with one commit at tmp/proj.
func (e *env) project() string {
	e.t.Helper()
	dir := e.path("proj")
	gitT(e.t, "", "init", "-q", "-b", "main", dir)
	writeFile(e.t, filepath.Join(dir, "README.md"), "project\n")
	commitAll(e.t, dir)
	return dir
}

func (e *env) libDir(libPath string) string {
	return filepath.Join(e.home, ".myskills", "skills", filepath.FromSlash(libPath))
}

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: The %s skill.\n---\n\n# %s\n\n%s\n", name, name, name, body))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "commit")
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// contains fails the test unless s contains every want.
func contains(t *testing.T, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("output does not contain %q:\n%s", w, s)
		}
	}
}

func isLink(t *testing.T, p string) bool {
	t.Helper()
	info, err := os.Lstat(p)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, s)
	}
	return v
}

func TestRootWithoutTerminalPrintsHelp(t *testing.T) {
	e := newEnv(t)
	r := e.ok(e.home)
	contains(t, r.out, "Usage:", "myskills [command]", "enable", "Library:")
}

func TestVersion(t *testing.T) {
	e := newEnv(t)
	r := e.ok(e.home, "--version")
	if !strings.HasPrefix(r.out, "myskills ") {
		t.Errorf("version output = %q", r.out)
	}
}

func TestUnknownCommandAndFlag(t *testing.T) {
	e := newEnv(t)
	r := e.fails(e.home, "lsit")
	contains(t, r.err, `✗ unknown command "lsit"`, `did you mean "list"?`, "Run 'myskills --help' for usage.")
	if strings.Contains(r.err, "Error:") || r.out != "" {
		t.Errorf("cobra's own error output leaked:\nstdout: %s\nstderr: %s", r.out, r.err)
	}

	r = e.fails(e.home, "list", "--bogus")
	contains(t, r.err, "✗ unknown flag: --bogus", "Run 'myskills list --help' for usage.")

	r = e.fails(e.home, "show")
	contains(t, r.err, "✗ accepts 1 arg(s), received 0", "Run 'myskills show --help'")
}

func TestAddSingleSkill(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "skills/tdd"})

	r := e.ok(e.home, "add", url)
	contains(t, r.out, "✓ added tdd ("+e.path("src")+", skills/tdd)", "Enable with: myskills enable tdd")
	if !exists(filepath.Join(e.libDir("tdd"), "SKILL.md")) {
		t.Fatal("tdd was not installed")
	}

	r = e.ok(e.home, "add", url)
	contains(t, r.out, "! tdd is already in the library", "myskills update tdd")
}

func TestAddSeveralSkills(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"alpha": "skills/alpha", "beta": "skills/beta", "gamma": "skills/gamma"})

	r := e.fails(e.home, "add", url)
	contains(t, r.err, "has 3 skills; choose with -s <name> (repeatable) or --all", "alpha", "skills/alpha", "gamma")
	if exists(e.libDir("alpha")) {
		t.Fatal("something was installed without a choice")
	}

	r = e.ok(e.home, "add", url, "-s", "alpha", "-s", "beta")
	contains(t, r.out, "✓ added alpha", "✓ added beta")
	if strings.Contains(r.out, "gamma") {
		t.Errorf("gamma was added:\n%s", r.out)
	}

	r = e.ok(e.home, "add", url, "-s", "gamma")
	contains(t, r.out, "✓ added gamma")

	r = e.fails(e.home, "add", url, "-s", "delta")
	contains(t, r.err, `no skill "delta"`, "alpha, beta, gamma")

	r = e.fails(e.home, "add", url, "-s", "alpha", "-s", "delta")
	contains(t, r.err, `no skill "delta"`)

	r = e.fails(e.home, "add", url, "--all", "-s", "alpha")
	contains(t, r.err, "--all can't be combined with -s")
}

func TestAddAll(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo(map[string]string{"alpha": "alpha", "beta": "beta"})
	writeFile(t, filepath.Join(dir, "broken", "SKILL.md"), "no frontmatter\n")
	commitAll(t, dir)

	r := e.fails(e.home, "add", url, "--all")
	contains(t, r.out, "✓ added alpha", "✓ added beta", "Enable with: myskills enable alpha beta")
	contains(t, r.err, "✗ broken can't be added:")
}

func TestAddLocal(t *testing.T) {
	e := newEnv(t)
	coll := e.path("collection")
	writeSkill(t, filepath.Join(coll, "one"), "one", "body")
	writeSkill(t, filepath.Join(coll, "two"), "two", "body")

	r := e.ok(e.home, "add", coll, "-s", "one")
	contains(t, r.out, "✓ added one ("+filepath.Join(coll, "one")+")")
	if isLink(t, e.libDir("one")) || !exists(e.libDir("one")) {
		t.Error("one should be a copied folder")
	}

	r = e.ok(e.home, "add", coll, "--link")
	contains(t, r.out, "✓ linked collection → "+coll)
	if !isLink(t, e.libDir("collection")) {
		t.Error("collection should be a link")
	}
	r = e.ok(e.home, "list")
	contains(t, r.out, "collection/two", "linked "+coll)

	_, url := e.repo(map[string]string{"x": "x"})
	r = e.fails(e.home, "add", url, "--link")
	contains(t, r.err, "--link works with local folders only")
	r = e.fails(e.home, "add", coll, "--link", "-s", "one")
	contains(t, r.err, "can't be combined")
}

func TestListAndShow(t *testing.T) {
	e := newEnv(t)
	r := e.ok(e.home, "list")
	contains(t, r.out, "The library is empty.")
	r = e.ok(e.home, "list", "--json")
	if got := decode[listJSON](t, r.out); got.Skills == nil || len(got.Skills) != 0 {
		t.Errorf("empty list JSON = %s", r.out)
	}

	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()
	e.ok(proj, "enable", "tdd")
	e.ok(proj, "enable", "-g", "tdd")

	r = e.ok(e.home, "ls")
	contains(t, r.out, "NAME", "SOURCE", "ENABLED", "tdd", "src", "global + 1 project")

	r = e.ok(e.home, "list", "--json")
	var raw map[string][]map[string]any
	if err := json.Unmarshal([]byte(r.out), &raw); err != nil {
		t.Fatal(err)
	}
	sk := raw["skills"][0]
	for _, key := range []string{"path", "name", "description", "dir", "source", "sourceLabel", "linked", "updatable", "modified", "enabled"} {
		if _, ok := sk[key]; !ok {
			t.Errorf("list JSON lacks %q: %s", key, r.out)
		}
	}
	got := decode[listJSON](t, r.out).Skills[0]
	if got.Path != "tdd" || got.Source == nil || got.Source.Type != "git" || got.Source.Commit == "" ||
		!got.Enabled.Global || len(got.Enabled.Projects) != 1 || got.Enabled.Projects[0] != proj {
		t.Errorf("list JSON = %+v", got)
	}

	// Local changes show as an issue.
	writeFile(t, filepath.Join(e.libDir("tdd"), "extra.md"), "x")
	r = e.ok(e.home, "list")
	contains(t, r.out, "modified")

	r = e.ok(e.home, "show", "tdd")
	contains(t, r.out, "tdd\nThe tdd skill.", "path", "~/.myskills/skills/tdd", "source", "enabled   global + 1 project", "# tdd\n\ntdd body")
	if strings.Contains(r.out, "description:") {
		t.Errorf("show printed the frontmatter:\n%s", r.out)
	}

	r = e.ok(e.home, "path", "tdd")
	if r.out != e.libDir("tdd")+"\n" {
		t.Errorf("path = %q", r.out)
	}
}

func TestUnknownSkill(t *testing.T) {
	e := newEnv(t)
	proj := e.project()
	for _, args := range [][]string{{"show", "nope"}, {"path", "nope"}, {"enable", "nope"}, {"remove", "-y", "nope"}} {
		r := e.fails(proj, args...)
		contains(t, r.err, `✗ no skill "nope" in the library`, "myskills list")
	}
	r := e.fails(proj, "disable", "nope")
	contains(t, r.err, `✗ no skill or broken placement "nope"`)

	writeSkill(t, e.path("tdd-go"), "tdd-go", "body")
	e.ok(e.home, "add", e.path("tdd-go"))
	r = e.fails(proj, "enable", "tdd", "tdd-go")
	contains(t, r.err, `✗ no skill "tdd" in the library`, "Did you mean tdd-go?")
	contains(t, r.out, "✓ linked tdd-go → .agents/skills/tdd-go")
}

func TestAmbiguousSkill(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"a", "b"} {
		writeSkill(t, e.path(c, "review"), "review", "body")
		e.ok(e.home, "add", e.path(c), "--link")
	}
	r := e.fails(e.home, "show", "review")
	contains(t, r.err, `"review" matches several skills: a/review, b/review`)
	e.ok(e.home, "show", "a/review")
}

func TestEnableDisableLink(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()

	r := e.ok(proj, "enable", "tdd")
	contains(t, r.out,
		"✓ linked tdd → .agents/skills/tdd (Universal)",
		"✓ linked tdd → .claude/skills/tdd (Claude Code)")
	for _, p := range []string{".agents/skills/tdd", ".claude/skills/tdd"} {
		if !isLink(t, filepath.Join(proj, p)) {
			t.Errorf("%s is not a link", p)
		}
	}

	r = e.ok(proj, "enable", "tdd")
	contains(t, r.out, "✓ tdd already linked → .agents/skills/tdd (Universal)")

	r = e.ok(proj, "status")
	contains(t, r.out, "Project "+proj, "  tdd  link  Universal, Antigravity, Claude Code", "Global\n  nothing enabled")

	r = e.ok(proj, "disable", "tdd", "-a", "claude-code")
	contains(t, r.out, "✓ unlinked tdd from .claude/skills/tdd (Claude Code)")
	if exists(filepath.Join(proj, ".claude/skills/tdd")) || !exists(filepath.Join(proj, ".agents/skills/tdd")) {
		t.Error("disable -a removed the wrong placements")
	}
	r = e.ok(proj, "status")
	contains(t, r.out, "tdd  link  Universal, Antigravity  partial")

	r = e.ok(proj, "disable", "tdd")
	contains(t, r.out, "✓ unlinked tdd from .agents/skills/tdd (Universal, Antigravity)")
	r = e.ok(proj, "disable", "tdd")
	contains(t, r.out, "! tdd is not enabled in "+proj)

	// -C picks the project from another directory.
	r = e.ok(e.home, "enable", "-C", filepath.Join(proj, ".agents"), "tdd", "-a", "universal")
	contains(t, r.out, "✓ linked tdd → .agents/skills/tdd (Universal)")
}

func TestEnableCopyAndModified(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()

	r := e.ok(proj, "enable", "tdd", "--copy", "-a", "universal")
	contains(t, r.out, "✓ copied tdd → .agents/skills/tdd (Universal)")
	copyDir := filepath.Join(proj, ".agents/skills/tdd")
	if isLink(t, copyDir) || !exists(filepath.Join(copyDir, "SKILL.md")) {
		t.Fatal("not copied")
	}

	writeFile(t, filepath.Join(copyDir, "notes.md"), "mine")
	r = e.ok(proj, "status")
	contains(t, r.out, "tdd  copy  Universal, Antigravity  modified, partial")

	r = e.fails(proj, "enable", "tdd", "--link", "-a", "universal")
	contains(t, r.err, "✗ ", "local changes", "Use --force to discard the local changes.")
	r = e.fails(proj, "disable", "tdd")
	contains(t, r.err, "local changes", "--force")
	if !exists(filepath.Join(copyDir, "notes.md")) {
		t.Fatal("modified copy was deleted")
	}

	r = e.ok(proj, "disable", "tdd", "--force")
	contains(t, r.out, "✓ removed tdd from .agents/skills/tdd (Universal, Antigravity)")

	r = e.fails(proj, "enable", "tdd", "--link", "--copy")
	contains(t, r.err, "--link and --copy can't be used together")
}

func TestEnableConflict(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()
	writeFile(t, filepath.Join(proj, ".agents/skills/tdd/SKILL.md"), "mine")

	r := e.fails(proj, "enable", "tdd")
	contains(t, r.err, "✗ "+filepath.Join(proj, ".agents/skills/tdd")+" already exists (not managed by myskills)")
	if exists(filepath.Join(proj, ".claude/skills/tdd")) {
		t.Error("a failed enable changed other placements")
	}
	r = e.ok(proj, "status")
	contains(t, r.out, "tdd  other  .agents/skills/tdd")
}

func TestEnableScopeErrors(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)

	r := e.fails(e.home, "enable", "tdd")
	contains(t, r.err, "home directory, not a project", "Use -g for the global scope")

	r = e.fails(e.home, "enable")
	contains(t, r.err, "✗ no skills given", "Run 'myskills enable --help'")
	r = e.fails(e.home, "disable", "-g")
	contains(t, r.err, "✗ no skills given")

	r = e.ok(e.home, "enable", "-g", "tdd")
	contains(t, r.out, "✓ linked tdd → ~/.agents/skills/tdd (Universal)", "✓ linked tdd → ~/.claude/skills/tdd (Claude Code)")
	if !isLink(t, filepath.Join(e.home, ".claude/skills/tdd")) {
		t.Error("global link missing")
	}
	r = e.ok(e.home, "status")
	contains(t, r.out, "Global\n  tdd  link  Universal, Claude Code")
	if strings.Contains(r.out, "Project") {
		t.Errorf("status in home shows a project:\n%s", r.out)
	}

	r = e.fails(e.home, "enable", "-g", "tdd", "-a", "nope")
	contains(t, r.err, `unknown agent "nope"`)
}

func TestStatusJSON(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()
	e.ok(proj, "enable", "tdd", "--copy")
	writeFile(t, filepath.Join(proj, ".claude/skills/mine/SKILL.md"), "x")

	r := e.ok(proj, "status", "--json")
	var raw map[string]any
	if err := json.Unmarshal([]byte(r.out), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["project"]; !ok {
		t.Errorf("status JSON lacks project: %s", r.out)
	}
	got := decode[statusJSON](t, r.out)
	if got.Project == nil || got.Project.Root != proj || len(got.Project.Enablements) != 1 {
		t.Fatalf("status JSON = %s", r.out)
	}
	en := got.Project.Enablements[0]
	if en.Skill != "tdd" || en.Mode != "copy" || len(en.Placements) != 2 || strings.Join(en.Agents, ",") != "universal,antigravity,claude-code" {
		t.Errorf("enablement = %+v", en)
	}
	if len(got.Project.Others) != 1 || got.Project.Others[0].Kind != "other" || got.Project.Others[0].Name != "mine" {
		t.Errorf("others = %+v", got.Project.Others)
	}
	if got.Global.Enablements == nil || got.Global.Broken == nil || got.Global.Root != "" {
		t.Errorf("global = %+v", got.Global)
	}

	r = e.ok(proj, "status", "-g", "--json")
	if got := decode[statusJSON](t, r.out); got.Project != nil {
		t.Errorf("status -g has a project: %s", r.out)
	}
}

func TestRemove(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd", "lint": "lint"})
	e.ok(e.home, "add", url, "--all")
	proj := e.project()
	e.ok(proj, "enable", "tdd")
	e.ok(proj, "enable", "lint", "--copy", "-a", "universal")

	r := e.fails(proj, "remove", "tdd")
	contains(t, r.err, "✗ removing skills needs confirmation", "Pass -y")
	if !exists(e.libDir("tdd")) {
		t.Fatal("removed without confirmation")
	}

	r = e.ok(proj, "rm", "-y", "tdd", "lint")
	contains(t, r.out,
		"✓ removed tdd",
		"✓ unlinked "+filepath.Join(proj, ".agents/skills/tdd"),
		"✓ removed lint",
		"! left the copy at "+filepath.Join(proj, ".agents/skills/lint"))
	if exists(e.libDir("tdd")) || exists(filepath.Join(proj, ".claude/skills/tdd")) {
		t.Error("tdd or its link is still there")
	}
	if !exists(filepath.Join(proj, ".agents/skills/lint/SKILL.md")) {
		t.Error("the copy was deleted")
	}
}

func TestRemoveLinkedEntry(t *testing.T) {
	e := newEnv(t)
	coll := e.path("collection")
	writeSkill(t, filepath.Join(coll, "one"), "one", "body")
	writeSkill(t, filepath.Join(coll, "two"), "two", "body")
	e.ok(e.home, "add", coll, "--link")

	r := e.ok(e.home, "remove", "-y", "one")
	contains(t, r.out,
		"! collection/one is in the linked entry collection: the whole entry (2 skills) will be removed",
		"✓ removed collection (linked entry with 2 skills; files in "+coll+" untouched)")
	if exists(e.libDir("collection")) || !exists(filepath.Join(coll, "two", "SKILL.md")) {
		t.Error("wrong things removed")
	}
}

func TestRemoveConfirm(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)

	// Prompts need a terminal; drive the prompt path directly.
	for _, tc := range []struct {
		answer  string
		removed bool
	}{{"n\n", false}, {"", false}, {"yes\n", true}} {
		var out, errOut bytes.Buffer
		s := streams{in: strings.NewReader(tc.answer), out: &out, err: &errOut, interactive: true}
		t.Chdir(e.home)
		if code := run(context.Background(), []string{"remove", "tdd"}, s, core.Open); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		contains(t, errOut.String(), "Remove tdd from the library? [y/N]")
		if got := !exists(e.libDir("tdd")); got != tc.removed {
			t.Errorf("answer %q: removed = %v", tc.answer, got)
		}
	}
}

func TestRemoveConfirmInterrupted(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)

	// The answer never comes; the interrupt must end the prompt.
	stdin, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut bytes.Buffer
	s := streams{in: stdin, out: &out, err: &errOut, interactive: true}
	t.Chdir(e.home)
	time.AfterFunc(100*time.Millisecond, cancel)
	if code := run(ctx, []string{"remove", "tdd"}, s, core.Open); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, errOut.String(), "✗ interrupted")
	if !exists(e.libDir("tdd")) {
		t.Error("tdd was removed")
	}
}

func TestOutputIsSanitized(t *testing.T) {
	e := newEnv(t)
	proj := e.project()
	// An entry myskills doesn't manage, named by whoever made the project.
	mkdirAll(t, filepath.Join(proj, ".agents", "skills", "evil\x1b]0;pwned\x07\x1b[2J"))

	r := e.ok(proj, "status")
	contains(t, r.out, "evil")
	if strings.ContainsRune(r.out, '\x1b') || strings.ContainsRune(r.out, '\a') {
		t.Errorf("escape sequences reached the output: %q", r.out)
	}
}

func TestCheckAndUpdate(t *testing.T) {
	e := newEnv(t)
	src, url := e.repo(map[string]string{"tdd": "tdd", "lint": "lint"})
	e.ok(e.home, "add", url, "--all")
	proj := e.project()
	e.ok(proj, "enable", "tdd", "--copy", "-a", "universal")

	r := e.ok(proj, "check")
	contains(t, r.out, "Checked 2 skills: 2 up to date.")

	writeSkill(t, filepath.Join(src, "tdd"), "tdd", "new body")
	commitAll(t, src)

	r = e.ok(proj, "check")
	contains(t, r.out, "! tdd: update available ("+src+")", "1 update available, 1 up to date", "Install updates with: myskills update")

	r = e.ok(proj, "check", "--json", "tdd")
	checks := decode[checkJSON](t, r.out).Checks
	if len(checks) != 1 || checks[0].Skill != "tdd" || checks[0].State != "available" || checks[0].Commit == "" {
		t.Errorf("check JSON = %s", r.out)
	}

	r = e.ok(proj, "update")
	contains(t, r.out, "✓ updated tdd (", "✓ refreshed tdd → .agents/skills/tdd (Universal, Antigravity)")
	body, err := os.ReadFile(filepath.Join(proj, ".agents/skills/tdd/SKILL.md"))
	if err != nil || !strings.Contains(string(body), "new body") {
		t.Errorf("copy not refreshed: %s %v", body, err)
	}

	r = e.ok(proj, "update")
	contains(t, r.out, "✓ Everything is up to date.")

	// A locally modified library skill is skipped: exit 1.
	writeFile(t, filepath.Join(e.libDir("lint"), "notes.md"), "mine")
	writeSkill(t, filepath.Join(src, "lint"), "lint", "new body")
	commitAll(t, src)
	r = e.fails(proj, "update", "lint")
	contains(t, r.out, "! lint has local changes", "Use --force to overwrite the local changes.")
	r = e.ok(proj, "update", "lint", "--force")
	contains(t, r.out, "✓ updated lint")
}

func TestUpdateSkipsModifiedCopy(t *testing.T) {
	e := newEnv(t)
	src, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	proj := e.project()
	e.ok(proj, "enable", "tdd", "--copy", "-a", "universal")
	writeFile(t, filepath.Join(proj, ".agents/skills/tdd/notes.md"), "mine")
	writeSkill(t, filepath.Join(src, "tdd"), "tdd", "new body")
	commitAll(t, src)

	r := e.fails(proj, "update")
	contains(t, r.out, "✓ updated tdd", "! .agents/skills/tdd: modified locally, so not refreshed", "Use --force")
	if strings.Contains(r.out, "already copied") {
		t.Errorf("skipped copy reported as fine:\n%s", r.out)
	}
	r = e.ok(proj, "update", "--force")
	contains(t, r.out, "✓ refreshed tdd → .agents/skills/tdd (Universal, Antigravity)")
}

func TestAgents(t *testing.T) {
	e := newEnv(t)
	r := e.ok(e.home, "agents")
	contains(t, r.out, "ID", "PROJECT DIR", "✓  universal    Universal    .agents/skills", "~/.claude/skills", "antigravity")

	piDir := filepath.Join(e.home, ".pi", "skills")
	r = e.ok(e.home, "agents", "add", "pi", "--name", "Pi", "--global", piDir)
	contains(t, r.out, "✓ added agent pi")
	r = e.fails(e.home, "agents", "add", "pi", "--global", piDir)
	contains(t, r.err, `agent "pi" already exists`)

	r = e.ok(e.home, "agents", "use", "universal,pi")
	contains(t, r.out, "✓ default agents: Universal, Pi")
	r = e.ok(e.home, "agents")
	contains(t, r.out, "✓  pi", "Pi", "—", "~/.pi/skills")

	_, url := e.repo(map[string]string{"tdd": "tdd"})
	e.ok(e.home, "add", url)
	r = e.ok(e.home, "enable", "-g", "tdd")
	contains(t, r.out, "✓ linked tdd → ~/.pi/skills/tdd (Pi)")

	r = e.ok(e.home, "agents", "remove", "pi")
	contains(t, r.out, "✓ unlinked ~/.pi/skills/tdd", "✓ removed agent pi")
	r = e.fails(e.home, "agents", "remove", "universal")
	contains(t, r.err, "built-in agent")
	r = e.fails(e.home, "agents", "use", "nope")
	contains(t, r.err, `unknown agent "nope"`)
}

func TestFind(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.URL.Query().Get("q") != "pdf tools" || r.URL.Query().Get("limit") != "5" {
			http.Error(w, "bad request "+r.URL.String(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"skills":[
			{"id":"a","source":"anthropics/skills","skillId":"pdf","name":"PDF \u001b[31mtools","installs":12345},
			{"id":"b","source":"other/repo","skillId":"pdf-lite","name":"pdf-lite","installs":7}]}`))
	}))
	defer srv.Close()
	e.skillsSH = srv.URL

	r := e.ok(e.home, "find", "pdf", "tools", "-n", "5")
	contains(t, r.out, "anthropics/skills@pdf  12345  PDF tools\nother/repo@pdf-lite    7      pdf-lite\n")

	r = e.ok(e.home, "find", "pdf tools", "--limit", "5", "--json")
	got := decode[findJSON](t, r.out).Results
	if len(got) != 2 || got[0].Source != "anthropics/skills" || got[0].SkillID != "pdf" || got[0].Installs != 12345 || got[0].InLibrary != "" {
		t.Errorf("find JSON = %s", r.out)
	}

	r = e.fails(e.home, "find")
	contains(t, r.err, "a search query is required")

	r = e.fails(e.home, "find", "other")
	contains(t, r.err, "✗ skills.sh search: 400")
}

func TestCompletion(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"a", "b"} {
		writeSkill(t, e.path(c, "review"), "review", "body")
		e.ok(e.home, "add", e.path(c), "--link")
	}
	writeSkill(t, e.path("tdd"), "tdd", "body")
	e.ok(e.home, "add", e.path("tdd"))

	r := e.ok(e.home, "__complete", "enable", "")
	contains(t, r.out, "a/review\t", "b/review\t", "tdd\tThe tdd skill.")
	if strings.Contains(r.out, "\nreview\t") {
		t.Errorf("ambiguous name completed:\n%s", r.out)
	}
	r = e.ok(e.home, "__complete", "enable", "tdd", "")
	if strings.Contains(r.out, "tdd\t") {
		t.Errorf("completed a skill twice:\n%s", r.out)
	}
	r = e.ok(e.home, "__complete", "enable", "-a", "")
	contains(t, r.out, "universal\tUniversal", "claude-code\tClaude Code")
	r = e.ok(e.home, "completion", "zsh")
	contains(t, r.out, "#compdef myskills")
}
