package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/core"
)

// projectsPage is the state of the Projects page.
type projectsPage struct {
	list  ui.ListState
	sel   int
	root  string // the place chosen, kept as the rows change; "" is Global
	split float32
	add   addSkills
}

// addSkills is the Add Skills dialog of a place.
type addSkills struct {
	open  bool
	scope core.Scope
	query string
	picks map[string]bool // library paths checked
	mode  int             // 0 link, 1 copy
}

func (a *app) projectsView(c *ui.Context) {
	if !a.data.read {
		loading(c)
		return
	}
	places := a.data.places
	a.projects.sel = max(0, slices.IndexFunc(places, func(p place) bool { return p.scope.Root == a.projects.root }))
	if len(places) > 0 {
		a.projects.root = places[a.projects.sel].scope.Root
	}
	ui.Split(c, &a.projects.split, func() {
		ui.Column(c).Fill().Children(func() {
			n := len(places) - 1
			bar(c).Padding(0, 10, 0, 16).Children(func() {
				paneTitle(c, "Projects", fmt.Sprintf("%d %s", n, plural(n, "project", "projects")))
			})
			s := &a.projects.list
			s.Selected = &a.projects.sel
			s.Key = func(i int) any { return places[i].scope.Root }
			s.Label = func(i int) string { return placeName(places[i].scope) }
			list := sourceList(c, s, len(places), "Places", func(i int, onAccent bool) { a.placeRow(c, &places[i], onAccent) })
			if list.Changed() && a.projects.sel >= 0 && a.projects.sel < len(places) {
				a.projects.root = places[a.projects.sel].scope.Root
			}
		})
	}, func() {
		if p, ok := a.data.place(core.Scope{Root: a.projects.root}); ok {
			a.placeDetail(c, p)
		}
	}).Grow(1)
	a.addSkillsView(c)
}

// placeRow is a place in the list: its name and path, how many skills it
// has, and how many need attention.
func (a *app) placeRow(c *ui.Context, p *place, onAccent bool) {
	where := "Every project"
	if !p.scope.IsGlobal() {
		where = tilde(p.scope.Root)
	}
	n := len(p.status.Enablements)
	listRow(c, onAccent, func() {
		ui.Text(c, placeName(p.scope)).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if k := p.attention(); k > 0 {
			rowTag(c, strconv.Itoa(k), c.Theme().Warning, onAccent).Tooltip(fmt.Sprintf("%d %s attention", k, plural(k, "skill needs", "skills need")))
		}
	}, fmt.Sprintf("%d %s · %s", n, plural(n, "skill", "skills"), where))
}

// placeDetail is the place chosen: its skills, its broken links and the
// entries myskills doesn't manage.
func (a *app) placeDetail(c *ui.Context, p *place) {
	key := "place:" + p.scope.Root
	busy := a.working(c, key)
	ui.Column(c).Fill().Children(func() {
		detailBar(c, func() {
			if busy {
				spinner(c, "Working")
			}
			if p.err != nil {
				return
			}
			if refreshable(p) {
				iconTextButton(c, iconRefresh, "Refresh Copies", plain).Disabled(busy).OnClick(func() {
					a.start(a.refresh(p.scope, nil, key, "Couldn't refresh the copies in "+placeName(p.scope)), false)
				})
			}
			if !p.scope.IsGlobal() {
				toolButton(c, iconFolder, revealLabel).OnClick(func() { a.desk.reveal(p.scope.Root) })
			}
			button(c, "Add Skills…", primary).Disabled(busy).OnClick(func() { a.openAddSkills(p.scope) })
		})
		detailPane(c, key, func() {
			description := ""
			if p.scope.IsGlobal() {
				description = "Skills every project gets, from the agents' global skill directories."
			}
			detailHeader(c, placeName(p.scope), description, func() {
				if !p.scope.IsGlobal() {
					metaText(c, tilde(p.scope.Root))
				}
			})
			if p.err != nil {
				note(c, "Couldn't read this place: "+p.err.Error())
				return
			}
			if len(p.status.Enablements) == 0 && len(p.status.Broken) == 0 && len(p.status.Others) == 0 {
				emptyState(c, "No skills here yet", "Add skills from your library to use them here.", nil)
				return
			}
			if len(p.status.Enablements) > 0 {
				group(c, "Skills", func(g *rowGroup) {
					for i := range p.status.Enablements {
						e := &p.status.Enablements[i]
						if sk, ok := a.data.skill(e.Skill); ok {
							a.placementRow(c, g, p, sk, e, a.skillLead(c, sk))
						}
					}
				})
			}
			if len(p.status.Broken) > 0 {
				a.brokenLinks(c, p)
			}
			if len(p.status.Others) > 0 {
				unmanaged(c, p.status.Others)
			}
		})
	})
}

// skillLead is a placement row's lead on Projects: the skill, whose name
// opens it on Skills.
func (a *app) skillLead(c *ui.Context, sk core.Skill) func() {
	return func() {
		p := colors(c)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			name := ui.ButtonBase(c).AlignSelf(ui.Start).MinWidth(0).Label(a.data.skillName(sk)).Tooltip("Show in Skills").Cursor(ui.CursorPointer)
			name.Children(func() {
				text := ui.Text(c, a.data.skillName(sk)).FontWeight(600).SingleLine()
				if name.Hovered() {
					text.TextColor(accent(c))
				}
			}).OnClick(func() { a.show(sk.Path) })
			if sk.Description != "" {
				ui.Text(c, sk.Description).FontSize(textSmall).TextColor(p.text2).SingleLine()
			}
		})
	}
}

