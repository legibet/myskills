package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/core"
)

func (a *app) removeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <skill>…",
		Aliases: []string{"rm"},
		Short:   "Remove skills from the library",
		Long: `Remove skills from the library. Their links are removed in every known project
and globally; copies stay in place as unmanaged files.

A skill inside a linked entry (a folder linked with 'add --link') can only be
removed with the whole entry: the link is removed, never the files behind it.

Asks for confirmation in a terminal; elsewhere pass -y.`,
		Example:           `  myskills remove tdd review -y`,
		Args:              args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: a.completeSkills,
		RunE: func(cmd *cobra.Command, refs []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			lib, err := svc.Library()
			if err != nil {
				return err
			}
			// Resolve everything before removing anything; skills sharing a
			// linked entry are removed once.
			var targets []core.Skill
			seen := map[string]bool{}
			for _, ref := range refs {
				sk, err := svc.Resolve(ref)
				if err != nil {
					return a.refError(ref, err)
				}
				key := sk.Path
				if sk.Linked {
					key = sk.Entry
				}
				if !seen[key] {
					seen[key] = true
					targets = append(targets, sk)
				}
			}
			var names []string
			for _, sk := range targets {
				names = append(names, sk.Path)
				if sk.Linked && sk.Path != sk.Entry {
					n := 0
					for _, other := range lib {
						if other.Entry == sk.Entry {
							n++
						}
					}
					a.warn(fmt.Sprintf("%s is in the linked entry %s: the whole entry (%s) will be removed", sk.Path, sk.Entry, plural(n, "skill")))
				}
			}
			if !yes {
				if !a.interactive {
					return withHint(errors.New("removing skills needs confirmation"), "Pass -y to remove without asking.")
				}
				ok, err := a.confirm(cmd.Context(), fmt.Sprintf("Remove %s from the library?", strings.Join(names, ", ")))
				if err != nil {
					return err
				}
				if !ok {
					a.note("Nothing removed.")
					return nil
				}
			}

			for _, sk := range targets {
				if err := cmd.Context().Err(); err != nil {
					return err // interrupted: leave the remaining skills
				}
				res, err := svc.Remove(sk)
				if res.Entry != "" {
					detail := ""
					if sk.Linked {
						detail = dim(fmt.Sprintf(" (linked entry with %s; files in %s untouched)", plural(len(res.Skills), "skill"), tilde(sk.LinkTarget)))
					}
					a.ok("removed " + res.Entry + detail)
				}
				for _, p := range res.Unlinked {
					a.ok("unlinked " + tilde(p))
				}
				for _, p := range res.Released {
					a.warn("left the copy at " + tilde(p) + dim(" (no longer managed)"))
				}
				for _, w := range res.Warnings {
					a.warn(w)
				}
				if err != nil {
					a.failErr(err)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking for confirmation")
	return cmd
}
