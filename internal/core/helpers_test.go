package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

// env is an isolated home with a myskills service over it.
type env struct {
	t    *testing.T
	tmp  string // resolved temp dir holding home, sources and projects
	home string
	svc  *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp := realPath(t.TempDir())
	home := filepath.Join(tmp, "home")
	mkdirAll(t, home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MYSKILLS_HOME", filepath.Join(home, ".myskills"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	// Test sources serve partial clones, as GitHub does. Local transports
	// drop GIT_CONFIG_* variables, so this goes in the global config.
	writeFile(t, filepath.Join(home, ".gitconfig"), "[uploadpack]\n\tallowFilter = true\n")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	svc, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, tmp: tmp, home: home, svc: svc}
}

func (e *env) path(parts ...string) string {
	return filepath.Join(append([]string{e.tmp}, parts...)...)
}

// libDir returns the directory of a library path.
func (e *env) libDir(libPath string) string {
	return filepath.Join(e.svc.paths.Skills, filepath.FromSlash(libPath))
}

func (e *env) lock() *store.Lock {
	e.t.Helper()
	l, err := store.LoadLock(e.svc.paths.Lock)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

// repo creates a git repository at tmp/name with the given skills (name →
// directory inside the repo) committed, and returns its file:// URL.
func (e *env) repo(name string, skills map[string]string) (dir, url string) {
	e.t.Helper()
	dir = e.path(name)
	gitInit(e.t, dir)
	for skillName, rel := range skills {
		writeSkill(e.t, filepath.Join(dir, filepath.FromSlash(rel)), skillName, skillName+" body")
	}
	commitAll(e.t, dir, "init")
	return dir, fileURL(dir)
}

// add fetches spec and installs all its valid candidates; every install must
// succeed.
func (e *env) add(spec string) {
	e.t.Helper()
	f := e.fetch(spec)
	defer func() { _ = f.Close() }()
	var picks []Candidate
	for _, c := range f.Candidates {
		if c.Issue == "" {
			picks = append(picks, c)
		}
	}
	for _, r := range e.svc.Install(context.Background(), f, picks) {
		if r.Err != nil {
			e.t.Fatalf("install %s: %v", r.Candidate.Name, r.Err)
		}
	}
}

func (e *env) fetch(spec string) *Fetched {
	e.t.Helper()
	return e.fetchSkills(spec)
}

// fetchSkills fetches spec restricted to the named skills, as
// owner/repo@skill or -s does.
func (e *env) fetchSkills(spec string, names ...string) *Fetched {
	e.t.Helper()
	sp, err := source.Parse(spec)
	if err != nil {
		e.t.Fatal(err)
	}
	sp.Skills = names
	f, err := e.svc.Fetch(context.Background(), sp)
	if err != nil {
		e.t.Fatalf("fetch %s: %v", spec, err)
	}
	return f
}

// skill resolves a library skill.
func (e *env) skill(ref string) Skill {
	e.t.Helper()
	sk, err := e.svc.Resolve(ref)
	if err != nil {
		e.t.Fatal(err)
	}
	return sk
}

// project creates a project folder at tmp/name, a git repository with one
// commit when withGit, and returns its scope.
func (e *env) project(name string, withGit bool) Scope {
	e.t.Helper()
	dir := e.path(name)
	mkdirAll(e.t, dir)
	if withGit {
		gitInit(e.t, dir)
		writeFile(e.t, filepath.Join(dir, "README.md"), "project\n")
		commitAll(e.t, dir, "init")
	}
	sc, err := e.svc.ProjectScope(dir)
	if err != nil {
		e.t.Fatal(err)
	}
	return sc
}

func (e *env) enable(sc Scope, ref string, opts EnableOptions) Result {
	e.t.Helper()
	res, err := e.svc.Enable(sc, e.skill(ref), opts)
	if err != nil {
		e.t.Fatalf("enable %s: %v", ref, err)
	}
	return res
}

func (e *env) status(sc Scope) Status {
	e.t.Helper()
	st, err := e.svc.Status(sc)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

// managedBlock returns the myskills block of an exclude file, "" when absent.
func managedBlock(content string) string {
	begin := strings.Index(content, excludeBegin)
	if begin < 0 {
		return ""
	}
	end := strings.Index(content[begin:], excludeEnd)
	return content[begin : begin+end+len(excludeEnd)+1]
}

// exclude returns the content of the repository's info/exclude file.
func exclude(t *testing.T, root string) string {
	t.Helper()
	file := runGit(t, root, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: The %s skill.\n---\n\n%s\n", name, name, body))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func readlink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// leftovers lists the hidden staging entries myskills leaves while working.
func leftovers(t *testing.T, dirs ...string) []string {
	t.Helper()
	var found []string
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".incoming-") || strings.HasPrefix(e.Name(), ".old-") || strings.HasPrefix(e.Name(), ".myskills-") {
				found = append(found, filepath.Join(dir, e.Name()))
			}
		}
	}
	return found
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	mkdirAll(t, dir)
	runGit(t, dir, "init", "-q", "-b", "main")
}

// commitAll commits everything in the repository and returns the commit.
func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	return runGit(t, dir, "rev-parse", "HEAD")
}

func fileURL(dir string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
}

func changePaths(changes []Change) []string {
	var paths []string
	for _, c := range changes {
		paths = append(paths, c.Path)
	}
	return paths
}

func wantErr(t *testing.T, err, kind error, contains ...string) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("error = %v, want %v", err, kind)
	}
	for _, s := range contains {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}
}
