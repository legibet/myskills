package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

func TestMain(m *testing.M) {
	readSkill = func(dir string) (string, error) {
		name := filepath.Base(dir)
		return "---\nname: " + name + "\ndescription: About " + name + ".\n---\n# Body of " + dir + "\n\nSome **markdown**.", nil
	}
	tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return timerMsg{d: d, fn: fn} }
	}
	openURL = func(url string) tea.Cmd {
		return func() tea.Msg { return openedMsg{} }
	}
	os.Exit(m.Run())
}

// timerMsg is a pending timer of the fake clock.
type timerMsg struct {
	d  time.Duration
	fn func(time.Time) tea.Msg
}

// fakeService is an in-memory service that records calls.
type fakeService struct {
	mu    sync.Mutex
	calls []string

	lib    []core.Skill
	status map[core.Scope]core.Status
	usage  map[string][]core.Scope
	config store.Config
	agents []agent.Agent
	dirs   map[core.Scope][]core.AgentDir

	errs     map[string][]error // queued errors per method
	results  map[string][]skillssh.Result
	markdown map[string]string
	fetched  map[string]*core.Fetched // by spec input
	checks   []core.UpdateCheck
}

func newFake() *fakeService {
	return &fakeService{
		status: map[core.Scope]core.Status{},
		dirs:   map[core.Scope][]core.AgentDir{},
		usage:  map[string][]core.Scope{},
		agents: slices.Clone(agent.Builtins),
		errs:   map[string][]error{},
		results: map[string][]skillssh.Result{
			"review": {
				{ID: "1", Source: "acme/skills", SkillID: "review", Name: "review", Installs: 12345},
				{ID: "2", Source: "other/repo", SkillID: "code-review", Name: "Code Review", Installs: 999},
			},
		},
		markdown: map[string]string{},
		fetched:  map[string]*core.Fetched{},
	}
}

func (f *fakeService) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := fmt.Sprintf(format, args...)
	f.calls = append(f.calls, call)
	method, _, _ := strings.Cut(call, " ")
	if q := f.errs[method]; len(q) > 0 {
		f.errs[method] = q[1:]
		return q[0]
	}
	return nil
}

func (f *fakeService) failNext(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[method] = append(f.errs[method], err)
}

func (f *fakeService) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeService) Library() ([]core.Skill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.lib), nil
}

func (f *fakeService) Status(scope core.Scope) (core.Status, error) {
	return f.status[scope], nil
}

func (f *fakeService) Usage() (map[string][]core.Scope, error) { return f.usage, nil }
func (f *fakeService) Config() (store.Config, error)           { return f.config, nil }
func (f *fakeService) Agents() ([]agent.Agent, error)          { return f.agents, nil }

func (f *fakeService) AgentDirs(scope core.Scope) ([]core.AgentDir, error) {
	return f.dirs[scope], nil
}

func (f *fakeService) SetDefaultAgents(ids []string) error {
	return f.record("SetDefaultAgents %v", ids)
}

func (f *fakeService) Enable(scope core.Scope, sk core.Skill, opts core.EnableOptions) (core.Result, error) {
	if err := f.record("Enable %s %s mode=%s agents=%v force=%v", scope.String(), sk.Path, opts.Mode, opts.Agents, opts.Force); err != nil {
		return core.Result{}, err
	}
	agents := opts.Agents
	if agents == nil {
		agents = []string{"universal", "claude-code"}
	}
	mode := cmp.Or(opts.Mode, core.Link)
	return core.Result{Skill: sk.Path, Changes: []core.Change{{Path: "/p/" + sk.Name, Agents: agents, Action: core.Linked, Mode: mode}}}, nil
}

func (f *fakeService) Disable(scope core.Scope, ref string, opts core.DisableOptions) (core.Result, error) {
	if err := f.record("Disable %s %s agents=%v force=%v", scope.String(), ref, opts.Agents, opts.Force); err != nil {
		return core.Result{}, err
	}
	agents := opts.Agents
	if agents == nil {
		agents = []string{"universal", "claude-code"}
	}
	return core.Result{Skill: ref, Changes: []core.Change{{Path: "/p/" + ref, Agents: agents, Action: core.Removed}}}, nil
}

