package tui

import (
	"cmp"
	"context"
	"errors"
	"image/color"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/source"
)

const (
	searchLimit    = 50
	searchDebounce = 250 * time.Millisecond
	previewSettle  = 200 * time.Millisecond
)

// findState is the Find view: a skills.sh search, or the candidates of a
// fetched source.
type findState struct {
	input  textinput.Model
	typing bool // the input has focus; otherwise the list has

	seq     int                // id of the latest search; older responses are dropped
	cancel  context.CancelFunc // cancels the search in flight
	loading bool
	results []skillssh.Result
	query   string // query of results
	err     error

	fetchSeq    int
	fetchCancel context.CancelFunc
	fetching    bool
	fetched     *core.Fetched // the browsed source, closed when left
	fetchErr    error
	installs    map[*core.Fetched]int // installs in flight per checkout

	inLib     map[string]string // result key → library path, for results already installed
	added     map[string]string // row key → library path of a skill added here, while it is in the library
	list      listCursor
	settleSeq int
	settled   string // key of the row whose remote preview may load
}

func newFindState() findState {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "search skills.sh, or paste owner/repo, a URL or a path"
	return findState{
		input:    in,
		installs: map[*core.Fetched]int{},
		inLib:    map[string]string{},
		added:    map[string]string{},
	}
}

// findRow is a row of the Find list: a search result, or a candidate of the
// browsed source.
type findRow struct {
	res  *skillssh.Result
	cand *core.Candidate
}

func (r findRow) key() string {
	switch {
	case r.res != nil:
		return "sh:" + r.res.Source + "/" + r.res.SkillID
	case r.cand != nil:
		return "cand:" + r.cand.Dir
	}
	return ""
}

func (a *app) findRows() []findRow {
	if f := a.fd.fetched; f != nil {
		rows := make([]findRow, len(f.Candidates))
		for i := range f.Candidates {
			rows[i] = findRow{cand: &f.Candidates[i]}
		}
		return rows
	}
	if source.IsExplicit(strings.TrimSpace(a.fd.input.Value())) {
		return nil // until enter fetches it
	}
	rows := make([]findRow, len(a.fd.results))
	for i := range a.fd.results {
		rows[i] = findRow{res: &a.fd.results[i]}
	}
	return rows
}

func (a *app) findCurrent() (findRow, bool) {
	rows := a.findRows()
	if c := a.fd.list.cursor; c >= 0 && c < len(rows) {
		return rows[c], true
	}
	return findRow{}, false
}

// inBrowse reports whether the list shows (or is fetching) a source.
func (a *app) inBrowse() bool {
	return a.fd.fetched != nil || a.fd.fetching || a.fd.fetchErr != nil
}

// enterFind shows Find with the query input focused, or, when it opens with
// a query, submits it as enter would.
func (a *app) enterFind(initial bool) tea.Cmd {
	a.view = Find
	a.help.ShowAll = false
	if initial && strings.TrimSpace(a.fd.input.Value()) != "" {
		return tea.Batch(a.queryChanged(true), a.submitQuery())
	}
	return a.typeQuery()
}

func (a *app) leaveFind() tea.Cmd {
	a.view = Skills
	a.help.ShowAll = false
	a.focusResults()
	a.revealAdded()
	return nil
}

// typeQuery gives the query input the focus.
func (a *app) typeQuery() tea.Cmd {
	a.fd.typing = true
	return a.fd.input.Focus()
}

// focusResults gives the list the focus.
func (a *app) focusResults() {
	a.fd.typing = false
	a.fd.input.Blur()
}

func (a *app) syncFindKeys() {
	q, k := &a.qkeys, &a.nkeys
	r, ok := a.findCurrent()
	query := strings.TrimSpace(a.fd.input.Value())
	explicit := source.IsExplicit(query)

	q.Submit.SetEnabled(explicit || utf8.RuneCountInString(query) >= 2)
	switch {
	case explicit && (a.fd.fetched != nil || a.fd.fetching):
		q.Submit.SetHelp("enter", "results")
	case explicit:
		q.Submit.SetHelp("enter", "fetch")
	default:
		q.Submit.SetHelp("enter", "search")
	}
	q.Results.SetEnabled(len(a.findRows()) > 0)

	k.Read.SetEnabled(ok)
	k.Add.SetEnabled(ok && !a.busy[r.key()] && (r.res == nil || a.resultsCurrent()))
	k.Retry.SetEnabled(a.fd.err != nil || a.fd.fetchErr != nil)
	k.Open.SetEnabled(ok && (r.cand == nil || a.fd.fetched.Spec.Kind == source.Git))
}

