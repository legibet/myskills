package cli

import (
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/tui"
)

// openTUI opens the full-screen app on view in the current project, or in
// the global scope outside one. query is the initial Find query.
func (a *app) openTUI(view tui.View, query string) error {
	project, err := a.project(".")
	if err != nil {
		return err
	}
	scope := core.Global
	if project != nil {
		scope = *project
	}
	return a.runTUI(tui.Options{View: view, Query: query, Scope: scope, Project: project})
}

// skillsTUI opens the full-screen app on the Skills view in scope; project
// is the current project, nil when there is none.
func (a *app) skillsTUI(scope core.Scope, project *core.Scope) error {
	return a.runTUI(tui.Options{View: tui.Skills, Scope: scope, Project: project})
}

// runTUI runs the app and prints the summary of its changes once the
// alternate screen is closed.
func (a *app) runTUI(opts tui.Options) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	summary, err := tui.Run(svc, opts)
	for _, line := range summary {
		a.println(line)
	}
	return err
}

// pick lets the user choose among the candidates of f; nil when cancelled.
func (a *app) pick(f *core.Fetched) ([]core.Candidate, error) {
	return tui.Pick(f)
}
