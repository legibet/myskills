package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/tui"
)

type addOptions struct {
	skills []string
	all    bool
	ref    string
	link   bool
}

func (a *app) addCmd() *cobra.Command {
	var o addOptions
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills to the library from GitHub, a git remote or a local folder",
		Long: `Add skills to the library. Adding never enables a skill; use 'myskills enable'.

Sources:
  owner/repo[/dir]              GitHub repository, optionally a folder in it
  owner/repo@skill              only the skill with that name
  https://github.com/o/r/tree/<ref>/<dir>
  https://gitlab.com/g/r/-/tree/<ref>/<dir>
  git@host:path, ssh://…, https://….git, file://…   any git remote
  <remote>#<ref>                a branch or tag
  ./dir, ../dir, /dir, ~/dir    a local folder (copied; --link links it instead)

When a source has several skills, pick them with -s or --all; in a terminal
a picker opens instead.`,
		Example: `  myskills add anthropics/skills -s pdf -s docx
  myskills add https://github.com/owner/repo --all
  myskills add ~/dev/my-skill --link`,
		Args: args(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.add(cmd, args[0], o)
		},
	}
	f := cmd.Flags()
	f.StringArrayVarP(&o.skills, "skill", "s", nil, "add only the skill with this `name` (repeatable)")
	f.BoolVar(&o.all, "all", false, "add every skill in the source")
	f.StringVar(&o.ref, "ref", "", "git branch or tag to add from (for refs containing '/')")
	f.BoolVar(&o.link, "link", false, "link a local folder (a skill or a collection) instead of copying it")
	return cmd
}

func (a *app) add(cmd *cobra.Command, input string, o addOptions) error {
	spec, err := source.Parse(input)
	if err != nil {
		return err
	}
	if o.link {
		return a.addLink(cmd, spec, o)
	}
	if o.ref != "" {
		if spec.Kind != source.Git {
			return usageError(cmd, errors.New("--ref applies to git sources only"))
		}
		spec.Ref = o.ref
	}
	for _, n := range o.skills {
		if !slices.Contains(spec.Skills, n) {
			spec.Skills = append(spec.Skills, n)
		}
	}
	if o.all && len(spec.Skills) > 0 {
		return usageError(cmd, errors.New("--all can't be combined with -s or owner/repo@skill"))
	}

	svc, err := a.service()
	if err != nil {
		return err
	}
	f, err := a.fetch(cmd.Context(), svc, spec)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	picks, err := a.choose(f, o.all)
	if err != nil || len(picks) == 0 {
		return err
	}
	for _, r := range svc.Install(cmd.Context(), f, picks) {
		switch {
		case r.Err != nil:
			a.failErr(r.Err)
		case r.Already:
			a.warn(fmt.Sprintf("%s is already in the library", r.Path) + dim(fmt.Sprintf(" (use 'myskills update %s' to update it)", r.Path)))
		default:
			a.ok("added " + r.Path + dim(" ("+candidateOrigin(f, r.Candidate)+")"))
		}
	}
	return nil
}

// addLink adds a local folder as a linked library entry.
func (a *app) addLink(cmd *cobra.Command, spec source.Spec, o addOptions) error {
	if spec.Kind != source.Local {
		return usageError(cmd, errors.New("--link works with local folders only, like ./dir or ~/dir"))
	}
	if len(o.skills) > 0 || o.all || o.ref != "" {
		return usageError(cmd, errors.New("--link adds the whole folder; it can't be combined with -s, --all or --ref"))
	}
	svc, err := a.service()
	if err != nil {
		return err
	}
	name, err := svc.LinkLocal(spec.Path)
	if err != nil {
		return err
	}
	a.ok("linked " + name + " → " + core.Tilde(spec.Path))
	return nil
}

// fetch clones or reads the source, with a spinner for remote sources.
func (a *app) fetch(ctx context.Context, svc *core.Service, spec source.Spec) (*core.Fetched, error) {
	if spec.Kind == source.Git {
		p := a.progress("Fetching " + source.Label(spec.URL) + "…")
		defer p.end()
	}
	return svc.Fetch(ctx, spec)
}

// choose returns the candidates to install: the named ones (Fetch keeps only
// those), all of them, the only one, or the user's picks. Nil means nothing
// to do.
func (a *app) choose(f *core.Fetched, all bool) ([]core.Candidate, error) {
	cands := f.Candidates
	switch {
	case len(f.Spec.Skills) > 0:
		return cands, nil
	case all:
		var picks []core.Candidate
		for _, c := range cands {
			if c.Issue != "" {
				a.fail(fmt.Sprintf("%s can't be added: %s", sanitize.Line(c.Name), sanitize.Line(c.Issue)))
				continue
			}
			picks = append(picks, c)
		}
		return picks, nil
	case len(cands) == 1:
		return cands, nil
	case a.interactive:
		picks, err := tui.Pick(f)
		if err == nil && len(picks) == 0 {
			a.note("Nothing added.")
		}
		return picks, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d skills; choose with -s <name> (repeatable) or --all:\n", f.Spec.Input, len(cands))
	rows := make([][]string, len(cands))
	for i, c := range cands {
		rows[i] = []string{sanitize.Line(c.Name), dim(cmp.Or(c.RelPath, "."))}
	}
	b.WriteString(table("    ", rows))
	return nil, errors.New(b.String())
}

// candidateOrigin describes where a candidate came from: "owner/repo,
// skills/pdf" or a local folder.
func candidateOrigin(f *core.Fetched, c core.Candidate) string {
	if f.Spec.Kind == source.Local {
		return core.Tilde(c.Dir)
	}
	if c.RelPath == "" {
		return source.Label(f.Spec.URL)
	}
	return source.Label(f.Spec.URL) + ", " + c.RelPath
}
