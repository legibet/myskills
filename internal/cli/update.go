package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
)

// checkJSON is the output of `check --json`.
type checkJSON struct {
	Checks []checkResultJSON `json:"checks"`
}

// checkResultJSON is the update check of one skill.
type checkResultJSON struct {
	Skill    string `json:"skill"` // library path
	Name     string `json:"name"`
	Source   string `json:"source"` // source label, e.g. "owner/repo"
	State    string `json:"state"`  // "up-to-date", "available", "removed-upstream", "not-updatable" or "failed"
	Commit   string `json:"commit,omitempty"`
	Modified bool   `json:"modified"` // changed locally: updating needs --force
	Error    string `json:"error,omitempty"`
}

func (a *app) checkCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check [skill…]",
		Short: "Check library skills for upstream updates",
		Long: `Check library skills (all of them, or the given ones) for changes in the
repository or local folder they were added from. Nothing is changed; use
'myskills update' to install updates.`,
		Example:           `  myskills check --json`,
		ValidArgsFunction: a.completeSkills,
		RunE: func(cmd *cobra.Command, refs []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			skills, err := a.selectSkills(svc, refs, false)
			if err != nil {
				return err
			}
			u, err := a.checkUpdates(cmd.Context(), svc, skills)
			if err != nil {
				return err
			}
			defer func() { _ = u.Close() }()

			if asJSON {
				out := checkJSON{Checks: []checkResultJSON{}}
				for _, c := range u.Checks {
					r := checkResultJSON{
						Skill:    c.Skill.Path,
						Name:     c.Skill.Name,
						Source:   c.Skill.SourceLabel(),
						State:    string(c.State),
						Commit:   c.Commit,
						Modified: c.Skill.Modified,
					}
					if c.Err != nil {
						r.Error = c.Err.Error()
						a.failed = true
					}
					out.Checks = append(out.Checks, r)
				}
				return a.printJSON(out)
			}

			count := map[core.UpdateState]int{}
			for _, c := range u.Checks {
				count[c.State]++
				a.printCheck(c)
			}
			if len(u.Checks) == 0 {
				a.note("The library is empty.")
				return nil
			}
			var parts []string
			for _, s := range []struct {
				state core.UpdateState
				label string
			}{
				{core.Available, "update available"},
				{core.UpToDate, "up to date"},
				{core.RemovedUpstream, "removed upstream"},
				{core.NotUpdatable, "not updatable"},
				{core.CheckFailed, "failed"},
			} {
				if n := count[s.state]; n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", n, s.label))
				}
			}
			a.note(fmt.Sprintf("Checked %s: %s.", plural(len(u.Checks), "skill"), strings.Join(parts, ", ")))
			if count[core.Available] > 0 {
				a.note("Install updates with: myskills update")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// printCheck prints a line for a check that needs attention.
func (a *app) printCheck(c core.UpdateCheck) {
	src := dim(" (" + c.Skill.SourceLabel() + ")")
	switch c.State {
	case core.Available:
		msg := c.Skill.Path + ": update available"
		if c.Skill.Modified {
			msg += "; modified locally, so updating needs --force"
		}
		a.warn(msg + src)
	case core.RemovedUpstream:
		a.warn(c.Skill.Path + ": removed upstream" + src)
	case core.CheckFailed:
		a.fail(fmt.Sprintf("%s: %v", c.Skill.Path, c.Err))
	}
}

func (a *app) updateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "update [skill…]",
		Short: "Update library skills and refresh their copies",
		Long: `Install upstream changes into the library (all updatable skills, or the given
ones), then refresh outdated copies of those skills in the current project and
globally. Links need no refresh: they always point at the library.

Skills and copies that were modified locally are skipped unless --force is
given; the exit code is 1 when anything was skipped or failed.`,
		Example: `  myskills update
  myskills update tdd --force`,
		ValidArgsFunction: a.completeSkills,
		RunE: func(cmd *cobra.Command, refs []string) error {
			return a.update(cmd.Context(), refs, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite local modifications of library skills and copies")
	return cmd
}

func (a *app) update(ctx context.Context, refs []string, force bool) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	skills, err := a.selectSkills(svc, refs, true)
	if err != nil {
		return err
	}
	project, err := a.project(".")
	if err != nil {
		return err
	}
	u, err := a.checkUpdates(ctx, svc, skills)
	if err != nil {
		return err
	}
	defer func() { _ = u.Close() }()

	// One skill at a time, so that an interrupt stops between them.
	quiet, skipped := true, false
	for _, c := range u.Checks {
		switch c.State {
		case core.CheckFailed, core.RemovedUpstream:
			quiet = false
			a.printCheck(c)
		case core.NotUpdatable:
			quiet = false
			a.note(c.Skill.Path + " has no source to update from")
		case core.Available:
			if err := ctx.Err(); err != nil {
				return err
			}
			quiet = false
			for _, r := range svc.ApplyUpdates(u, []string{c.Skill.Path}, force) {
				switch {
				case errors.Is(r.Err, core.ErrModified):
					skipped = true
					a.warn(r.Err.Error())
				case r.Err != nil:
					a.fail(r.Err.Error())
				default:
					a.ok("updated " + r.Skill + dim(shortCommit(c.Commit)))
				}
			}
		}
	}

	// Copies of the selected skills; all copies when none were named.
	var paths []string
	if len(refs) > 0 {
		for _, sk := range skills {
			paths = append(paths, sk.Path)
		}
	}
	scopes := []core.Scope{core.Global}
	if project != nil {
		scopes = []core.Scope{*project, core.Global}
	}
	for _, sc := range scopes {
		if err := ctx.Err(); err != nil {
			return err
		}
		results, err := svc.RefreshCopies(sc, paths, force)
		for _, res := range results {
			quiet = false
			a.printResult(sc, res)
		}
		for _, e := range leafErrors(err) {
			if errors.Is(e, core.ErrModified) {
				skipped = true // printed above as the copy's warning
			} else {
				a.fail(e.Error())
			}
		}
	}
	switch {
	case skipped:
		a.failed = true
		a.note("Use --force to overwrite the local changes.")
	case quiet && !a.failed:
		a.ok("Everything is up to date.")
	}
	return nil
}

// leafErrors flattens the errors.Join trees that core returns for
// operations on several items.
func leafErrors(err error) []error {
	if err == nil {
		return nil
	}
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}
	var out []error
	for _, e := range j.Unwrap() {
		out = append(out, leafErrors(e)...)
	}
	return out
}

