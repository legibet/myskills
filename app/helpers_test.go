package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/source"
)

// harness is the app over an isolated home, drawn headless. Work the app
// posts to the main thread is queued, and settle runs it.
type harness struct {
	t    *testing.T
	tmp  string // resolved temp dir holding home, repositories and projects
	home string
	svc  *core.Service
	a    *app
	tt   *ui.Tester

	mu    sync.Mutex
	queue []func()

	folder   string // what choosing a folder returns
	revealed []string
	opened   []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, "home")
	mkdirAll(t, home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MYSKILLS_HOME", filepath.Join(home, ".myskills"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	svc, err := core.Open()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, tmp: tmp, home: home, svc: svc}
	return h
}

// start opens the window, once the fixtures are in place.
func (h *harness) start() *harness {
	h.t.Helper()
	desk := desktop{
		chooseFolder: func(string) string { return h.folder },
		reveal:       func(path string) { h.revealed = append(h.revealed, path) },
		open:         func(path string) { h.opened = append(h.opened, path) },
	}
	h.a = newApp(h.svc, desk, h.post)
	h.a.reload()
	h.t.Cleanup(h.a.close)
	h.tt = ui.NewTester(h.a.view, 1200, 800)
	h.settle()
	return h
}

func (h *harness) post(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queue = append(h.queue, fn)
}

// settle runs what the app posts and draws frames until the work in flight
// is done and nothing is posted for a while.
func (h *harness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	quiet := 0
	for quiet < 3 {
		if time.Now().After(deadline) {
			h.t.Fatalf("the app is still working: loading %v, busy %v, checking %v, frame pending %v", h.a.loading, h.a.busy, h.a.updates.checking, h.tt.FramePending())
		}
		h.mu.Lock()
		queue := h.queue
		h.queue = nil
		h.mu.Unlock()
		for _, fn := range queue {
			fn()
		}
		h.tt.Frame()
		h.tt.Advance(50 * time.Millisecond) // the tester's clock: animations and timers move with it
		if len(queue) == 0 && !h.a.loading && len(h.a.busy) == 0 && !h.a.updates.checking && h.a.find.due.IsZero() && !h.tt.FramePending() {
			quiet++
		} else {
			quiet = 0
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// click clicks the element showing s, then settles.
func (h *harness) click(s string) {
	h.t.Helper()
	if err := h.tt.Click(s); err != nil {
		h.t.Fatalf("click %q: %v\nshown: %q", s, err, h.tt.Texts())
	}
	h.settle()
}

// choose clicks the element showing s, which opens a menu, and chooses an
// item of it.
func (h *harness) choose(s, item string) {
	h.t.Helper()
	h.click(s)
	if err := h.tt.ChooseMenuItem(item); err != nil {
		h.t.Fatalf("choose %q: %v\nmenu: %q", item, err, h.tt.Menu())
	}
	h.settle()
}

// typeText types into the focused field, then settles.
func (h *harness) typeText(s string) {
	h.t.Helper()
	h.tt.Type(s)
	h.settle()
}

// want fails unless every text is shown.
func (h *harness) want(texts ...string) {
	h.t.Helper()
	for _, s := range texts {
		if !h.tt.HasText(s) {
			h.t.Errorf("%q not shown; shown: %q", s, h.tt.Texts())
		}
	}
}

// wantNo fails when one of the texts is shown.
func (h *harness) wantNo(texts ...string) {
	h.t.Helper()
	for _, s := range texts {
		if h.tt.HasText(s) {
			h.t.Errorf("%q shown", s)
		}
	}
}

// alert returns the title of the alert shown, "" when none is.
func (h *harness) alert() string {
	if len(h.a.alerts) == 0 {
		return ""
	}
	return h.a.alerts[0].title
}

// confirm presses Enter on the alert titled title, which clicks its last
// button: the action, or OK.
func (h *harness) confirm(title string) {
	h.t.Helper()
	h.wantAlert(title)
	h.tt.Key(0, ui.KeyEnter)
	h.settle()
}

// dismiss presses Escape on the alert titled title, choosing no button.
func (h *harness) dismiss(title string) {
	h.t.Helper()
	h.wantAlert(title)
	h.tt.Key(0, ui.KeyEscape)
	h.settle()
}

func (h *harness) wantAlert(title string) {
	h.t.Helper()
	if got := h.alert(); got != title {
		msg := ""
		if got != "" {
			msg = h.a.alerts[0].message
		}
		h.t.Fatalf("alert = %q (%s), want %q", got, msg, title)
	}
}

func (h *harness) path(parts ...string) string {
	return filepath.Join(append([]string{h.tmp}, parts...)...)
}

// repo creates a git repository at tmp/name with skills (name → directory
// in the repository) committed, and returns its file:// URL.
func (h *harness) repo(name string, skills map[string]string) string {
	h.t.Helper()
	dir := h.path(name)
	gitInit(h.t, dir)
	for skillName, rel := range skills {
		writeSkill(h.t, filepath.Join(dir, filepath.FromSlash(rel)), skillName, "Use "+skillName+".", "# "+skillName+"\n\nThe body of "+skillName+".")
	}
	commitAll(h.t, dir)
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
}

// add adds every skill of a source to the library.
func (h *harness) add(spec string) {
	h.t.Helper()
	sp, err := source.Parse(spec)
	if err != nil {
		h.t.Fatal(err)
	}
	f, err := h.svc.Fetch(context.Background(), sp)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, r := range h.svc.Install(f, f.Candidates) {
		if r.Err != nil {
			h.t.Fatalf("install %s: %v", r.Candidate.Name, r.Err)
		}
	}
}

// skill returns the library skill named ref.
func (h *harness) skill(ref string) core.Skill {
	h.t.Helper()
	sk, err := h.svc.Resolve(ref)
	if err != nil {
		h.t.Fatal(err)
	}
	return sk
}

// project creates a git repository at tmp/name and returns its scope.
func (h *harness) project(name string) core.Scope {
	h.t.Helper()
	dir := h.path(name)
	gitInit(h.t, dir)
	writeFile(h.t, filepath.Join(dir, "README.md"), "project\n")
	commitAll(h.t, dir)
	sc, err := h.svc.ProjectScope(dir)
	if err != nil {
		h.t.Fatal(err)
	}
	return sc
}

func (h *harness) enable(sc core.Scope, ref string, opts core.EnableOptions) {
	h.t.Helper()
	if _, err := h.svc.Enable(sc, h.skill(ref), opts); err != nil {
		h.t.Fatalf("enable %s: %v", ref, err)
	}
}

func (h *harness) status(sc core.Scope) core.Status {
	h.t.Helper()
	st, err := h.svc.Status(sc)
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

// enabled returns the names of the skills enabled in a scope.
func (h *harness) enabled(sc core.Scope) []string {
	h.t.Helper()
	var names []string
	for _, e := range h.status(sc).Enablements {
		names = append(names, e.Name)
	}
	slices.Sort(names)
	return names
}

func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: "+description+"\n---\n"+body+"\n")
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	mkdirAll(t, dir)
	runGit(t, dir, "init", "-q", "-b", "main")
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "commit")
}