// findKey handles a key in Find: the query input takes every key but its
// own while it has the focus.
func (a *app) findKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.fd.typing {
		return a.queryKey(msg)
	}
	k := a.nkeys
	n := len(a.findRows())
	page := max(1, a.layout().bodyH-3)
	if a.help.ShowAll && msg.String() == "esc" {
		a.help.ShowAll = false
		return nil
	}
	switch {
	case key.Matches(msg, k.Quit):
		return a.quit()
	case key.Matches(msg, k.Help):
		a.help.ShowAll = !a.help.ShowAll
	case key.Matches(msg, k.Back), key.Matches(msg, k.Search):
		return a.typeQuery()
	case key.Matches(msg, k.Up):
		if a.fd.list.cursor == 0 {
			return a.typeQuery()
		}
		a.fd.list.move(-1, n)
		return a.settle()
	case key.Matches(msg, k.Down):
		a.fd.list.move(1, n)
		return a.settle()
	case key.Matches(msg, k.Top):
		a.fd.list.cursor = 0
		return a.settle()
	case key.Matches(msg, k.Bottom):
		a.fd.list.cursor = n - 1
		return a.settle()
	case key.Matches(msg, k.PageUp):
		a.fd.list.move(-page, n)
		return a.settle()
	case key.Matches(msg, k.PageDown):
		a.fd.list.move(page, n)
		return a.settle()
	case key.Matches(msg, k.Read):
		a.openReader()
	case key.Matches(msg, k.Add):
		return a.confirmAdd()
	case key.Matches(msg, k.Retry):
		return a.retry()
	case key.Matches(msg, k.Open):
		return a.openSource()
	case key.Matches(msg, k.ScrollDown):
		a.pv.vp.HalfPageDown()
	case key.Matches(msg, k.ScrollUp):
		a.pv.vp.HalfPageUp()
	}
	return nil
}

func (a *app) queryKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.qkeys
	switch {
	case key.Matches(msg, k.Submit):
		return a.submitQuery()
	case key.Matches(msg, k.Results):
		a.focusResults()
		return nil
	case key.Matches(msg, k.Back):
		return a.leaveFind()
	}
	return a.findInput(msg)
}

// submitQuery is enter in the query input: it fetches the source typed,
// unless it is fetched already, or searches without waiting (again, after a
// failure), and focuses the list.
func (a *app) submitQuery() tea.Cmd {
	q := strings.TrimSpace(a.fd.input.Value())
	explicit := source.IsExplicit(q)
	if !explicit && utf8.RuneCountInString(q) < 2 {
		return nil
	}
	a.focusResults()
	switch {
	case a.fd.err != nil, a.fd.fetchErr != nil:
		return a.retry()
	case explicit && !a.inBrowse():
		return a.browse(q)
	case !explicit && a.fd.loading && a.fd.cancel == nil: // waiting for the debounce
		a.fd.seq++
		return a.search()
	}
	return nil
}

func (a *app) findInput(msg tea.Msg) tea.Cmd {
	before := a.fd.input.Value()
	var cmd tea.Cmd
	a.fd.input, cmd = a.fd.input.Update(msg)
	if a.fd.input.Value() == before {
		return cmd
	}
	return tea.Batch(cmd, a.queryChanged(false))
}

type (
	searchDebounceMsg struct{ seq int }
	searchMsg         struct {
		seq     int
		query   string
		results []skillssh.Result
		err     error
	}
	fetchMsg struct {
		seq int
		f   *core.Fetched
		err error
	}
	settleMsg struct{ seq int }
)