func (f *fakeService) RefreshCopies(scope core.Scope, paths []string, force bool) ([]core.Result, error) {
	return nil, f.record("RefreshCopies %s %v force=%v", scope.String(), paths, force)
}

func (f *fakeService) Remove(sk core.Skill) (core.RemoveResult, error) {
	if err := f.record("Remove %s", sk.Path); err != nil {
		return core.RemoveResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lib = slices.DeleteFunc(f.lib, func(s core.Skill) bool { return s.Path == sk.Path })
	return core.RemoveResult{Entry: sk.Entry, Skills: []string{sk.Path}}, nil
}

func (f *fakeService) CheckUpdates(_ context.Context, skills []core.Skill, progress func(done, total int)) (*core.Updates, error) {
	var paths []string
	for _, sk := range skills {
		paths = append(paths, sk.Path)
	}
	if err := f.record("CheckUpdates %v", paths); err != nil {
		return nil, err
	}
	if progress != nil {
		progress(1, 1)
	}
	// Like core, a check is about the record it was given: one made for an
	// older commit than the skill's does not apply.
	var checks []core.UpdateCheck
	for _, sk := range skills {
		for _, c := range f.checks {
			if c.Skill.Path == sk.Path && commit(c.Skill) == commit(sk) {
				c.Skill = sk
				checks = append(checks, c)
			}
		}
	}
	return &core.Updates{Checks: checks}, nil
}

// ApplyUpdates moves the library record to a new commit, and fails like core
// when the library changed since the check.
func (f *fakeService) ApplyUpdates(u *core.Updates, paths []string, force bool) []core.UpdateResult {
	err := f.record("ApplyUpdates %v force=%v", paths, force)
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []core.UpdateResult
	for _, p := range paths {
		r := core.UpdateResult{Skill: p, Err: err}
		i := slices.IndexFunc(f.lib, func(sk core.Skill) bool { return sk.Path == p })
		for _, c := range u.Checks {
			if c.Skill.Path == p && i >= 0 && commit(c.Skill) != commit(f.lib[i]) {
				r.Err = fmt.Errorf("%s changed since it was checked", p)
			}
		}
		if r.Err == nil && i >= 0 && f.lib[i].Record != nil {
			rec := *f.lib[i].Record
			rec.Commit += "+1"
			f.lib[i].Record, f.lib[i].Modified, r.Updated = &rec, false, true
		}
		out = append(out, r)
	}
	return out
}

// UpdateSkill does what core's does, through the fake's CheckUpdates and
// ApplyUpdates. A skill the fake has no check for gives no state.
func (f *fakeService) UpdateSkill(ctx context.Context, sk core.Skill, force bool) (core.UpdateState, error) {
	u, err := f.CheckUpdates(ctx, []core.Skill{sk}, nil)
	if err != nil {
		return "", err
	}
	if len(u.Checks) == 0 {
		return "", nil
	}
	switch c := u.Checks[0]; c.State {
	case core.Available:
		return c.State, f.ApplyUpdates(u, []string{sk.Path}, force)[0].Err
	case core.CheckFailed:
		return c.State, c.Err
	default:
		return c.State, nil
	}
}

func commit(sk core.Skill) string {
	if sk.Record == nil {
		return ""
	}
	return sk.Record.Commit
}

func (f *fakeService) Fetch(_ context.Context, spec source.Spec) (*core.Fetched, error) {
	name := strings.Join(spec.Skills, ",")
	if err := f.record("Fetch %s skill=%s", spec.URL, name); err != nil {
		return nil, err
	}
	if fe, ok := f.fetched[spec.Input]; ok {
		return fe, nil
	}
	return &core.Fetched{Spec: spec, Candidates: []core.Candidate{{Name: name, Dir: "/tmp/checkout/" + name}}}, nil
}

func (f *fakeService) Install(_ context.Context, fe *core.Fetched, picks []core.Candidate) []core.InstallResult {
	var names []string
	for _, c := range picks {
		names = append(names, c.Name)
	}
	err := f.record("Install %v", names)
	f.mu.Lock()
	defer f.mu.Unlock()
	// Like core: a skill lands at its name; the same source is Already there,
	// another source's skill under that name is a conflict.
	var out []core.InstallResult
	for _, c := range picks {
		r := core.InstallResult{Candidate: c, Err: err}
		i := slices.IndexFunc(f.lib, func(sk core.Skill) bool { return sk.Path == c.Name })
		switch {
		case err != nil:
		case i >= 0 && f.lib[i].Record != nil && f.lib[i].Record.Source.URL == fe.Spec.URL:
			r.Path, r.Already = c.Name, true
		case i >= 0:
			r.Err = fmt.Errorf("%w: the library already has a skill %q from another source", core.ErrConflict, c.Name)
		default:
			f.lib = append(f.lib, core.Skill{Path: c.Name, Name: c.Name, Dir: "/lib/" + c.Name, Entry: c.Name, Record: gitRecord(fe.Spec.URL)})
			r.Path = c.Name
		}
		out = append(out, r)
	}
	return out
}

func (f *fakeService) Search(_ context.Context, query string, _ int) ([]skillssh.Result, error) {
	if err := f.record("Search %s", query); err != nil {
		return nil, err
	}
	return f.results[query], nil
}

func (f *fakeService) SkillMarkdown(_ context.Context, src, skillID string) (string, error) {
	if err := f.record("SkillMarkdown %s/%s", src, skillID); err != nil {
		return "", err
	}
	return cmp.Or(f.markdown[src+"/"+skillID], "---\nname: "+skillID+"\n---\n# "+skillID+" remote body"), nil
}

// harness drives an app synchronously: commands run in place, their messages
// are fed back, timers wait for fire.
type harness struct {
	t      *testing.T
	a      *app
	svc    *fakeService
	timers []timerMsg
	quit   bool
}

func newHarness(t *testing.T, svc *fakeService, opts Options, w, h int) *harness {
	t.Helper()
	hs := &harness{t: t, a: newApp(context.Background(), svc, opts), svc: svc}
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	hs.exec(hs.a.Init())
	return hs
}

// send delivers msg and runs the resulting commands.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.a.Update(msg)
	h.exec(cmd)
}

