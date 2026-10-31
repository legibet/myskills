package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
)

// listJSON is the output of `list --json`.
type listJSON struct {
	Skills []skillJSON `json:"skills"`
}

// skillJSON is a library skill.
type skillJSON struct {
	Path        string      `json:"path"` // library path, e.g. "tdd" or "collection/review"
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Dir         string      `json:"dir"`         // absolute directory
	Source      *sourceJSON `json:"source"`      // null when the skill has no record
	SourceLabel string      `json:"sourceLabel"` // "owner/repo", the linked folder ("~/agent-skills"), or "local"
	Linked      bool        `json:"linked"`      // inside a linked library entry
	Updatable   bool        `json:"updatable"`
	Issue       string      `json:"issue,omitempty"` // why the skill is unusable
	Modified    bool        `json:"modified"`        // changed since it was installed
	Enabled     enabledJSON `json:"enabled"`
}

// sourceJSON is where a skill was installed from.
type sourceJSON struct {
	Type   string `json:"type"` // "git" or "local"
	URL    string `json:"url,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"` // folder in the repository, or the local folder
	Commit string `json:"commit,omitempty"`
}

// enabledJSON is where a skill is enabled.
type enabledJSON struct {
	Global   bool     `json:"global"`
	Projects []string `json:"projects"` // project roots
}

func (a *app) listCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the skills in the library",
		Long: `List the skills in the library with their source, where they are enabled,
and issues: invalid skills, local modifications, missing linked folders.`,
		Args:              args(cobra.NoArgs),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(*cobra.Command, []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			lib, err := svc.Library()
			if err != nil {
				return err
			}
			usage, err := svc.Usage()
			if err != nil {
				return err
			}
			if asJSON {
				out := listJSON{Skills: []skillJSON{}}
				for _, sk := range lib {
					out.Skills = append(out.Skills, newSkillJSON(sk, usage[sk.Path]))
				}
				return a.printJSON(out)
			}
			if len(lib) == 0 {
				a.note("The library is empty.")
				return nil
			}
			dup := core.DuplicateNames(lib)
			rows := [][]string{{bold("NAME"), bold("SOURCE"), bold("ENABLED"), bold("ISSUES")}}
			for _, sk := range lib {
				name := sk.Name
				if dup[sk.Name] {
					name += " " + dim(sk.Path)
				}
				where := whereEnabled(usage[sk.Path])
				if where == "" {
					where = dim("—")
				}
				var issues string
				switch {
				case sk.Issue != "":
					issues = styleErr.Render(sanitize.Line(sk.Issue))
				case sk.Modified:
					issues = styleWarn.Render("modified")
				}
				rows = append(rows, []string{name, dim(sk.SourceLabel()), where, issues})
			}
			a.println(table("", rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newSkillJSON(sk core.Skill, scopes []core.Scope) skillJSON {
	out := skillJSON{
		Path:        sk.Path,
		Name:        sk.Name,
		Description: sk.Description,
		Dir:         sk.Dir,
		SourceLabel: sk.SourceLabel(),
		Linked:      sk.Linked,
		Updatable:   sk.Updatable(),
		Issue:       sk.Issue,
		Modified:    sk.Modified,
		Enabled:     enabledJSON{Projects: []string{}},
	}
	if r := sk.Record; r != nil {
		out.Source = &sourceJSON{Type: r.Source.Type, URL: r.Source.URL, Ref: r.Source.Ref, Path: r.Source.Path, Commit: r.Commit}
	}
	for _, sc := range scopes {
		if sc.IsGlobal() {
			out.Enabled.Global = true
		} else {
			out.Enabled.Projects = append(out.Enabled.Projects, sc.Root)
		}
	}
	return out
}

// whereEnabled summarizes scopes: "global", "2 projects", "global + 1
// project", or "" for none.
func whereEnabled(scopes []core.Scope) string {
	global, projects := false, 0
	for _, sc := range scopes {
		if sc.IsGlobal() {
			global = true
		} else {
			projects++
		}
	}
	switch {
	case global && projects > 0:
		return fmt.Sprintf("global + %s", plural(projects, "project"))
	case global:
		return "global"
	case projects > 0:
		return plural(projects, "project")
	}
	return ""
}
