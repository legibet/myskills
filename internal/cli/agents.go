package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
)

func (a *app) agentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List agents and their skill directories, and manage the defaults",
		Long: `List the agents whose skill directories myskills manages. Agents marked ✓ are
the defaults for new enablements. Custom agents and the defaults are stored in
~/.myskills/config.json.`,
		Args:              args(cobra.NoArgs),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(*cobra.Command, []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			agents, err := svc.Agents()
			if err != nil {
				return err
			}
			cfg, err := svc.Config()
			if err != nil {
				return err
			}
			defaults := cfg.DefaultAgents()
			rows := [][]string{{"", bold("ID"), bold("NAME"), bold("PROJECT DIR"), bold("GLOBAL DIR")}}
			for _, ag := range agents {
				mark := ""
				if slices.Contains(defaults, ag.ID) {
					mark = styleOK.Render("✓")
				}
				project, global := dim("—"), dim("—")
				if ag.Project != "" {
					project = ag.Project
				}
				if dir, ok := ag.GlobalDir(); ok {
					global = core.Tilde(dir)
				}
				rows = append(rows, []string{mark, ag.ID, ag.Name, project, global})
			}
			a.println(table("", rows))
			return nil
		},
	}
	cmd.AddCommand(a.agentsUseCmd(), a.agentsAddCmd(), a.agentsRemoveCmd())
	return cmd
}

func (a *app) agentsUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "use <id>…",
		Short:             "Set the default agents for new enablements",
		Example:           `  myskills agents use universal claude-code`,
		Args:              args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: a.completeAgents(false),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			var ids []string
			for _, arg := range args {
				for id := range strings.SplitSeq(arg, ",") {
					if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
						ids = append(ids, id)
					}
				}
			}
			if err := svc.SetDefaultAgents(ids); err != nil {
				return err
			}
			a.ok("default agents: " + a.agentNames(ids))
			return nil
		},
	}
}

func (a *app) agentsAddCmd() *cobra.Command {
	var ag agent.Agent
	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add a custom agent",
		Long: `Add a custom agent with a project skill directory (relative to the project
root), a global one (absolute or starting with ~/), or both.`,
		Example:           `  myskills agents add grok --name Grok --project .grok/skills --global ~/.grok/skills`,
		Args:              args(cobra.ExactArgs(1)),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(_ *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			ag.ID = args[0]
			if err := svc.AddAgent(ag); err != nil {
				return err
			}
			a.ok("added agent " + ag.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&ag.Name, "name", "", "display `name` (default: the id)")
	cmd.Flags().StringVar(&ag.Project, "project", "", "skill `dir` relative to a project root, like .grok/skills")
	cmd.Flags().StringVar(&ag.Global, "global", "", "global skill `dir`, like ~/.grok/skills")
	_ = cmd.MarkFlagDirname("global")
	return cmd
}

func (a *app) agentsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a custom agent and its links",
		Long: `Remove a custom agent. Its links are removed globally and in every known
project first; its copies stay as unmanaged files.`,
		Args:              args(cobra.ExactArgs(1)),
		ValidArgsFunction: oneArg(a.completeAgents(true)),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			removed, err := svc.RemoveAgent(args[0])
			for _, p := range removed {
				a.ok("unlinked " + core.Tilde(p))
			}
			if err != nil {
				return err
			}
			a.ok("removed agent " + args[0])
			return nil
		},
	}
}
