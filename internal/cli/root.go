package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/store"
	"github.com/legibet/myskills/internal/tui"
)

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "myskills",
		Short: "Manage agent skills in a private library and enable them where needed",
		Long: `myskills keeps agent skills (folders with SKILL.md) in a private library at
~/.myskills/skills ($MYSKILLS_HOME/skills) and enables them per project or
globally for the agents you use: as links (personal, kept out of git) or as
copies (committed with the project).

Run without arguments in a terminal to open the interactive app.`,
		Example: `  myskills add anthropics/skills@pdf
  myskills enable pdf
  myskills status`,
		Version:       versionString(),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			msg := fmt.Sprintf("unknown command %q", args[0])
			if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
				msg += fmt.Sprintf("; did you mean %q?", s[0])
			}
			return usageError(cmd, errors.New(msg))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !a.interactive {
				return cmd.Help()
			}
			return a.openTUI(tui.Skills, "")
		},
	}
	root.SuggestionsMinimumDistance = 2
	root.SetVersionTemplate("myskills {{.Version}}\n")
	root.SetFlagErrorFunc(usageError)
	root.AddGroup(
		&cobra.Group{ID: "library", Title: "Library:"},
		&cobra.Group{ID: "enable", Title: "Enabling:"},
		&cobra.Group{ID: "update", Title: "Updates:"},
		&cobra.Group{ID: "config", Title: "Configuration:"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("library", a.findCmd(), a.addCmd(), a.listCmd(), a.showCmd(), a.pathCmd(), a.removeCmd())
	add("enable", a.enableCmd(), a.disableCmd(), a.statusCmd())
	add("update", a.checkCmd(), a.updateCmd())
	add("config", a.agentsCmd())
	return root
}

// scope resolves the -g and -C flags: the global scope, or the project
// containing dir.
func (a *app) scope(global bool, dir string) (core.Scope, error) {
	if global {
		return core.Global, nil
	}
	svc, err := a.service()
	if err != nil {
		return core.Scope{}, err
	}
	sc, err := svc.ProjectScope(dir)
	if errors.Is(err, core.ErrNoProject) {
		return core.Scope{}, withHint(err, "Use -g for the global scope, or -C <dir> to pick a project.")
	}
	return sc, err
}

// project returns the project containing dir, or nil when dir is not in one.
func (a *app) project(dir string) (*core.Scope, error) {
	sc, err := a.scope(false, dir)
	if errors.Is(err, core.ErrNoProject) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// scopeFlags registers -g and -C on cmd.
func scopeFlags(cmd *cobra.Command, global *bool, dir *string) {
	cmd.Flags().BoolVarP(global, "global", "g", false, "use the global scope instead of the current project")
	cmd.Flags().StringVarP(dir, "dir", "C", ".", "find the project from `dir` instead of the current directory")
	_ = cmd.MarkFlagDirname("dir")
}

// agentFlag registers -a/--agent on cmd with completion of agent ids.
func (a *app) agentFlag(cmd *cobra.Command, agents *[]string, usage string) {
	cmd.Flags().StringSliceVarP(agents, "agent", "a", nil, usage)
	_ = cmd.RegisterFlagCompletionFunc("agent", a.completeAgents(false))
}

// completeSkills completes library skill refs: names, plus library paths
// for names that several skills share. Refs already given are left out.
func (a *app) completeSkills(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
	svc, err := a.service()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	lib, err := svc.Library()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	count := map[string]int{}
	for _, sk := range lib {
		count[sk.Name]++
	}
	var out []cobra.Completion
	for _, sk := range lib {
		ref := sk.Name
		if count[sk.Name] > 1 {
			ref = sk.Path // the name alone would be ambiguous
		}
		if !slices.Contains(args, ref) {
			out = append(out, cobra.CompletionWithDesc(ref, shorten(sanitize.Line(sk.Description), 60)))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeAgents completes agent ids, only custom ones when custom is set.
// Ids already given are left out.
func (a *app) completeAgents(custom bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		svc, err := a.service()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var agents []agent.Agent
		if custom {
			var cfg store.Config
			cfg, err = svc.Config()
			agents = cfg.CustomAgents
		} else {
			agents, err = svc.Agents()
		}
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []cobra.Completion
		for _, ag := range agents {
			if !slices.Contains(args, ag.ID) {
				out = append(out, cobra.CompletionWithDesc(ag.ID, ag.Name))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// shorten cuts s to at most n runes, ending with "…" when cut.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// oneArg limits a completion to the first argument.
func oneArg(f cobra.CompletionFunc) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return f(cmd, args, toComplete)
	}
}
