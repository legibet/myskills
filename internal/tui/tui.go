// Package tui is the full-screen terminal interface of myskills (design §9):
// the Skills and Find views, and the picker used by `myskills add`.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

// View selects the view the app opens in.
type View int

const (
	Skills View = iota // the library with enablement state
	Find               // skills.sh search and source browsing
)

// Options configures Run.
type Options struct {
	View    View        // view to open
	Query   string      // initial Find query
	Scope   core.Scope  // initial active scope
	Project *core.Scope // the current project; nil when there is none
}

// Run runs the full-screen app until the user quits. It returns one line per
// change made during the session, for the caller to print after the alternate
// screen closes.
func Run(svc *core.Service, opts Options) ([]string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newApp(ctx, liveService{svc}, opts)
	_, err := tea.NewProgram(a).Run()
	cancel()
	a.shutdown()
	return a.summary, err
}

// Pick runs the add picker over the candidates of f and returns the ones the
// user chose, or nil when the picker was cancelled.
func Pick(f *core.Fetched) ([]core.Candidate, error) {
	p := newPicker(f)
	if _, err := tea.NewProgram(p).Run(); err != nil {
		return nil, err
	}
	return p.picked, nil
}

// service is the part of core.Service the TUI uses. Model tests replace it
// with a fake that records calls.
type service interface {
	Library() ([]core.Skill, error)
	Status(scope core.Scope) (core.Status, error)
	Usage() (map[string][]core.Scope, error)
	Config() (store.Config, error)
	Agents() ([]agent.Agent, error)
	AgentDirs(scope core.Scope) ([]core.AgentDir, error)
	SetDefaultAgents(ids []string) error
	Enable(scope core.Scope, sk core.Skill, opts core.EnableOptions) (core.Result, error)
	Disable(scope core.Scope, ref string, opts core.DisableOptions) (core.Result, error)
	RefreshCopies(scope core.Scope, paths []string, force bool) ([]core.Result, error)
	Remove(sk core.Skill) (core.RemoveResult, error)
	CheckUpdates(ctx context.Context, skills []core.Skill, progress func(done, total int)) (*core.Updates, error)
	ApplyUpdates(u *core.Updates, paths []string, force bool) []core.UpdateResult
	Fetch(ctx context.Context, spec source.Spec) (*core.Fetched, error)
	Install(f *core.Fetched, picks []core.Candidate) []core.InstallResult
	Search(ctx context.Context, query string, limit int) ([]skillssh.Result, error)
	SkillMarkdown(ctx context.Context, source, skillID string) (string, error)
}

// liveService adds the skills.sh client calls to core.Service.
type liveService struct{ *core.Service }

func (s liveService) Search(ctx context.Context, query string, limit int) ([]skillssh.Result, error) {
	return s.SkillsSH.Search(ctx, query, limit)
}

func (s liveService) SkillMarkdown(ctx context.Context, source, skillID string) (string, error) {
	return s.SkillsSH.SkillMarkdown(ctx, source, skillID)
}

// Indirections that model tests replace: reading SKILL.md, timers and the
// browser.
var (
	readSkill = readSkillFile
	tick      = tea.Tick
	openURL   = openBrowser
)
