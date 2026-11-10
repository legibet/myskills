package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/source"
)

const (
	searchLimit    = 50
	searchDebounce = 250 * time.Millisecond
)

// The busy keys of Find's own work: the search or the fetch of the query,
// and the load of the preview shown.
const (
	findSearch  = "find:search"
	findFetch   = "find:fetch"
	findPreview = "find:preview"
)

// findPage is the state of the Find page: a skills.sh search, or the
// candidates of a fetched source.
type findPage struct {
	query string
	split float32
	list  ui.ListState
	sel   int
	key   string // the row chosen, kept as the rows change
	md    markdown.Options

	seq    int                // id of the query's latest search or fetch; older results are dropped
	cancel context.CancelFunc // cancels that search or fetch
	due    time.Time          // when the search waiting for typing to pause starts; zero when none waits

	results  []skillssh.Result
	searched string // the query the results answer
	err      error  // why the search failed

	browsing bool          // a fetch was asked for the query: in flight, done or failed
	spec     source.Spec   // the source fetched
	fetched  *core.Fetched // its checkout, while the page shows it
	fetchErr error

	docs          map[string]preview // SKILL.md by row key
	previewSeq    int
	previewing    string // the row whose remote preview loads
	previewCancel context.CancelFunc

	added map[string]string // row key → library path of a skill added here

	mu   sync.Mutex
	uses map[*core.Fetched]int // open checkouts and their holders: the page while it shows one, each add from it
}

// preview is the SKILL.md of a row, or why it couldn't be read.
type preview struct {
	doc
	err error
}

// findRow is a row of the list: a search result or a candidate of the
// fetched source.
type findRow struct {
	res  *skillssh.Result
	cand *core.Candidate
}

func (r findRow) key() string {
	if r.res != nil {
		return "sh:" + r.res.Source + "/" + r.res.SkillID
	}
	return "cand:" + r.cand.Dir
}

func (r findRow) name() string {
	if r.res != nil {
		return cmp.Or(r.res.Name, r.res.SkillID)
	}
	return r.cand.Name
}

// The states a row shows in place of Add.
const (
	stateAdded     = "Added"
	stateInLibrary = "In Library"
	stateConflict  = "Conflict"
	stateInvalid   = "Invalid"
)

// hold takes a hold on a checkout; it is closed when the last is released.
func (p *findPage) hold(f *core.Fetched) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.uses == nil {
		p.uses = map[*core.Fetched]int{}
	}
	p.uses[f]++
}

func (p *findPage) release(f *core.Fetched) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.uses[f]--; p.uses[f] <= 0 {
		delete(p.uses, f)
		_ = f.Close()
	}
}

// closeAll closes every checkout left open: the app is quitting, and the
// work that held them is done.
func (p *findPage) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for f := range p.uses {
		_ = f.Close()
	}
	clear(p.uses)
}

// drop releases the page's hold on a checkout off the main thread, since
// closing it deletes its files.
func (a *app) drop(f *core.Fetched) {
	a.work.Go(func() {
		a.find.release(f)
	})
}

// findRows returns what the query gives: the candidates of the source
// fetched, none for a source not fetched yet, or the search results.
func (a *app) findRows() []findRow {
	p := &a.find
	var rows []findRow
	switch {
	case p.browsing:
		if p.fetched != nil {
			for i := range p.fetched.Candidates {
				rows = append(rows, findRow{cand: &p.fetched.Candidates[i]})
			}
		}
	case !source.IsExplicit(strings.TrimSpace(p.query)):
		for i := range p.results {
			rows = append(rows, findRow{res: &p.results[i]})
		}
	}
	return rows
}

// stale reports whether the results answer an earlier query: they show
// until the new ones arrive, and can't be added meanwhile.
func (a *app) stale() bool {
	return a.find.searched != strings.TrimSpace(a.find.query)
}