// queryChanged reacts to a new input value: it leaves a browsed source,
// cancels the search in flight and schedules the next one.
func (a *app) queryChanged(immediate bool) tea.Cmd {
	var cmds []tea.Cmd
	if a.inBrowse() {
		cmds = append(cmds, a.leaveBrowse())
	}
	a.fd.seq++
	if a.fd.cancel != nil {
		a.fd.cancel()
		a.fd.cancel = nil
	}
	a.fd.loading, a.fd.err = false, nil
	a.fd.list = listCursor{}
	q := strings.TrimSpace(a.fd.input.Value())
	switch {
	case source.IsExplicit(q):
	case utf8.RuneCountInString(q) < 2:
		a.fd.results, a.fd.query = nil, ""
	case immediate:
		a.fd.loading = true
		cmds = append(cmds, a.search())
	default:
		a.fd.loading = true
		seq := a.fd.seq
		cmds = append(cmds, tick(searchDebounce, func(time.Time) tea.Msg { return searchDebounceMsg{seq} }))
	}
	return tea.Batch(cmds...)
}

func (a *app) search() tea.Cmd {
	ctx, cancel := context.WithCancel(a.ctx)
	a.fd.cancel = cancel
	seq, q, svc := a.fd.seq, strings.TrimSpace(a.fd.input.Value()), a.svc
	return func() tea.Msg {
		res, err := svc.Search(ctx, q, searchLimit)
		return searchMsg{seq: seq, query: q, results: res, err: err}
	}
}

// settle lets the remote preview of the highlighted row load once the cursor
// has rested for a moment.
func (a *app) settle() tea.Cmd {
	a.fd.settleSeq++
	seq := a.fd.settleSeq
	return tick(previewSettle, func(time.Time) tea.Msg { return settleMsg{seq} })
}

func (a *app) updateFind(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		if msg.seq == a.fd.seq {
			return a.search()
		}
	case searchMsg:
		if msg.seq != a.fd.seq || errors.Is(msg.err, context.Canceled) {
			return nil
		}
		a.fd.loading = false
		if a.fd.cancel != nil {
			a.fd.cancel()
			a.fd.cancel = nil
		}
		if msg.err != nil {
			a.fd.err, a.fd.results, a.fd.query = msg.err, nil, msg.query
			return nil
		}
		a.fd.results, a.fd.query = msg.results, msg.query
		a.fd.list = listCursor{}
		a.refreshInLibrary()
		return a.settle()
	case fetchMsg:
		if msg.seq != a.fd.fetchSeq {
			if msg.f != nil {
				return a.closeFetched(msg.f)
			}
			return nil
		}
		a.fd.fetching = false
		if a.fd.fetchCancel != nil {
			a.fd.fetchCancel()
			a.fd.fetchCancel = nil
		}
		if msg.err != nil {
			a.fd.fetchErr = msg.err
			return nil
		}
		a.fd.fetched = msg.f
		a.fd.list = listCursor{}
	case settleMsg:
		if r, ok := a.findCurrent(); ok && msg.seq == a.fd.settleSeq {
			a.fd.settled = r.key()
		}
	}
	return nil
}

// resultsCurrent reports whether the search results answer the text in the
// input; until then they are shown dimmed and cannot be added.
func (a *app) resultsCurrent() bool {
	return !a.fd.loading && a.fd.query == strings.TrimSpace(a.fd.input.Value())
}

// inLibrary reports whether the loaded library has a skill at path.
func (a *app) inLibrary(path string) bool {
	return path != "" && slices.ContainsFunc(a.data.lib, func(sk core.Skill) bool { return sk.Path == path })
}

// liveCandidate drops what a fetched candidate says about the library when it
// no longer holds: the source was listed against the library of that moment.
func (a *app) liveCandidate(c core.Candidate) core.Candidate {
	if !a.inLibrary(c.Installed) {
		c.Installed = ""
	}
	if !a.inLibrary(c.Conflict) {
		c.Conflict = ""
	}
	return c
}

// refreshInLibrary marks the search results that are already installed, and
// forgets the skills added here that have left the library since.
func (a *app) refreshInLibrary() {
	maps.DeleteFunc(a.fd.added, func(_, path string) bool { return !a.inLibrary(path) })
	a.fd.inLib = map[string]string{}
	for _, r := range a.fd.results {
		spec, err := source.Parse(r.Source)
		if err != nil || spec.Kind != source.Git {
			continue
		}
		if sk, ok := core.Installed(a.data.lib, spec.URL, r.SkillID); ok {
			a.fd.inLib[findRow{res: &r}.key()] = sk.Path
		}
	}
}