// press sends key presses, e.g. "space", "ctrl+r", "A", "enter".
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyPress(k))
	}
}

// typeText types into the focused input.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// fire delivers the pending timers.
func (h *harness) fire() {
	h.t.Helper()
	timers := h.timers
	h.timers = nil
	for _, tm := range timers {
		h.send(tm.fn(time.Now()))
	}
}

func (h *harness) exec(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil, spinner.TickMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case timerMsg:
			if _, ok := msg.fn(time.Now()).(statusClearMsg); !ok {
				h.timers = append(h.timers, msg)
			}
		case tea.QuitMsg:
			h.quit = true
		default:
			_, next := h.a.Update(msg)
			queue = append(queue, next)
		}
	}
}

// view returns the screen without styles.
func (h *harness) view() string {
	return ansi.Strip(h.a.View().Content)
}

func (h *harness) contains(fragments ...string) {
	h.t.Helper()
	v := h.view()
	for _, f := range fragments {
		if !strings.Contains(v, f) {
			h.t.Fatalf("view does not contain %q:\n%s", f, v)
		}
	}
}

func (h *harness) lacks(fragments ...string) {
	h.t.Helper()
	v := h.view()
	for _, f := range fragments {
		if strings.Contains(v, f) {
			h.t.Fatalf("view contains %q:\n%s", f, v)
		}
	}
}