func (a *app) findView(c *ui.Context) {
	p := &a.find
	if p.docs == nil {
		p.docs, p.added = map[string]preview{}, map[string]string{}
	}
	if !p.due.IsZero() {
		if wait := p.due.Sub(c.Now()); wait > 0 {
			c.After(wait)
		} else {
			a.search()
		}
	}
	rows := a.findRows()
	p.sel = max(0, slices.IndexFunc(rows, func(r findRow) bool { return r.key() == p.key }))
	if len(rows) > 0 {
		p.key = rows[p.sel].key()
		if r := rows[p.sel]; r.res != nil {
			if _, ok := p.docs[p.key]; !ok && p.previewing != p.key {
				a.loadPreview(*r.res)
			}
		}
	}
	ui.Split(c, &p.split, func() {
		ui.Column(c).Fill().Children(func() {
			bar(c).Padding(0, 10, 0, 16).Children(func() {
				paneTitle(c, "Find", a.findDetailLine(rows))
				if a.busy[findSearch] {
					spinner(c, "Searching").Margin(0, 7)
				}
				toolButton(c, iconFolder, "Choose Folder…").OnClick(a.chooseSource)
			})
			ui.Column(c).Padding(0, 12, 10, 12).Gap(8).Children(func() {
				field, changed := searchField(c, &p.query, "Search or enter a source", &a.searchField)
				field.Label("Search skills.sh, or enter owner/repo, a git URL or a folder").AutoFocus()
				if changed {
					a.queryChanged(c.Now())
				}
				if field.Submitted() {
					a.submitQuery()
				}
				a.sourceBar(c)
			})
			s := &p.list
			s.Selected = &p.sel
			s.Key = func(i int) any { return rows[i].key() }
			s.Label = func(i int) string { return rows[i].name() }
			list := sourceList(c, s, len(rows), "Skills found", func(i int, onAccent bool) { a.findRowView(c, rows[i], onAccent) })
			if !p.browsing && a.stale() {
				list.Opacity(0.5)
			}
			if list.Changed() && p.sel >= 0 && p.sel < len(rows) {
				p.key = rows[p.sel].key()
			}
		})
	}, func() {
		switch i := slices.IndexFunc(rows, func(r findRow) bool { return r.key() == p.key }); {
		case i >= 0:
			a.findDetail(c, rows[i])
		case len(rows) == 0:
			a.findEmpty(c)
		}
	}).Grow(1)
}

// findDetailLine is the line under the Find title: where the rows are from.
func (a *app) findDetailLine(rows []findRow) string {
	p := &a.find
	switch {
	case p.browsing:
		return sourceName(p.spec)
	case len(rows) > 0:
		return fmt.Sprintf("%d %s on skills.sh", len(rows), plural(len(rows), "result", "results"))
	}
	return "skills.sh, git or a folder"
}

// queryChanged leaves what the last query showed, and schedules the search
// of a search term: an explicit source waits for Fetch, a clone.
func (a *app) queryChanged(now time.Time) {
	p := &a.find
	a.leaveQuery()
	q := strings.TrimSpace(p.query)
	switch {
	case source.IsExplicit(q):
	case utf8.RuneCountInString(q) < 2:
		p.results, p.searched = nil, ""
	default:
		p.due = now.Add(searchDebounce)
	}
}

// leaveQuery stops the search or fetch in flight and releases the checkout
// shown. The results stay, dimmed, until the next ones arrive.
func (a *app) leaveQuery() {
	p := &a.find
	a.stopFind()
	p.seq++
	p.due, p.err = time.Time{}, nil
	if p.fetched != nil {
		a.drop(p.fetched)
	}
	p.browsing, p.fetched, p.fetchErr = false, nil, nil
}

// stopFind cancels the search or fetch in flight.
func (a *app) stopFind() {
	p := &a.find
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	delete(a.busy, findSearch)
	delete(a.busy, findFetch)
}

// submitQuery is Return in the field: it fetches the source typed, retries
// a failed fetch, or searches without waiting.
func (a *app) submitQuery() {
	p := &a.find
	q := strings.TrimSpace(p.query)
	switch {
	case p.browsing:
		if p.fetchErr != nil {
			a.fetch(p.spec)
		}
	case source.IsExplicit(q):
		if spec, err := source.Parse(q); err == nil {
			a.fetch(spec)
		}
	case utf8.RuneCountInString(q) >= 2 && !a.busy[findSearch] && (a.stale() || p.err != nil):
		a.search()
	}
}