// brokenLinks lists the links into the library whose skill is gone.
func (a *app) brokenLinks(c *ui.Context, p *place) {
	pal := colors(c)
	group(c, "Broken links", func(g *rowGroup) {
		for _, b := range p.status.Broken {
			g.row(c.Key(b.Path)).Children(func() {
				ui.Icon(c, iconWarning).Size(16, 16).Shrink(0).TextColor(pal.red)
				rowText(c, b.Name, tilde(b.Path)+" → "+b.Skill+", missing from the library")
				if a.working(c, "broken:"+b.Path) {
					spinner(c, "Working")
					return
				}
				button(c, "Remove", plain).OnClick(func() { a.removeBroken(p.scope, b) })
			})
		}
	})
}

// unmanaged lists the entries myskills didn't make, which it never touches.
func unmanaged(c *ui.Context, others []core.Placement) {
	group(c, "Not managed by myskills", func(g *rowGroup) {
		for _, o := range others {
			g.row(c.Key(o.Path)).MinHeight(44).Children(func() {
				rowText(c, o.Name, tilde(o.Path))
			})
		}
	})
}

// addSkillsView is the Add Skills dialog: a searchable checklist of the
// valid library skills the place doesn't have, and the mode to enable them
// in.
func (a *app) addSkillsView(c *ui.Context) {
	d := &a.projects.add
	if !d.open {
		return
	}
	p, ok := a.data.place(d.scope)
	if !ok {
		d.open = false
		return
	}
	pal := colors(c)
	q := strings.ToLower(strings.TrimSpace(d.query))
	var rows []core.Skill
	for _, sk := range a.data.lib {
		if _, on := p.enablement(sk.Path); on || !sk.Valid() {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(sk.Path+"\x00"+sk.Name+"\x00"+sk.Description), q) {
			rows = append(rows, sk)
		}
	}
	picked := 0
	for path, on := range d.picks {
		if _, ok := a.data.skill(path); on && ok {
			picked++
		}
	}
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(500).Gap(14).Children(func() {
			ui.Text(c, "Add Skills to "+placeName(d.scope)).FontSize(15).FontWeight(700)
			field, _ := searchField(c, &d.query, "Search", nil)
			field.AutoFocus()
			ui.Scroll(c).Height(300).Radius(10).Background(pal.card).Border(1, pal.separator).Children(func() {
				ui.Column(c).Padding(4, 0).Children(func() {
					if len(rows) == 0 {
						ui.Text(c, "No skills to add").FontSize(textSmall).TextColor(pal.text2).Padding(24).TextAlign(ui.Center)
					}
					for _, sk := range rows {
						on := d.picks[sk.Path]
						ui.Column(c.Key(sk.Path)).Padding(7, 14).Gap(2).Children(func() {
							if ui.Checkbox(c, &on, a.data.skillName(sk)).Changed() {
								d.picks[sk.Path] = on
							}
							if sk.Description != "" {
								ui.Text(c, sk.Description).FontSize(textSmall).TextColor(pal.text2).Margin(0, 0, 0, 26).MaxLines(2)
							}
						})
					}
				})
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				segmented(c, &d.mode, "Mode", "Link", "Copy").Width(130).Shrink(0)
				ui.Text(c, modeHint(d.mode)).FontSize(textSmall).TextColor(pal.text2).Grow(1)
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if button(c, "Cancel", plain).Clicked() {
					d.open = false
				}
				label := "Enable"
				if picked > 0 {
					label = fmt.Sprintf("Enable %d", picked)
				}
				if button(c, label, primary).Disabled(picked == 0).Clicked() {
					d.open = false
					a.enableMany(d.scope, d.picks, []core.Mode{core.Link, core.Copy}[d.mode])
				}
			})
		})
	})
}

// refreshable reports whether a place has outdated copies Refresh Copies
// replaces without asking.
func refreshable(p *place) bool {
	return slices.ContainsFunc(p.status.Enablements, func(e core.Enablement) bool {
		return slices.ContainsFunc(e.Placements, func(pl core.Placement) bool { return pl.Outdated && !pl.Modified })
	})
}

func (a *app) openAddSkills(sc core.Scope) {
	a.projects.add = addSkills{open: true, scope: sc, picks: map[string]bool{}}
}

func modeHint(mode int) string {
	if mode == 1 {
		return "Copied into the project, to commit with it."
	}
	return "Linked to the library, and kept out of git."
}

// enableMany enables the skills picked in a place, each its own change, and
// says which failed.
func (a *app) enableMany(sc core.Scope, picks map[string]bool, mode core.Mode) {
	var skills []core.Skill
	for _, sk := range a.data.lib {
		if picks[sk.Path] {
			skills = append(skills, sk)
		}
	}
	a.start(op{key: "place:" + sc.Root, fail: "Some skills weren't enabled in " + placeName(sc), run: func(bool) outcome {
		var out outcome
		var errs []error
		for _, sk := range skills {
			res, err := a.svc.Enable(sc, sk, core.EnableOptions{Mode: mode})
			out.warnings = append(out.warnings, warnings(res)...)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sk.Name, err))
			}
		}
		out.err = errors.Join(errs...)
		return out
	}}, false)
}