func keyPress(k string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp,
		"down": tea.KeyDown, "backspace": tea.KeyBackspace, "space": tea.KeySpace,
		"left": tea.KeyLeft, "right": tea.KeyRight, "pgdown": tea.KeyPgDown,
	}
	switch {
	case k == "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case strings.HasPrefix(k, "ctrl+"):
		return tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
	case special[k] != 0:
		msg := tea.KeyPressMsg{Code: special[k]}
		if k == "space" {
			msg.Text = " "
		}
		return msg
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

// Fixtures.

var project = core.Scope{Root: "/work/proj"}

func gitRecord(url string) *store.SkillRecord {
	return &store.SkillRecord{Source: store.Source{Type: "git", URL: url}}
}

// libraryFixture is a small library with every row state, in project scope:
// tdd linked, review off (in a collection), notes a copy with changes,
// broken invalid, plus a broken link and an unmanaged entry without a skill.
func libraryFixture() *fakeService {
	f := newFake()
	f.lib = []core.Skill{
		{Path: "broken", Name: "broken", Dir: "/lib/broken", Entry: "broken", Issue: "SKILL.md: missing description"},
		{Path: "notes", Name: "notes", Description: "Keeps notes.", Dir: "/lib/notes", Entry: "notes", Record: gitRecord("https://github.com/acme/notes.git")},
		{Path: "agent-skills/review", Name: "review", Description: "Reviews code.", Dir: "/lib/agent-skills/review", Entry: "agent-skills", Linked: true, LinkTarget: "/home/agent-skills"},
		{Path: "tdd", Name: "tdd", Description: "Test-driven development.", Dir: "/lib/tdd", Entry: "tdd", Record: gitRecord("https://github.com/acme/tdd.git")},
	}
	f.status[project] = core.Status{
		Scope: project,
		Enablements: []core.Enablement{
			{Skill: "notes", Name: "notes", Mode: core.Copy, Placements: []core.Placement{
				{Path: "/work/proj/.claude/skills/notes", Name: "notes", Agents: []string{"claude-code"}, Kind: core.KindCopy, Skill: "notes", Modified: true},
			}, Missing: []string{"universal"}},
			{Skill: "tdd", Name: "tdd", Mode: core.Link, Placements: []core.Placement{
				{Path: "/work/proj/.agents/skills/tdd", Name: "tdd", Agents: []string{"universal", "antigravity"}, Kind: core.KindLink, Skill: "tdd"},
				{Path: "/work/proj/.claude/skills/tdd", Name: "tdd", Agents: []string{"claude-code"}, Kind: core.KindLink, Skill: "tdd"},
			}},
		},
		Broken: []core.Placement{{Path: "/work/proj/.claude/skills/gone", Name: "gone", Agents: []string{"claude-code"}, Kind: core.KindBroken, Skill: "old/gone"}},
		Others: []core.Placement{{Path: "/work/proj/.claude/skills/mine", Name: "mine", Agents: []string{"claude-code"}, Kind: core.KindOther}},
	}
	f.status[core.Global] = core.Status{Enablements: []core.Enablement{
		{Skill: "agent-skills/review", Name: "review", Mode: core.Link, Placements: []core.Placement{
			{Path: "/home/.agents/skills/review", Name: "review", Agents: []string{"universal"}, Kind: core.KindLink, Skill: "agent-skills/review"},
		}},
	}}
	f.usage = map[string][]core.Scope{"tdd": {project}, "agent-skills/review": {core.Global}}
	f.dirs[project] = []core.AgentDir{
		{Path: "/work/proj/.agents/skills", Agents: []string{"universal", "antigravity"}},
		{Path: "/work/proj/.claude/skills", Agents: []string{"claude-code"}},
	}
	f.dirs[core.Global] = []core.AgentDir{
		{Path: "/home/.agents/skills", Agents: []string{"universal"}},
		{Path: "/home/.claude/skills", Agents: []string{"claude-code"}},
		{Path: "/home/.gemini/config/skills", Agents: []string{"antigravity"}},
	}
	return f
}

func projectOptions() Options {
	p := project
	return Options{View: Skills, Scope: project, Project: &p}
}

// cursorTo moves the Skills cursor to the row with the given name.
func (h *harness) cursorTo(name string) {
	h.t.Helper()
	for i, r := range h.a.sk.rows {
		if r.name() == name {
			h.a.sk.list.cursor = i
			h.send(nil)
			return
		}
	}
	h.t.Fatalf("no row %q", name)
}

func windowSize(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func stripView(s string) string { return ansi.Strip(s) }