// search searches skills.sh for the query.
func (a *app) search() {
	p := &a.find
	a.stopFind()
	p.seq++
	p.due, p.err = time.Time{}, nil
	seq, q := p.seq, strings.TrimSpace(p.query)
	ctx, cancel := context.WithCancel(a.ctx)
	p.cancel = cancel
	a.busy[findSearch] = true
	a.work.Go(func() {
		res, err := a.svc.SkillsSH.Search(ctx, q, searchLimit)
		a.post(func() { a.searched(seq, q, res, err) })
	})
}

// searched shows the results of a search, unless a newer query replaced it.
func (a *app) searched(seq int, q string, res []skillssh.Result, err error) {
	p := &a.find
	if seq != p.seq {
		return
	}
	a.stopFind()
	p.results, p.searched, p.err, p.key = res, q, err, ""
}

// fetch clones a git source or reads a folder, to list its skills.
func (a *app) fetch(spec source.Spec) {
	p := &a.find
	a.stopFind()
	p.seq++
	p.due = time.Time{}
	p.browsing, p.spec, p.fetchErr = true, spec, nil
	seq := p.seq
	ctx, cancel := context.WithCancel(a.ctx)
	p.cancel = cancel
	a.busy[findFetch] = true
	a.work.Go(func() {
		f, err := a.svc.Fetch(ctx, spec)
		docs := map[string]preview{}
		if err == nil {
			p.hold(f) // the page's, until the query changes
			for _, c := range f.Candidates {
				var pv preview
				pv.meta, _ = skill.ReadFile(filepath.Join(c.Dir, skill.FileName))
				pv.body, pv.err = skill.Body(c.Dir)
				docs[findRow{cand: &c}.key()] = pv
			}
		}
		a.post(func() { a.fetchDone(seq, f, docs, err) })
	})
}

// fetchDone shows the candidates of a fetched source, unless the query
// changed meanwhile.
func (a *app) fetchDone(seq int, f *core.Fetched, docs map[string]preview, err error) {
	p := &a.find
	if seq != p.seq {
		if f != nil {
			a.drop(f)
		}
		return
	}
	a.stopFind()
	p.fetched, p.fetchErr, p.key = f, err, ""
	maps.Copy(p.docs, docs)
}

// chooseSource asks for a folder and fetches it. The path names a source
// even when it has spaces, which a typed query doesn't (source.IsExplicit).
func (a *app) chooseSource() {
	a.chooseFolder("Choose a skill, or a folder of skills", func(dir string) {
		spec, err := source.Parse(dir)
		if err != nil {
			a.fail("Can't read "+tilde(dir), err)
			return
		}
		a.find.query = dir
		a.leaveQuery()
		a.fetch(spec)
	})
}

// loadPreview loads the SKILL.md of a search result from skills.sh. Only
// the row chosen loads: choosing another cancels it.
func (a *app) loadPreview(res skillssh.Result) {
	p := &a.find
	if p.previewCancel != nil {
		p.previewCancel()
	}
	p.previewSeq++
	seq, key := p.previewSeq, findRow{res: &res}.key()
	ctx, cancel := context.WithCancel(a.ctx)
	p.previewing, p.previewCancel = key, cancel
	a.busy[findPreview] = true
	a.work.Go(func() {
		md, err := a.svc.SkillsSH.SkillMarkdown(ctx, res.Source, res.SkillID)
		canceled := ctx.Err() != nil
		a.post(func() {
			if !canceled {
				pv := preview{err: err}
				if err == nil {
					pv.meta, _ = skill.Parse([]byte(md))
					pv.body = skill.StripFrontmatter(md)
				}
				p.docs[key] = pv
			}
			if seq == p.previewSeq {
				cancel()
				p.previewing, p.previewCancel = "", nil
				delete(a.busy, findPreview)
			}
		})
	})
}