// confirmAdd is space on a result or candidate: add it to the library once
// confirmed, since that downloads and keeps files from elsewhere.
func (a *app) confirmAdd() tea.Cmd {
	r, _ := a.findCurrent()
	key := r.key()
	var name, from string
	var add func() tea.Cmd
	switch {
	case r.res != nil:
		res := *r.res
		if p := a.fd.inLib[key]; p != "" {
			return a.setStatus(stInfo, sanitize.Line(res.SkillID)+" is already in your library as "+sanitize.Line(p))
		}
		name, from = res.SkillID, res.Source
		if spec, err := source.Parse(res.Source); err == nil {
			from = spec.URL
		}
		add = func() tea.Cmd { return a.addResult(res, key) }
	case r.cand != nil:
		c := a.liveCandidate(*r.cand)
		switch {
		case c.Issue != "":
			return a.setStatus(stWarn, "Can't add "+sanitize.Line(c.Name)+": "+c.Issue)
		case c.Installed != "":
			return a.setStatus(stInfo, sanitize.Line(c.Name)+" is already in your library as "+sanitize.Line(c.Installed))
		case c.Conflict != "":
			return a.setStatus(stWarn, "The library already has "+sanitize.Line(c.Conflict)+" from another source under this name")
		}
		name, from = c.Name, tildePath(c.Dir)
		if spec := a.fd.fetched.Spec; spec.Kind == source.Git {
			from = spec.URL
			if c.RelPath != "" {
				from += " · " + c.RelPath
			}
		}
		add = func() tea.Cmd { return a.installCandidate(*r.cand, key) }
	default:
		return nil
	}
	a.modal = confirmModal("Add "+sanitize.Line(name)+" to the library?", "From "+sanitize.Line(from), "Add", false, add)
	return nil
}

// browse fetches an explicit source and lists its candidates.
func (a *app) browse(src string) tea.Cmd {
	spec, err := source.Parse(src)
	if err != nil {
		return a.setStatus(stError, err.Error())
	}
	a.fd.fetchSeq++
	ctx, cancel := context.WithCancel(a.ctx)
	a.fd.fetchCancel = cancel
	a.fd.fetching, a.fd.fetchErr = true, nil
	seq, svc, co := a.fd.fetchSeq, a.svc, a.checkouts
	return func() tea.Msg {
		f, err := svc.Fetch(ctx, spec)
		if err != nil {
			return fetchMsg{seq: seq, err: err}
		}
		co.add(f)
		if ctx.Err() != nil { // left, or quit, while it was fetched
			co.close(f)
			return nil
		}
		return fetchMsg{seq: seq, f: f}
	}
}

// leaveBrowse drops the browsed source, when the query changes, and releases
// its checkout once no install uses it.
func (a *app) leaveBrowse() tea.Cmd {
	if a.fd.fetchCancel != nil {
		a.fd.fetchCancel()
		a.fd.fetchCancel = nil
	}
	a.fd.fetchSeq++
	f := a.fd.fetched
	a.fd.fetched, a.fd.fetching, a.fd.fetchErr = nil, false, nil
	a.fd.list = listCursor{}
	if f != nil && a.fd.installs[f] == 0 {
		return a.closeFetched(f)
	}
	return nil
}

// closeFetched closes a checkout off the UI goroutine.
func (a *app) closeFetched(f *core.Fetched) tea.Cmd {
	co := a.checkouts
	return func() tea.Msg {
		co.close(f)
		return nil
	}
}

// checkouts tracks the checkouts of browsed sources from the moment Fetch
// returns until they are closed, so that shutdown closes those whose fetch
// result or close command never ran.
type checkouts struct {
	mu     sync.Mutex
	open   map[*core.Fetched]bool
	closed bool // shutdown has run: checkouts fetched later are closed at once
}

func (c *checkouts) add(f *core.Fetched) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = f.Close()
		return
	}
	c.open[f] = true
}

func (c *checkouts) close(f *core.Fetched) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.open, f)
	_ = f.Close()
}

// closeAll closes every open checkout, and those added after.
func (c *checkouts) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for f := range c.open {
		_ = f.Close()
	}
	clear(c.open)
}

