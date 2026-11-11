package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/tui"
)

// findJSON is the output of `find --json`.
type findJSON struct {
	Results []findResultJSON `json:"results"`
}

// findResultJSON is one skills.sh search result.
type findResultJSON struct {
	Source    string `json:"source"`  // GitHub owner/repo
	SkillID   string `json:"skillId"` // skill name; add with "<source>@<skillId>"
	Name      string `json:"name"`    // display name
	Installs  int    `json:"installs"`
	InLibrary string `json:"inLibrary,omitempty"` // library path when already added
}

func (a *app) findCmd() *cobra.Command {
	var (
		asJSON bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "find [query]",
		Short: "Search skills.sh for skills to add",
		Long: `Search skills.sh. In a terminal this opens the interactive Find view, where
results can be previewed and added; otherwise it prints one result per line:
owner/repo@skill, installs, name. Add a result with 'myskills add owner/repo@skill'.`,
		Example: `  myskills find react testing
  myskills find pdf --json -n 5`,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			if a.interactive && !asJSON {
				return a.openTUI(tui.Find, query)
			}
			if strings.TrimSpace(query) == "" {
				return usageError(cmd, errors.New("a search query is required"))
			}
			svc, err := a.service()
			if err != nil {
				return err
			}
			p := a.progress("Searching skills.sh…")
			results, err := svc.SkillsSH.Search(cmd.Context(), query, limit)
			p.end()
			if err != nil {
				return err
			}
			lib, err := svc.Library()
			if err != nil {
				return err
			}

			out := findJSON{Results: []findResultJSON{}}
			for _, r := range results {
				fr := findResultJSON{Source: r.Source, SkillID: r.SkillID, Name: r.Name, Installs: r.Installs}
				if sk, ok := core.Installed(lib, r.Spec().URL, r.SkillID); ok {
					fr.InLibrary = sk.Path
				}
				out.Results = append(out.Results, fr)
			}
			if asJSON {
				return a.printJSON(out)
			}
			if len(results) == 0 {
				a.note("No skills found.")
				return nil
			}
			var rows [][]string
			for _, r := range out.Results {
				row := []string{sanitize.Line(r.Source + "@" + r.SkillID), strconv.Itoa(r.Installs), dim(sanitize.Line(r.Name))}
				if r.InLibrary != "" {
					row = append(row, styleOK.Render("in library"))
				}
				rows = append(rows, row)
			}
			a.println(table("", rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "maximum number of results")
	return cmd
}
