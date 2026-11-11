package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/tui"
)

func (a *app) enableCmd() *cobra.Command {
	var (
		global         bool
		dir            string
		agents         []string
		asLink, asCopy bool
		force          bool
	)
	cmd := &cobra.Command{
		Use:   "enable [skill…]",
		Short: "Enable library skills in the current project or globally",
		Long: `Enable library skills for the default agents (see 'myskills agents') or the
ones given with -a, in the current project or globally with -g.

A new enablement is a link: personal, and kept out of git through the
repository's info/exclude file. --copy puts real files in the project instead,
meant to be committed. On an enabled skill, --link and --copy convert the
existing placements and --copy refreshes outdated copies.

A skill is a library path or a name. Without skills, the interactive app opens
in a terminal.`,
		Example: `  myskills enable tdd
  myskills enable tdd review -a claude-code --copy
  myskills enable -g pdf`,
		ValidArgsFunction: a.completeSkills,
		RunE: func(cmd *cobra.Command, refs []string) error {
			if asLink && asCopy {
				return usageError(cmd, errors.New("--link and --copy can't be used together"))
			}
			if len(refs) == 0 {
				return a.noSkills(cmd, global, dir)
			}
			scope, err := a.scope(global, dir)
			if err != nil {
				return err
			}
			opts := core.EnableOptions{Agents: agents, Force: force}
			switch {
			case asLink:
				opts.Mode = core.Link
			case asCopy:
				opts.Mode = core.Copy
			}
			svc, err := a.service()
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				sk, err := svc.Resolve(ref)
				if err != nil {
					a.failErr(a.refError(ref, err))
					continue
				}
				res, err := svc.Enable(scope, sk, opts)
				a.printResult(scope, res)
				if err != nil {
					a.failErr(forceHint(err))
				}
			}
			return nil
		},
	}
	scopeFlags(cmd, &global, &dir)
	a.agentFlag(cmd, &agents, "enable for this agent `id` instead of the defaults (repeatable, or comma-separated)")
	cmd.Flags().BoolVar(&asLink, "link", false, "link to the library (converts existing copies)")
	cmd.Flags().BoolVar(&asCopy, "copy", false, "copy the files (converts existing links, refreshes outdated copies)")
	cmd.Flags().BoolVar(&force, "force", false, "allow deleting locally modified copies when converting")
	return cmd
}

func (a *app) disableCmd() *cobra.Command {
	var (
		global bool
		dir    string
		agents []string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "disable [skill…]",
		Short: "Remove skills' placements from the current project or globally",
		Long: `Remove the placements of skills in the current project, or globally with -g:
for every agent, or only the ones given with -a. Links are removed, and copies
deleted unless they were modified locally (use --force to delete those).

A skill is a library path or a name; a name also matches broken links whose
library skill no longer exists. Without skills, the interactive app opens in a
terminal.`,
		Example: `  myskills disable tdd
  myskills disable -g pdf -a claude-code`,
		ValidArgsFunction: a.completeSkills,
		RunE: func(cmd *cobra.Command, refs []string) error {
			if len(refs) == 0 {
				return a.noSkills(cmd, global, dir)
			}
			scope, err := a.scope(global, dir)
			if err != nil {
				return err
			}
			svc, err := a.service()
			if err != nil {
				return err
			}
			opts := core.DisableOptions{Agents: agents, Force: force}
			for _, ref := range refs {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				res, err := svc.Disable(scope, ref, opts)
				a.printResult(scope, res)
				switch {
				case err != nil:
					a.failErr(forceHint(a.refError(ref, err)))
				case len(res.Changes) == 0:
					a.warn(fmt.Sprintf("%s is not enabled in %s", ref, scope))
				}
			}
			return nil
		},
	}
	scopeFlags(cmd, &global, &dir)
	a.agentFlag(cmd, &agents, "disable only for this agent `id` (repeatable, or comma-separated)")
	cmd.Flags().BoolVar(&force, "force", false, "also delete locally modified copies")
	return cmd
}

// noSkills handles enable and disable without arguments: the interactive
// app in a terminal, an error otherwise.
func (a *app) noSkills(cmd *cobra.Command, global bool, dir string) error {
	if !a.interactive {
		return usageError(cmd, errors.New("no skills given"))
	}
	scope, err := a.scope(global, dir)
	if err != nil {
		return err
	}
	project := &scope
	if global {
		if project, err = a.project(dir); err != nil {
			return err
		}
	}
	return a.runTUI(tui.Options{View: tui.Skills, Scope: scope, Project: project})
}

// refError adds a hint to a failed lookup of a skill ref: library skills
// whose path contains it, or where to see the library.
func (a *app) refError(ref string, err error) error {
	if !errors.Is(err, core.ErrNotFound) {
		return err
	}
	var similar []string
	if svc, e := a.service(); e == nil {
		lib, _ := svc.Library()
		for _, sk := range lib {
			if strings.Contains(strings.ToLower(sk.Path), strings.ToLower(ref)) {
				similar = append(similar, sk.Path)
			}
		}
	}
	if len(similar) > 0 {
		return withHint(err, "Did you mean "+strings.Join(similar[:min(len(similar), 5)], ", ")+"?")
	}
	return withHint(err, "See the library with 'myskills list', or add skills with 'myskills add'.")
}

// forceHint points to --force for ErrModified.
func forceHint(err error) error {
	if errors.Is(err, core.ErrModified) {
		return withHint(err, "Use --force to discard the local changes.")
	}
	return err
}