// addResult adds a skills.sh result to the library through git.
func (a *app) addResult(res skillssh.Result, key string) tea.Cmd {
	if p := a.fd.inLib[key]; p != "" {
		return a.setStatus(stInfo, sanitize.Line(res.SkillID)+" is already in your library as "+sanitize.Line(p))
	}
	svc, ctx, src := a.svc, a.ctx, res.Source
	return a.start(mutation{key: key, run: func(bool) outcome {
		spec, err := source.Parse(src + "@" + res.SkillID)
		if err != nil {
			return outcome{err: err}
		}
		f, err := svc.Fetch(ctx, spec)
		if err != nil {
			return outcome{err: err}
		}
		defer func() { _ = f.Close() }()
		return installOutcome(svc.Install(ctx, f, f.Candidates), sanitize.Line(src))
	}, done: func(out outcome) tea.Cmd {
		a.installed(key, out)
		return nil
	}}, false)
}

// installCandidate adds a candidate of the browsed source to the library.
// What the candidate says about the library may be stale, so core decides
// whether it is already installed or conflicts.
func (a *app) installCandidate(c core.Candidate, key string) tea.Cmd {
	f, svc, ctx := a.fd.fetched, a.svc, a.ctx
	from := fetchedLabel(f)
	a.fd.installs[f]++
	return a.start(mutation{key: key, run: func(bool) outcome {
		return installOutcome(svc.Install(ctx, f, []core.Candidate{c}), from)
	}, done: func(out outcome) tea.Cmd {
		if a.fd.installs[f]--; a.fd.installs[f] <= 0 {
			delete(a.fd.installs, f)
		}
		a.installed(key, out)
		if a.fd.fetched != f && a.fd.installs[f] == 0 {
			return a.closeFetched(f)
		}
		return nil
	}}, false)
}

// installed records the outcome of adding a Find row: the row shows it was
// added, and Skills selects the skill when the view shows next.
func (a *app) installed(key string, out outcome) {
	if out.err != nil || out.path == "" {
		return
	}
	if len(out.lines) > 0 {
		a.fd.added[key] = out.path
	}
	a.sk.reveal = out.path
}

func installOutcome(results []core.InstallResult, from string) outcome {
	var out outcome
	for _, r := range results {
		name := sanitize.Line(r.Candidate.Name)
		switch {
		case r.Err != nil:
			out.err = r.Err
			continue
		case r.Already:
			out.note = name + " is already in your library as " + sanitize.Line(r.Path)
		default:
			out.lines = append(out.lines, "Added "+name+" to the library from "+from)
		}
		out.path = r.Path
	}
	return out
}

func fetchedLabel(f *core.Fetched) string {
	if f.Spec.Kind == source.Local {
		return tildePath(f.Spec.Path)
	}
	label := source.Label(f.Spec.URL)
	if f.Spec.Subdir != "" {
		label += "/" + f.Spec.Subdir
	}
	return sanitize.Line(label)
}

// retry repeats the failed fetch or search.
func (a *app) retry() tea.Cmd {
	if a.fd.fetchErr != nil {
		return a.browse(strings.TrimSpace(a.fd.input.Value()))
	}
	a.fd.seq++
	a.fd.err, a.fd.loading = nil, true
	return a.search()
}

// openSource opens the highlighted row's repository page in the browser.
func (a *app) openSource() tea.Cmd {
	r, _ := a.findCurrent()
	var url string
	switch {
	case r.res != nil:
		if spec, err := source.Parse(r.res.Source); err == nil {
			url = source.WebURL(spec.URL, "", "")
		}
	case r.cand != nil:
		spec := a.fd.fetched.Spec
		url = source.WebURL(spec.URL, spec.Ref, r.cand.RelPath)
	}
	if url == "" {
		return a.setStatus(stInfo, "This source has no web page")
	}
	return tea.Batch(openURL(url), a.setStatus(stInfo, "Opening "+url))
}

// Rendering.

