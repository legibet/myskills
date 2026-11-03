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
	header(c, "Projects", nil)
	if !a.data.read {
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	}
	places := a.data.places
	a.projects.sel = max(0, slices.IndexFunc(places, func(p place) bool { return p.scope.Root == a.projects.root }))
	if len(places) > 0 {
		a.projects.root = places[a.projects.sel].scope.Root
	}
	ui.Split(c, &a.projects.split, func() {
		s := &a.projects.list
		s.Selected = &a.projects.sel
		s.Key = func(i int) any { return places[i].scope.Root }
		s.Label = func(i int) string { return placeName(places[i].scope) }
		list := ui.List(c, s, len(places)).Fill().Padding(6).Gap(2).Label("Places")
		list.Rows(func(r ui.ListRow) { a.placeRow(r.Context, &places[r.Index], r.Selected()) })
		if list.Changed() && a.projects.sel >= 0 && a.projects.sel < len(places) {
			a.projects.root = places[a.projects.sel].scope.Root
		}
	}, func() {
		if p, ok := a.data.place(core.Scope{Root: a.projects.root}); ok {
			a.placeDetail(c, p)
		}
	}).Grow(1)
	a.addSkillsView(c)
}

// placeRow is a place in the list: its name and path, how many skills it
// has, and whether one needs attention.
func (a *app) placeRow(c *ui.Context, p *place, chosen bool) {
	t := c.Theme()
	sub := t.TextMuted
	if chosen {
		sub = t.AccentText.Alpha(0.8)
	}
	icon := iconProject
	if p.scope.IsGlobal() {
		icon = iconGlobal
	}
	ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).TextColor(sub)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, placeName(p.scope)).FontWeight(600).SingleLine().Grow(1)
				if n := p.attention(); n > 0 {
					if chosen {
						ui.Badge(c, strconv.Itoa(n)).Background(t.AccentText.Alpha(0.2)).TextColor(t.AccentText)
					} else {
						tag(c, strconv.Itoa(n), t.Warning)
					}
				}
			})
			where := "Every project"
			if !p.scope.IsGlobal() {
				where = tilde(p.scope.Root)
			}
			n := len(p.status.Enablements)
			ui.Text(c, fmt.Sprintf("%s · %d %s", where, n, plural(n, "skill", "skills"))).FontSize(12).TextColor(sub).SingleLine()
		})
	})
}

// placeDetail is the place chosen: its skills, its broken links and the
// entries myskills doesn't manage.
func (a *app) placeDetail(c *ui.Context, p *place) {
	t := c.Theme()
	key := "place:" + p.scope.Root
	ui.Scroll(c.Key(key)).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(18).MaxWidth(820).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, placeName(p.scope)).FontSize(20).Bold().SingleLine()
					if p.scope.IsGlobal() {
						muted(c, "Skills every project gets, in the agents' global skill directories.").FontSize(12)
					} else {
						muted(c, tilde(p.scope.Root)).FontSize(12).Selectable()
					}
				})
				if a.working(c, key) {
					ui.Spinner(c).Label("Working")
				}
				if p.err != nil {
					return
				}
				if refreshable(p) {
					ui.Button(c, "").Gap(6).Disabled(a.working(c, key)).Children(func() {
						ui.Icon(c, iconRefresh)
						ui.Text(c, "Refresh Copies")
					}).OnClick(func() {
						a.start(a.refresh(p.scope, nil, key, "Couldn't refresh the copies in "+placeName(p.scope)), false)
					})
				}
				if !p.scope.IsGlobal() {
					iconButton(c, iconFolder, "Reveal in Finder").OnClick(func() { a.desk.reveal(p.scope.Root) })
				}
				ui.PrimaryButton(c, "Add Skills…").Disabled(a.working(c, key)).OnClick(func() { a.openAddSkills(p.scope) })
			})
			if p.err != nil {
				note(c, "Couldn't read this place: "+p.err.Error())
				return
			}
			if len(p.status.Enablements) == 0 && len(p.status.Broken) == 0 && len(p.status.Others) == 0 {
				muted(c, "No skills enabled here yet.").Padding(24, 0).TextAlign(ui.Center)
				return
			}
			if len(p.status.Enablements) > 0 {
				ui.Column(c).Gap(8).Children(func() {
					section(c, "Skills")
					ui.Column(c).Radius(10).Border(1, t.Border).Dividers(1, t.Border).Children(func() {
						for i := range p.status.Enablements {
							e := &p.status.Enablements[i]
							if sk, ok := a.data.skill(e.Skill); ok {
								a.placementRow(c, p, sk, e, a.skillLead(c, sk))
							}
						}
					})
				})
			}
			if len(p.status.Broken) > 0 {
				a.brokenLinks(c, p)
			}
			if len(p.status.Others) > 0 {
				unmanaged(c, p.status.Others)
			}
		})
	}).Background(t.Background)
}

