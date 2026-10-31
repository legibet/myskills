package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
)

// statusJSON is the output of `status --json`.
type statusJSON struct {
	Project *scopeJSON `json:"project"` // null outside a project or with -g
	Global  scopeJSON  `json:"global"`
}

// scopeJSON is what was found in the agent skill directories of a scope.
type scopeJSON struct {
	Root        string           `json:"root,omitempty"` // project root; absent for global
	Enablements []enablementJSON `json:"enablements"`
	Broken      []placementJSON  `json:"broken"` // links whose library skill is gone
	Others      []placementJSON  `json:"others"` // entries myskills does not manage
}

// enablementJSON is a library skill's placements in one scope.
type enablementJSON struct {
	Skill      string          `json:"skill"` // library path
	Name       string          `json:"name"`
	Mode       string          `json:"mode"`     // "link", "copy" or "mixed"
	Agents     []string        `json:"agents"`   // agent ids
	Partial    bool            `json:"partial"`  // a default agent has no placement
	Outdated   bool            `json:"outdated"` // a copy differs from the library
	Modified   bool            `json:"modified"` // a copy was changed locally
	Placements []placementJSON `json:"placements"`
}

// placementJSON is an entry in an agent skill directory.
type placementJSON struct {
	Path     string   `json:"path"` // absolute
	Name     string   `json:"name"`
	Agents   []string `json:"agents"`
	Kind     string   `json:"kind"`            // "link", "copy", "broken" or "other"
	Skill    string   `json:"skill,omitempty"` // library path, for managed entries
	Modified bool     `json:"modified"`
	Outdated bool     `json:"outdated"`
}

func (a *app) statusCmd() *cobra.Command {
	var (
		global bool
		dir    string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what is enabled in the current project and globally",
		Long: `Show the skills enabled in the current project (if any) and globally: mode,
agents, and whether copies are outdated or modified, or an enablement misses a
default agent (partial). Broken links and entries myskills doesn't manage are
listed dimmed.`,
		Args:              args(cobra.NoArgs),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(*cobra.Command, []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			var scopes []core.Scope
			if !global {
				project, err := a.project(dir)
				if err != nil {
					return err
				}
				if project != nil {
					scopes = append(scopes, *project)
				}
			}
			scopes = append(scopes, core.Global)
			var statuses []core.Status
			for _, sc := range scopes {
				st, err := svc.Status(sc)
				if err != nil {
					return err
				}
				statuses = append(statuses, st)
			}

			if asJSON {
				var out statusJSON
				for _, st := range statuses {
					if st.Scope.IsGlobal() {
						out.Global = newScopeJSON(st)
					} else {
						s := newScopeJSON(st)
						out.Project = &s
					}
				}
				return a.printJSON(out)
			}
			lib, err := svc.Library()
			if err != nil {
				return err
			}
			dup := core.DuplicateNames(lib)
			for i, st := range statuses {
				if i > 0 {
					a.println("")
				}
				a.printStatus(st, dup)
			}
			return nil
		},
	}
	scopeFlags(cmd, &global, &dir)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// printStatus prints a scope's enablements, then its broken and unmanaged
// entries. dup holds the names shown with their library path.
func (a *app) printStatus(st core.Status, dup map[string]bool) {
	if st.Scope.IsGlobal() {
		a.println(bold("Global"))
	} else {
		a.println(bold("Project ") + st.Scope.String())
	}
	s := newScopeJSON(st)
	if len(s.Enablements)+len(s.Broken)+len(s.Others) == 0 {
		a.note("  nothing enabled")
		return
	}
	var rows [][]string
	for _, e := range s.Enablements {
		name := e.Name
		if dup[e.Name] {
			name += " " + dim(e.Skill)
		}
		var flags []string
		if e.Outdated {
			flags = append(flags, "outdated")
		}
		if e.Modified {
			flags = append(flags, "modified")
		}
		if e.Partial {
			flags = append(flags, "partial")
		}
		rows = append(rows, []string{name, e.Mode, a.agentNames(e.Agents), styleWarn.Render(strings.Join(flags, ", "))})
	}
	for _, p := range slices.Concat(s.Broken, s.Others) {
		rows = append(rows, []string{dim(p.Name), dim(p.Kind), dim(scopePath(st.Scope, p.Path))})
	}
	a.println(table("  ", rows))
}

// placementAgents returns the agents of placements, in order, without
// duplicates.
func placementAgents(ps []core.Placement) []string {
	ids := []string{}
	for _, p := range ps {
		for _, id := range p.Agents {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func newScopeJSON(st core.Status) scopeJSON {
	out := scopeJSON{
		Root:        st.Scope.Root,
		Enablements: []enablementJSON{},
		Broken:      newPlacementsJSON(st.Broken),
		Others:      newPlacementsJSON(st.Others),
	}
	for _, e := range st.Enablements {
		ej := enablementJSON{
			Skill:      e.Skill,
			Name:       e.Name,
			Mode:       string(e.Mode),
			Agents:     placementAgents(e.Placements),
			Partial:    e.Partial,
			Placements: newPlacementsJSON(e.Placements),
		}
		for _, p := range e.Placements {
			ej.Outdated = ej.Outdated || p.Outdated
			ej.Modified = ej.Modified || p.Modified
		}
		out.Enablements = append(out.Enablements, ej)
	}
	return out
}

func newPlacementsJSON(ps []core.Placement) []placementJSON {
	out := []placementJSON{}
	for _, p := range ps {
		out = append(out, placementJSON{
			Path:     p.Path,
			Name:     p.Name,
			Agents:   append([]string{}, p.Agents...),
			Kind:     string(p.Kind),
			Skill:    p.Skill,
			Modified: p.Modified,
			Outdated: p.Outdated,
		})
	}
	return out
}