func (a *app) findListView(w, h int) string {
	t := a.th
	spin := ""
	if a.fd.loading || a.fd.fetching {
		spin = a.spin.View() + " "
	}
	lines := []string{
		" " + layoutLine(w-1, a.fd.input.View(), spin),
		" " + t.hint.Render(truncate(a.findMeta(w-2), w-2)),
	}
	rows := a.findRows()
	msg := a.findMessage(rows, w-2)
	if len(msg) > 0 {
		lines = append(lines, "")
		for _, m := range msg {
			lines = append(lines, " "+m)
		}
		return strings.Join(lines, "\n")
	}
	start, end := a.fd.list.window(len(rows), h-2)
	for i := start; i < end; i++ {
		lines = append(lines, a.findRowView(rows[i], w, i == a.fd.list.cursor))
	}
	return strings.Join(lines, "\n")
}

// findMeta is the line under the input: what the list shows, in w cells.
func (a *app) findMeta(w int) string {
	q := sanitize.Line(strings.TrimSpace(a.fd.input.Value()))
	switch {
	case a.fd.fetched != nil:
		label, count := fetchedLabel(a.fd.fetched), " · "+plural(len(a.fd.fetched.Candidates), "skill", "skills")
		if lw, fit := ansi.StringWidth(label), max(8, w-ansi.StringWidth(count)); lw > fit {
			label = ansi.TruncateLeft(label, lw-fit+1, "…") // keep the count and the end of the path
		}
		return label + count
	case a.fd.fetching:
		return "Fetching " + q + "…"
	case a.fd.fetchErr != nil:
		return q
	case source.IsExplicit(q):
		spec, _ := source.Parse(q)
		if spec.Kind == source.Local {
			return tildePath(spec.Path)
		}
		return sanitize.Line(spec.URL)
	case a.fd.loading && a.fd.results == nil:
		return "Searching skills.sh…"
	case a.fd.query != "" && a.fd.err == nil:
		return "skills.sh · " + plural(len(a.fd.results), "result", "results")
	}
	return "skills.sh"
}

// findMessage is shown instead of rows: errors, hints and empty results.
func (a *app) findMessage(rows []findRow, w int) []string {
	t := a.th
	q := sanitize.Line(strings.TrimSpace(a.fd.input.Value()))
	switch {
	case a.fd.fetchErr != nil:
		return []string{
			t.bad.Render("✗ Couldn't fetch " + truncate(q, w-18)),
			wrapText(t.dim.Render(sanitize.Line(a.fd.fetchErr.Error())), w),
		}
	case a.fd.err != nil:
		return []string{
			t.bad.Render("✗ Search failed"),
			wrapText(t.dim.Render(sanitize.Line(a.fd.err.Error())), w),
		}
	case len(rows) > 0, a.fd.loading, a.fd.fetching:
		return nil
	case a.fd.fetched != nil:
		return []string{t.dim.Render("No skills found in this source")}
	case source.IsExplicit(q):
		return nil // the line above names the source; enter fetches it
	case utf8.RuneCountInString(q) < 2:
		return []string{
			t.dim.Render("Type to search skills.sh."),
			"",
			t.dim.Render("Or paste a source to browse its skills:"),
			t.hint.Render("  owner/repo"),
			t.hint.Render("  https://github.com/owner/repo"),
			t.hint.Render("  ./path/to/folder"),
		}
	}
	return []string{t.dim.Render("No skills found for “" + truncate(q, w-24) + "”")}
}

func (a *app) findRowView(r findRow, w int, selected bool) string {
	t := a.th
	var bg color.Color
	nameStyle := t.text
	selected = selected && !a.fd.typing // the cursor shows once the list has the focus
	if selected {
		bg, nameStyle = t.selection, t.bold
	}
	if r.res != nil && !a.resultsCurrent() {
		nameStyle = t.dim // results of an earlier query, until the new ones arrive
	}
	left := []seg{cursorSeg(t, selected)}
	if r.res != nil {
		left = append(left,
			seg{text: sanitize.Line(cmp.Or(r.res.Name, r.res.SkillID)), style: nameStyle},
			seg{text: "  " + sanitize.Line(r.res.Source), style: t.hint})
	} else {
		left = append(left, seg{text: sanitize.Line(r.cand.Name), style: nameStyle})
		if r.cand.RelPath != "" {
			left = append(left, seg{text: "  " + sanitize.Line(r.cand.RelPath), style: t.hint})
		}
	}
	// The state, then the install count at the right edge, where counts line
	// up. A row that does not fit shortens its left side, as layoutRow does,
	// and drops the count before the state when the right side would take over
	// 40% of it.
	var right []seg
	if text, st := a.findStatus(r); text != "" {
		if a.busy[r.key()] {
			right = append(right, seg{text: a.spinnerText(bg)}, seg{text: " " + text, style: st})
		} else {
			right = append(right, seg{text: text, style: st})
		}
	}
	if r.res != nil {
		count := []seg{{text: compactCount(r.res.Installs), style: t.dim}}
		if len(right) > 0 {
			count = append([]seg{{text: "  "}}, count...)
		}
		with := append(slices.Clone(right), count...)
		if rw := segsWidth(with) + 1; len(right) == 0 || segsWidth(left)+rw+1 <= w || (rw+1)*5 <= w*2 {
			right = with
		}
	}
	right = append(right, seg{text: " "})
	return layoutRow(w, bg, left, right)
}