// rowState is what a row shows in place of Add, "" while it can be added,
// and the library skill it is for Added and In Library.
func (a *app) rowState(r findRow) (state, path string) {
	if path := a.find.added[r.key()]; path != "" {
		// Until the read after the add completes, the library lacks it.
		if _, ok := a.data.skill(path); ok || a.loading {
			return stateAdded, path
		}
	}
	if r.res != nil {
		if spec, err := source.Parse(r.res.Source); err == nil && spec.Kind == source.Git {
			if sk, ok := core.Installed(a.data.lib, spec.URL, r.res.SkillID); ok {
				return stateInLibrary, sk.Path
			}
		}
		// The library keeps a skill under its name: one from another source
		// has it already (docs/library.md).
		if _, ok := a.data.skill(r.res.SkillID); ok {
			return stateConflict, r.res.SkillID
		}
		return "", ""
	}
	// What the candidate says about the library was true when it was
	// fetched; it holds while the library still has those skills.
	c := r.cand
	if _, ok := a.data.skill(c.Installed); ok && c.Installed != "" {
		return stateInLibrary, c.Installed
	}
	for _, sk := range a.data.lib {
		if sk.Linked && filepath.Join(sk.LinkTarget, strings.TrimPrefix(sk.Path, sk.Entry)) == c.Dir {
			return stateInLibrary, sk.Path
		}
	}
	if _, ok := a.data.skill(c.Conflict); ok && c.Conflict != "" {
		return stateConflict, c.Conflict
	}
	if c.Issue != "" {
		return stateInvalid, ""
	}
	return "", ""
}

// add adds a row's skill to the library. It asks nothing and never enables.
func (a *app) add(r findRow) {
	if r.res != nil {
		a.addResult(*r.res)
	} else {
		a.addCandidate(a.find.fetched, *r.cand)
	}
}

// addResult adds a skills.sh result through git, as `add owner/repo@skill`
// does (docs/library.md).
func (a *app) addResult(res skillssh.Result) {
	key := findRow{res: &res}.key()
	var path string
	a.start(op{key: key, fail: "Couldn't add " + res.SkillID, run: func(bool) outcome {
		spec, err := source.Parse(res.Source + "@" + res.SkillID)
		if err != nil {
			return outcome{err: err}
		}
		f, err := a.svc.Fetch(a.ctx, spec)
		if err != nil {
			return outcome{err: err}
		}
		defer func() { _ = f.Close() }()
		var out outcome
		out, path = installOutcome(a.svc.Install(a.ctx, f, f.Candidates)[0])
		return out
	}, then: func() { a.markAdded(key, path) }}, false)
}

// addCandidate adds a candidate of the fetched source. The add holds the
// checkout, which outlives the query until the add is done. What the
// candidate says about the library may be stale, so core decides whether it
// is in the library already or conflicts.
func (a *app) addCandidate(f *core.Fetched, c core.Candidate) {
	key := findRow{cand: &c}.key()
	if a.busy[key] {
		return
	}
	a.find.hold(f)
	var path string
	a.start(op{key: key, fail: "Couldn't add " + c.Name, run: func(bool) outcome {
		defer a.find.release(f)
		var out outcome
		out, path = installOutcome(a.svc.Install(a.ctx, f, []core.Candidate{c})[0])
		return out
	}, then: func() { a.markAdded(key, path) }}, false)
}

func (a *app) markAdded(key, path string) {
	if path != "" {
		a.find.added[key] = path
	}
}

// installOutcome is what adding one skill did, and its library path when it
// was added.
func installOutcome(r core.InstallResult) (outcome, string) {
	switch {
	case r.Err != nil:
		return outcome{err: r.Err}, ""
	case r.Already:
		return outcome{note: r.Candidate.Name + " is already in your library as " + r.Path}, ""
	}
	return outcome{}, r.Path
}

// linkFolder adds the fetched folder to the library as a link (docs/library.md).
func (a *app) linkFolder(dir string) {
	a.start(op{key: "link:" + dir, fail: "Couldn't link " + tilde(dir), run: func(bool) outcome {
		_, err := a.svc.LinkLocal(dir)
		return outcome{err: err}
	}}, false)
}