// skillLead is a placement row's lead on Projects: the skill, which opens
// it on Skills.
func (a *app) skillLead(c *ui.Context, sk core.Skill) func() {
	return func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Button(c, "").Padding(0).Background(ui.Transparent).BorderWidth(0).AlignSelf(ui.Start).Tooltip("Show in Skills").Children(func() {
				ui.Text(c, a.data.skillName(sk)).Bold().SingleLine()
			}).OnClick(func() { a.show(sk.Path) })
			if sk.Description != "" {
				muted(c, sk.Description).FontSize(12).SingleLine()
			}
		})
	}
}

// brokenLinks lists the links into the library whose skill is gone.
func (a *app) brokenLinks(c *ui.Context, p *place) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		section(c, "Broken links")
		ui.Column(c).Radius(10).Border(1, t.Border).Dividers(1, t.Border).Children(func() {
			for _, b := range p.status.Broken {
				ui.Row(c.Key(b.Path)).Gap(10).AlignItems(ui.Center).Padding(8, 12).Children(func() {
					ui.Icon(c, iconWarning).TextColor(t.Danger)
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, b.Name).Bold().SingleLine()
						muted(c, tilde(b.Path)+" → "+b.Skill+", missing from the library").FontSize(12).SingleLine()
					})
					if a.working(c, "broken:"+b.Path) {
						ui.Spinner(c).Label("Working")
						return
					}
					ui.Button(c, "Remove").Padding(3, 10).OnClick(func() { a.removeBroken(p.scope, b) })
				})
			}
		})
	})
}

// unmanaged lists the entries myskills didn't make, which it never touches.
func unmanaged(c *ui.Context, others []core.Placement) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		section(c, "Not managed by myskills")
		ui.Column(c).Radius(10).Border(1, t.Border).Dividers(1, t.Border).Children(func() {
			for _, o := range others {
				ui.Column(c.Key(o.Path)).Gap(1).Padding(8, 12).Children(func() {
					ui.Text(c, o.Name).SingleLine()
					muted(c, tilde(o.Path)).FontSize(12).SingleLine().Selectable()
				})
			}
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
	t := c.Theme()
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
		ui.Column(c).Width(480).Gap(12).Children(func() {
			ui.Text(c, "Add Skills to "+placeName(d.scope)).FontSize(15).Bold()
			ui.SearchField(c, &d.query).Placeholder("Search Skills").AutoFocus()
			ui.Scroll(c).Height(300).Radius(8).Border(1, t.Border).Children(func() {
				ui.Column(c).Padding(6).Gap(2).Children(func() {
					if len(rows) == 0 {
						muted(c, "No skills to add.").Padding(16).TextAlign(ui.Center)
					}
					for _, sk := range rows {
						on := d.picks[sk.Path]
						ui.Column(c.Key(sk.Path)).Padding(6, 8).Gap(2).Children(func() {
							if ui.Checkbox(c, &on, a.data.skillName(sk)).Changed() {
								d.picks[sk.Path] = on
							}
							if sk.Description != "" {
								muted(c, sk.Description).FontSize(12).Margin(0, 0, 0, 26).MaxLines(2)
							}
						})
					}
				})
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Segmented(c, &d.mode, "Link", "Copy").Label("Mode")
				muted(c, modeHint(d.mode)).FontSize(12).Grow(1)
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					d.open = false
				}
				label := "Enable"
				if picked > 0 {
					label = fmt.Sprintf("Enable %d", picked)
				}
				if ui.PrimaryButton(c, label).Disabled(picked == 0).Clicked() {
					d.open = false
					a.enableMany(d.scope, d.picks, []core.Mode{core.Link, core.Copy}[d.mode])
				}
			})
		})
	})
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