// findStatus is the right-hand state of a row.
func (a *app) findStatus(r findRow) (string, lipgloss.Style) {
	t := a.th
	key := r.key()
	switch {
	case a.busy[key]:
		return "adding", t.accented
	case a.fd.added[key] != "":
		return "✓ added", t.good
	case r.res != nil && a.fd.inLib[key] != "":
		return "✓ in library", t.good
	case r.cand == nil:
		return "", t.text
	}
	switch c := a.liveCandidate(*r.cand); {
	case c.Installed != "":
		return "✓ in library", t.good
	case c.Conflict != "":
		return "✗ conflict", t.bad
	case c.Issue != "":
		return "✗ invalid", t.bad
	}
	return "", t.text
}

// spinnerText is the current spinner frame on the given background.
func (a *app) spinnerText(bg color.Color) string {
	sp := a.spin
	if bg != nil {
		sp.Style = sp.Style.Background(bg)
	}
	return sp.View()
}

func (a *app) findPreview(w int) (item string, header []string, intro string, d *doc) {
	r, ok := a.findCurrent()
	if !ok {
		return "", nil, "", nil
	}
	t := a.th
	key := r.key()
	if r.res != nil {
		res := r.res
		title := t.bold.Render(sanitize.Line(cmp.Or(res.Name, res.SkillID)))
		if res.Name != "" && res.Name != res.SkillID {
			title += "  " + t.hint.Render(sanitize.Line(res.SkillID))
		}
		state := t.hint.Render("not in your library yet")
		switch {
		case a.fd.added[key] != "":
			state = t.good.Render("✓ added")
		case a.fd.inLib[key] != "":
			state = t.good.Render("✓ in your library as " + sanitize.Line(a.fd.inLib[key]))
		}
		header = []string{title, t.dim.Render(sanitize.Line(res.Source) + " · " + compactCount(res.Installs) + " installs"), state}
		dd := remoteDoc(a.svc, res.Source, res.SkillID)
		return key, header, "", &dd
	}
	header, intro = candidatePreview(t, a.liveCandidate(*r.cand), fetchedLabel(a.fd.fetched), a.fd.added[key] != "", w)
	dd := dirDoc(r.cand.Dir)
	return key, header, intro, &dd
}

// candidatePreview is the preview header and intro of a fetched candidate,
// shared by the Find view and the add picker.
func candidatePreview(t theme, c core.Candidate, from string, added bool, w int) (header []string, intro string) {
	where := sanitize.Line(c.RelPath)
	if where == "" {
		where = "repository root"
	}
	state := t.hint.Render("not in your library yet")
	var notes []string
	switch {
	case added:
		state = t.good.Render("✓ added")
	case c.Installed != "":
		state = t.good.Render("✓ in your library as " + sanitize.Line(c.Installed))
	case c.Conflict != "":
		state = t.bad.Render("✗ name taken")
		notes = append(notes, t.bad.Render("✗ The library already has "+sanitize.Line(c.Conflict)+" from another source under this name."))
	case c.Issue != "":
		state = t.bad.Render("✗ invalid")
		notes = append(notes, t.bad.Render("✗ "+sanitize.Line(c.Issue)))
	}
	header = []string{t.bold.Render(sanitize.Line(c.Name)), t.dim.Render(from + " · " + where), state}
	return header, wrapText(strings.Join(notes, "\n\n"), w)
}
