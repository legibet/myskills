package cli

import (
	"fmt"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/skill"
)

func (a *app) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <skill>",
		Short: "Show a library skill and its SKILL.md",
		Long: `Show a library skill: where it lives, its source, where it is enabled, and
its SKILL.md, rendered in a terminal and as plain markdown otherwise.`,
		Example:           `  myskills show tdd | less`,
		Args:              args(cobra.ExactArgs(1)),
		ValidArgsFunction: oneArg(a.completeSkills),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			sk, err := svc.Resolve(args[0])
			if err != nil {
				return a.refError(args[0], err)
			}
			usage, err := svc.Usage()
			if err != nil {
				return err
			}

			title := bold(sk.Name)
			if sk.Path != sk.Name {
				title += " " + dim(sk.Path)
			}
			a.println(title)
			if sk.Description != "" {
				a.println(sanitize.Line(sk.Description))
			}
			where := whereEnabled(usage[sk.Path])
			if where == "" {
				where = "not enabled"
			}
			rows := [][]string{
				{dim("path"), tilde(sk.Dir)},
				{dim("source"), sk.SourceLabel()},
				{dim("enabled"), where},
			}
			if sk.Issue != "" {
				rows = append(rows, []string{dim("issue"), styleErr.Render(sanitize.Line(sk.Issue))})
			}
			if sk.Modified {
				rows = append(rows, []string{dim("modified"), styleWarn.Render("changed since it was installed")})
			}
			a.println("")
			a.println(table("  ", rows))

			body, err := skill.Body(sk.Dir)
			if err != nil {
				return nil // the issue above explains it
			}
			a.println("")
			return a.printMarkdown(sanitize.Text(body))
		},
	}
}

// printMarkdown renders markdown for the terminal, or prints it as is when
// stdout is not a terminal.
func (a *app) printMarkdown(md string) error {
	if !a.outTTY {
		_, err := fmt.Fprint(a.out, strings.TrimRight(md, "\n")+"\n")
		return err
	}
	width, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || width <= 0 {
		width = 80
	}
	style := styles.LightStyle
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		style = styles.DarkStyle
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(width))
	if err != nil {
		return err
	}
	out, err := r.Render(md)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.out, sanitize.Styled(out))
	return err
}

func (a *app) pathCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "path <skill>",
		Short:             "Print the absolute directory of a library skill",
		Example:           `  cd "$(myskills path tdd)"`,
		Args:              args(cobra.ExactArgs(1)),
		ValidArgsFunction: oneArg(a.completeSkills),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			sk, err := svc.Resolve(args[0])
			if err != nil {
				return a.refError(args[0], err)
			}
			_, err = fmt.Fprintln(a.rawOut, sk.Dir)
			return err
		},
	}
}