// sourceName names a source: owner/repo for GitHub, the URL for other
// remotes, the folder's name for a local source.
func sourceName(spec source.Spec) string {
	if spec.Kind == source.Local {
		return filepath.Base(spec.Path)
	}
	name := source.Label(spec.URL)
	if spec.Subdir != "" {
		name += "/" + spec.Subdir
	}
	return name
}

// sourceBar names the source the query gives, under the field: Fetch before
// it is fetched; its skills after, and Link Folder for a folder.
func (a *app) sourceBar(c *ui.Context) {
	p := &a.find
	spec := p.spec
	if !p.browsing {
		q := strings.TrimSpace(p.query)
		if !source.IsExplicit(q) {
			return
		}
		spec, _ = source.Parse(q)
	}
	pal := colors(c)
	local := spec.Kind == source.Local
	icon, sub := iconGlobal, spec.URL
	if local {
		icon, sub = iconFolder, tilde(spec.Path)
	}
	switch {
	case p.fetched != nil:
		n := len(p.fetched.Candidates)
		sub = fmt.Sprintf("%d %s", n, plural(n, "skill", "skills"))
		if local {
			sub += " · " + tilde(spec.Path)
		}
	case p.fetchErr != nil:
		sub = "Couldn't fetch"
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8, 8, 8, 12).Radius(8).Background(pal.card).Border(1, pal.separator).Children(func() {
		ui.Icon(c, icon).Size(15, 15).Shrink(0).TextColor(pal.text2)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, sourceName(spec)).FontSize(textSmall(c)).FontWeight(600).SingleLine()
			ui.Text(c, sub).FontSize(textCaption(c)).TextColor(pal.text2).SingleLine()
		})
		key := "link:" + spec.Path
		switch {
		case a.busy[findFetch], a.working(c, key):
			spinner(c, "Working")
		case !p.browsing:
			button(c, "Fetch", primary).MinHeight(24).Padding(3, 10).FontSize(textSmall(c)).OnClick(func() { a.fetch(spec) })
		case p.fetched == nil || !local:
		case slices.ContainsFunc(a.data.lib, func(sk core.Skill) bool { return sk.Linked && sk.LinkTarget == spec.Path }):
			tag(c, "Linked", pal.green)
		default:
			iconTextButton(c, iconLink, "Link Folder", plain).MinHeight(24).Padding(3, 10).FontSize(textSmall(c)).
				Tooltip("Add the folder to the library as a link: its skills stay where they are").OnClick(func() { a.linkFolder(spec.Path) })
		}
	})
}

// findRowView is a row of the list: the name, where it comes from (what it
// does, for a candidate, whose source the bar names), and Add or its state.
func (a *app) findRowView(c *ui.Context, r findRow, onAccent bool) {
	from := ""
	if r.res != nil {
		from = r.res.Source + " · " + installs(r.res.Installs)
	} else {
		from = cmp.Or(r.cand.Description, r.cand.RelPath, sourceName(a.find.spec))
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			listRow(c, onAccent, func() {
				ui.Text(c, r.name()).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			}, from)
		})
		ui.Row(c).Shrink(0).Padding(0, 10, 0, 0).Children(func() {
			if a.working(c, r.key()) {
				spinner(c, "Adding")
				return
			}
			switch state, _ := a.rowState(r); state {
			case "":
				button(c, "Add", plain).MinHeight(22).Padding(2, 10).FontSize(textSmall(c)).Label("Add " + r.name()).
					Disabled(r.res != nil && a.stale()).OnClick(func() { a.add(r) })
			default:
				rowTag(c, state, stateColor(c, state), onAccent)
			}
		})
	})
}

func stateColor(c *ui.Context, state string) ui.Color {
	if state == stateAdded || state == stateInLibrary {
		return colors(c).green
	}
	return colors(c).red
}

// installs writes a skills.sh install count as the TUI does: "12.3K
// installs".
func installs(n int) string {
	var s string
	switch {
	case n < 1000:
		s = fmt.Sprint(n)
	case n < 1_000_000:
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "K"
	default:
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	}
	return s + " " + plural(n, "install", "installs")
}