// selectSkills resolves refs, each skill once, or returns the whole library
// (only updatable skills when updatable is set).
func (a *app) selectSkills(svc *core.Service, refs []string, updatable bool) ([]core.Skill, error) {
	if len(refs) == 0 {
		lib, err := svc.Library()
		if err != nil || !updatable {
			return lib, err
		}
		var skills []core.Skill
		for _, sk := range lib {
			if sk.Updatable() {
				skills = append(skills, sk)
			}
		}
		return skills, nil
	}
	var skills []core.Skill
	for _, ref := range refs {
		sk, err := svc.Resolve(ref)
		if err != nil {
			return nil, a.refError(ref, err)
		}
		if !slices.ContainsFunc(skills, func(s core.Skill) bool { return s.Path == sk.Path }) {
			skills = append(skills, sk)
		}
	}
	return skills, nil
}

// checkUpdates runs svc.CheckUpdates with a progress line.
func (a *app) checkUpdates(ctx context.Context, svc *core.Service, skills []core.Skill) (*core.Updates, error) {
	p := a.progress("Checking for updates…")
	defer p.end()
	return svc.CheckUpdates(ctx, skills, func(done, total int) {
		p.set(fmt.Sprintf("Checking for updates… %d/%d", done, total))
	})
}

// shortCommit formats a commit for a dim detail: " (3f2c1ab)", or "".
func shortCommit(c string) string {
	if c == "" {
		return ""
	}
	return " (" + c[:min(len(c), 7)] + ")"
}