// findDetail is the row chosen: what it is, where it comes from, Add or its
// state, and its SKILL.md.
func (a *app) findDetail(c *ui.Context, r findRow) {
	p := &a.find
	pal := colors(c)
	key := r.key()
	pv, loaded := p.docs[key]
	desc := pv.meta.Description
	if r.cand != nil {
		desc = r.cand.Description
	}
	state, path := a.rowState(r)
	ui.Column(c).Fill().Children(func() {
		detailBar(c, func() {
			switch {
			case a.working(c, key):
				spinner(c, "Adding")
			case state == "":
				button(c, "Add to Library", primary).Disabled(r.res != nil && a.stale()).OnClick(func() { a.add(r) })
			default:
				tag(c, state, stateColor(c, state))
				if _, ok := a.data.skill(path); ok && state != stateConflict {
					button(c, "Show in Skills", plain).OnClick(func() { a.show(path) })
				}
			}
		})
		detailPane(c, key, func() {
			detailHeader(c, r.name(), desc, func() { a.origin(c, r) })
			switch state {
			case stateConflict:
				note(c, "The library already has "+path+" from another source under this name.")
			case stateInvalid:
				note(c, "This skill can't be added: "+r.cand.Issue)
			}
			switch {
			case !loaded:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					spinner(c, "Loading")
					ui.Text(c, "Loading SKILL.md…").FontSize(textSmall(c)).TextColor(pal.text2)
				})
			case pv.err != nil:
				note(c, "Couldn't load SKILL.md: "+pv.err.Error())
			default:
				docView(c, pv.meta.License, pv.meta.Compatibility, pv.meta.AllowedTools, pv.meta.Metadata, pv.body, &p.md)
			}
		})
	})
}

// origin is where a row's skill comes from: its repository, a link to its
// page when there is one, or its folder.
func (a *app) origin(c *ui.Context, r findRow) {
	var label, url, extra string
	if r.res != nil {
		label, extra = r.res.Source, installs(r.res.Installs)
		if spec, err := source.Parse(r.res.Source); err == nil {
			url = source.WebURL(spec.URL, "", "")
		}
	} else {
		spec := a.find.spec
		label = sourceName(spec)
		if spec.Kind == source.Git {
			url = source.WebURL(spec.URL, spec.Ref, r.cand.RelPath)
			extra = r.cand.RelPath
		} else {
			label = tilde(r.cand.Dir)
		}
	}
	if url != "" {
		webLink(c, label, url)
	} else {
		metaText(c, label)
	}
	if extra != "" {
		dot(c)
		metaText(c, extra)
	}
}

// findEmpty fills the preview pane while the list is empty: what to type,
// the work in flight, or why nothing shows.
func (a *app) findEmpty(c *ui.Context) {
	p := &a.find
	q := strings.TrimSpace(p.query)
	ui.Column(c).Fill().Children(func() {
		bar(c)
		switch {
		case p.fetchErr != nil:
			emptyState(c, "Couldn't fetch "+sourceName(p.spec), p.fetchErr.Error(), func() {
				button(c, "Retry", plain).OnClick(func() { a.fetch(p.spec) })
			})
		case p.err != nil:
			emptyState(c, "Search failed", p.err.Error(), func() {
				button(c, "Retry", plain).OnClick(a.search)
			})
		case a.busy[findFetch], a.busy[findSearch], !p.due.IsZero():
			label := "Searching skills.sh…"
			if a.busy[findFetch] {
				label = "Fetching " + sourceName(p.spec) + "…"
			}
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				spinner(c, "Working")
				ui.Text(c, label).FontSize(textSmall(c)).TextColor(colors(c).text2)
			})
		case source.IsExplicit(q):
			emptyState(c, "Fetch to see its skills", "Nothing is added to your library until you choose.", nil)
		case utf8.RuneCountInString(q) < 2:
			emptyState(c, "Find skills", "Search skills.sh by name or topic, or enter owner/repo, a git URL or a folder path to see the skills it has.", nil)
		default:
			emptyState(c, "No skills found", "Nothing on skills.sh matches “"+q+"”.", nil)
		}
	})
}
