package tui

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skillssh"
	"github.com/legibet/myskills/internal/source"
)

func findHarness(t *testing.T, f *fakeService) *harness {
	t.Helper()
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.press("f")
	return h
}

func TestFindDebouncesAndDropsStaleResults(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.contains("Type to search skills.sh", "owner/repo")

	h.typeText("r")
	if len(h.timers) != 0 {
		t.Fatal("one character should not search")
	}
	h.typeText("eview")
	if got := f.called("Search"); len(got) != 0 {
		t.Fatalf("searched before the debounce: %v", got)
	}
	h.fire() // five debounce timers; only the last one is current
	if got := f.called("Search"); !slices.Equal(got, []string{"Search review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("skills.sh · 2 results", "review", "acme/skills", "12.3K", "Code Review", "999")

	// A response for an older request is dropped.
	h.send(searchMsg{seq: h.a.fd.seq - 1, query: "rev", results: []skillssh.Result{{Source: "x/y", SkillID: "stale"}}})
	h.lacks("stale")
}

func TestFindInitialQuerySearchesAtOnce(t *testing.T) {
	f := libraryFixture()
	h := newHarness(t, f, Options{View: Find, Query: "review"}, 120, 30)
	if got := f.called("Search"); !slices.Equal(got, []string{"Search review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("acme/skills")
	if h.a.fd.typing {
		t.Fatal("the results should have the focus")
	}
	h.press("esc", "esc") // back to the query, then to Skills
	if h.a.view != Skills {
		t.Fatal("esc should go back to Skills")
	}
}

func TestFindInitialSourceFetchesAtOnce(t *testing.T) {
	f := libraryFixture()
	spec, _ := source.Parse("acme/skills")
	f.fetched["acme/skills"] = &core.Fetched{Spec: spec, Candidates: []core.Candidate{{Name: "alpha", Dir: "/co/alpha"}}}
	h := newHarness(t, f, Options{View: Find, Query: "acme/skills"}, 120, 30)
	if got := f.called("Fetch"); len(got) != 1 || h.a.fd.typing {
		t.Fatalf("calls = %v, typing %v", got, h.a.fd.typing)
	}
	h.contains("acme/skills · 1 skill", "alpha")
}

func TestFindAddsResultWithoutEnabling(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.press("enter", "space")
	h.contains("Add review to the library?", "From https://github.com/acme/skills.git")
	if got := f.called("Fetch"); len(got) != 0 {
		t.Fatalf("added before it was confirmed: %v", got)
	}
	h.press("y")
	want := []string{"Fetch https://github.com/acme/skills.git skill=review", "Install [review]"}
	if got := slices.Concat(f.called("Fetch"), f.called("Install")); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if got := slices.Concat(f.called("Enable"), f.called("Disable")); len(got) != 0 {
		t.Fatalf("adding must not enable: %v", got)
	}
	h.contains("✓ added", "Added review to the library from acme/skills")
	h.lacks("enable it")
	h.press("space")
	h.contains("review is already in your library as review")
	if len(f.called("Install")) != 1 {
		t.Fatal("adding twice should not install again")
	}
}

func TestFindAddedSkillIsSelectedInSkills(t *testing.T) {
	f := libraryFixture()
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("tab", "/") // Enabled tab and a filter hide the new skill
	h.typeText("td")
	h.press("enter", "f")
	h.typeText("review")
	h.fire()
	h.press("enter", "space", "y", "esc", "esc")
	if r, _ := h.a.current(); h.a.view != Skills || r.key() != "review" || h.a.sk.tab != tabAll || h.a.sk.filter.Value() != "" {
		t.Fatalf("view %v, row %q, tab %v, filter %q", h.a.view, r.key(), h.a.sk.tab, h.a.sk.filter.Value())
	}
	h.press("space")
	if got := f.called("Enable"); !slices.Equal(got, []string{"Enable /work/proj review mode=link agents=[] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
	if h.a.sk.reveal != "" {
		t.Fatal("the reveal should be used once")
	}
}

func TestFindMarksInstalledResults(t *testing.T) {
	f := libraryFixture()
	f.lib = append(f.lib, core.Skill{Path: "review", Name: "review", Dir: "/lib/review", Record: gitRecord("https://github.com/acme/skills.git")})
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.contains("✓ in library", "✓ in your library as review")
	h.press("enter", "space")
	if len(f.called("Fetch")) != 0 || h.a.modal != nil {
		t.Fatal("an installed result should not be fetched")
	}
}

func TestFindMarksConflictingResults(t *testing.T) {
	f := libraryFixture()
	f.lib = append(f.lib, core.Skill{Path: "review", Name: "review", Dir: "/lib/review", Record: gitRecord("https://github.com/other/review.git")})
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.contains("✗ conflict")
	h.press("enter", "space")
	h.contains("The library already has review from another source under this name")
	if len(f.called("Fetch")) != 0 || h.a.modal != nil {
		t.Fatal("a conflicting result should not be fetched")
	}
}

func TestFindBrowsesSource(t *testing.T) {
	f := libraryFixture()
	spec, _ := source.Parse("acme/skills")
	f.lib = append(f.lib,
		core.Skill{Path: "beta", Name: "beta", Dir: "/lib/beta", Record: gitRecord(spec.URL)},
		core.Skill{Path: "gamma", Name: "gamma", Dir: "/lib/gamma", Record: gitRecord("https://github.com/other/gamma.git")})
	f.fetched["acme/skills"] = &core.Fetched{Spec: spec, Candidates: []core.Candidate{
		{Name: "alpha", Description: "First skill.", Dir: "/co/alpha", RelPath: "skills/alpha"},
		{Name: "beta", Dir: "/co/beta", RelPath: "skills/beta", Installed: "beta"},
		{Name: "gamma", Dir: "/co/gamma", RelPath: "skills/gamma", Conflict: "gamma"},
	}}
	h := findHarness(t, f)
	h.typeText("acme/skills")
	h.fire() // timers of the prefixes typed before it became a source are stale
	if got := f.called("Search"); len(got) != 0 {
		t.Fatalf("an explicit source should not search skills.sh: %v", got)
	}
	// The source is named, not listed: enter fetches it.
	h.contains("https://github.com/acme/skills", "enter fetch")
	h.lacks("Browse", "No skills found")
	h.press("enter")
	if got := f.called("Fetch"); !slices.Equal(got, []string{"Fetch https://github.com/acme/skills.git skill="}) || h.a.fd.typing {
		t.Fatalf("calls = %v, typing %v", got, h.a.fd.typing)
	}
	h.contains("acme/skills · 3 skills", "alpha", "skills/alpha", "✓ in library", "✗ conflict", "About alpha.", "Body of /co/alpha")

	h.press("down", "down", "space") // the name is taken: nothing to confirm
	h.contains("The library already has gamma from another source under this name")
	h.press("up", "up", "space")
	h.contains("Add alpha to the library?", "From https://github.com/acme/skills.git · skills/alpha")
	h.press("y")
	if got := f.called("Install"); !slices.Equal(got, []string{"Install [alpha]"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("✓ added")

	// Back in the query, the source stays: enter or ↓ return to it unfetched.
	for _, k := range []string{"enter", "down"} {
		h.press("esc")
		if !h.a.fd.typing || h.a.fd.fetched == nil {
			t.Fatal("esc should focus the query and keep the source")
		}
		h.contains("enter results")
		h.press(k)
		if h.a.fd.typing || len(f.called("Fetch")) != 1 {
			t.Fatalf("%s should return to the source without fetching it again", k)
		}
	}

	// Editing the query leaves it.
	h.press("esc", "backspace")
	if h.a.fd.fetched != nil || h.a.view != Find {
		t.Fatal("a new query should leave the source")
	}
}

func TestFindQueryTakesEveryKey(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("react best qjklh")
	if v := h.a.fd.input.Value(); v != "react best qjklh" || !h.a.fd.typing || h.a.modal != nil {
		t.Fatalf("query %q, typing %v", v, h.a.fd.typing)
	}

	// enter or ↓ go to the results; esc, h, ←, / and ↑ on the first row
	// come back; esc in the query goes back to Skills.
	h.a.fd.input.SetValue("")
	h.typeText("review")
	h.fire()
	for _, k := range []string{"esc", "h", "left", "/", "up"} {
		h.press("down")
		if h.a.fd.typing {
			t.Fatal("down should focus the results")
		}
		h.press(k)
		if !h.a.fd.typing || h.a.view != Find {
			t.Fatalf("%s in the results should focus the query", k)
		}
	}
	h.press("enter")
	if h.a.fd.typing {
		t.Fatal("enter should focus the results")
	}
	h.press("esc", "esc")
	if h.a.view != Skills {
		t.Fatal("esc in the query should go back to Skills")
	}
}

func TestFindPasteOverResultsStartsANewQuery(t *testing.T) {
	h := findHarness(t, libraryFixture())
	h.typeText("review")
	h.fire()
	h.press("enter")
	h.send(tea.PasteMsg{Content: "acme/skills"})
	if v := h.a.fd.input.Value(); v != "acme/skills" || !h.a.fd.typing {
		t.Fatalf("query %q, typing %v", v, h.a.fd.typing)
	}
	h.send(tea.PasteMsg{Content: "/x"}) // in the query, a paste inserts
	if v := h.a.fd.input.Value(); v != "acme/skills/x" {
		t.Fatalf("query %q", v)
	}
}

func TestFindReaderAdds(t *testing.T) {
	f := libraryFixture()
	h := newHarness(t, f, projectOptions(), 80, 24)
	h.press("f")
	h.typeText("review")
	h.fire()
	h.press("enter", "enter") // to the results, then read the first
	if h.a.reading == "" {
		t.Fatal("enter should open the reader")
	}
	h.contains("space add", "o open in browser")
	h.press("space", "y")
	if got := f.called("Install"); !slices.Equal(got, []string{"Install [review]"}) || h.a.reading == "" {
		t.Fatalf("calls = %v, reading %q", got, h.a.reading)
	}
	h.contains("✓ added")
}

func TestFindFetchErrorRetries(t *testing.T) {
	f := libraryFixture()
	f.failNext("Fetch", errors.New("repository not found"))
	h := findHarness(t, f)
	h.typeText("acme/nope")
	h.press("enter")
	h.contains("Couldn't fetch acme/nope", "repository not found", "r retry")
	h.press("r")
	if got := f.called("Fetch"); len(got) != 2 {
		t.Fatalf("calls = %v", got)
	}
}

func TestFindSearchErrorRetries(t *testing.T) {
	f := libraryFixture()
	f.failNext("Search", errors.New("skills.sh: 503"))
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.contains("Search failed", "skills.sh: 503")
	h.press("enter") // enter in the query searches again
	h.contains("acme/skills")
}

func TestFindPreviewWaitsForCursor(t *testing.T) {
	f := libraryFixture()
	f.markdown["acme/skills/review"] = "---\nname: review\ndescription: Reviews code.\nlicense: MIT\nmetadata:\n  author: acme\n---\n# review remote body"
	h := findHarness(t, f)
	h.typeText("review")
	h.fire() // debounce → results, which start the settle timer
	if got := f.called("SkillMarkdown"); len(got) != 0 {
		t.Fatalf("preview fetched before the cursor settled: %v", got)
	}
	h.contains("Loading preview…")
	h.press("enter", "down", "up")
	h.fire() // three settle timers; only the last counts
	if got := f.called("SkillMarkdown"); !slices.Equal(got, []string{"SkillMarkdown acme/skills/review"}) {
		t.Fatalf("calls = %v", got)
	}
	// The frontmatter shows as fields above the body.
	h.contains("Reviews code.", "license", "MIT", "author", "acme", "review remote body")
	h.lacks("name: review", "---")

	h.press("down")
	h.fire()
	h.press("up")
	h.contains("review remote body")
	if got := f.called("SkillMarkdown"); len(got) != 2 {
		t.Fatalf("previews should be cached: %v", got)
	}
}

func TestFindSanitizesRemoteText(t *testing.T) {
	f := libraryFixture()
	f.results["evil"] = []skillssh.Result{{Source: "bad/repo", SkillID: "evil", Name: "ev\x1b[31mil\x07", Installs: 1}}
	f.markdown["bad/repo/evil"] = "# Title\x1b]0;pwned\x07\n\nText \x1b[2J here"
	h := findHarness(t, f)
	h.typeText("evil")
	h.fire()
	h.fire()
	v := h.a.View().Content
	for _, bad := range []string{"\x1b[31m", "\x07", "\x1b]0;", "\x1b[2J", "pwned"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q", bad)
		}
	}
	h.contains("evil", "Text  here")
}

func TestFindOpensSourceInBrowser(t *testing.T) {
	h := findHarness(t, libraryFixture())
	h.typeText("review")
	h.fire()
	h.press("enter", "o")
	h.contains("Opening https://github.com/acme/skills")
}

func TestFindIgnoresStaleResults(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.typeText("er") // the next search is pending
	h.press("down", "space")
	if got := f.called("Fetch"); len(got) != 0 || h.a.modal != nil {
		t.Fatalf("a result of the previous query was added: %v", got)
	}
	if h.a.nkeys.Add.Enabled() {
		t.Fatal("adding should be off until the results answer the input")
	}
}

func TestFindAddsAgainAfterRemove(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.press("enter", "space", "y", "esc", "esc") // added; Skills selects it
	h.press("d", "y")
	if got := f.called("Remove"); !slices.Equal(got, []string{"Remove review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.press("f")
	h.lacks("✓ added", "✓ in library")
	h.press("enter", "space", "y")
	if got := f.called("Install"); len(got) != 2 {
		t.Fatalf("the removed skill should install again: %v", got)
	}
	h.contains("✓ added")
}

// realCheckout fetches a local git repository with one skill through a real
// core service, so that the checkout is a temporary directory Close removes.
func realCheckout(t *testing.T) (input string, f *core.Fetched) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	t.Setenv("MYSKILLS_HOME", filepath.Join(tmp, "home", ".myskills"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "gitconfig"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: alpha\ndescription: First skill.\n---\n\n# Alpha\n"
	if err := os.WriteFile(filepath.Join(repo, "skills", "alpha", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	svc, err := core.Open()
	if err != nil {
		t.Fatal(err)
	}
	input = (&url.URL{Scheme: "file", Path: filepath.ToSlash(repo)}).String()
	spec, err := source.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if f, err = svc.Fetch(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	return input, f
}

func TestShutdownClosesCheckoutsOfUnfinishedInstalls(t *testing.T) {
	input, checkout := realCheckout(t)
	f := libraryFixture()
	f.fetched[input] = checkout
	h := findHarness(t, f)
	h.typeText(input)
	h.press("enter")
	h.contains("alpha")
	h.press("space")
	_, install := h.a.Update(keyPress("y")) // the install starts
	h.press("esc")
	h.typeText("x") // a new query leaves the source; the install keeps the checkout
	if _, err := os.Stat(checkout.Root); err != nil {
		t.Fatalf("the checkout went while an install used it: %v", err)
	}
	// The program quits before the install's result is handled.
	drain(install)
	h.a.shutdown()
	if _, err := os.Stat(checkout.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("checkout left behind: %v", err)
	}
}

func TestShutdownClosesCheckoutsOfDroppedFetches(t *testing.T) {
	// The program quits before the fetch result is handled.
	input, checkout := realCheckout(t)
	f := libraryFixture()
	f.fetched[input] = checkout
	h := findHarness(t, f)
	h.typeText(input)
	_, fetch := h.a.Update(keyPress("enter"))
	drain(fetch)
	h.a.shutdown()
	if _, err := os.Stat(checkout.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("checkout left behind: %v", err)
	}

	// The fetch finishes only after shutdown.
	input, checkout = realCheckout(t)
	f.fetched[input] = checkout
	h = findHarness(t, f)
	h.typeText(input)
	_, fetch = h.a.Update(keyPress("enter"))
	h.a.shutdown()
	drain(fetch)
	if _, err := os.Stat(checkout.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("checkout left behind: %v", err)
	}
}

// drain runs cmd and the commands it batches, and drops their messages, as
// when the program has quit.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			drain(c)
		}
	}
}

func TestFindCountsLineUpAndGoFirst(t *testing.T) {
	f := libraryFixture()
	f.results["review"] = []skillssh.Result{
		{Source: "acme/skills", SkillID: "review-long", Installs: 12345},
		{Source: "other/repo", SkillID: "lint", Installs: 999},
	}
	f.lib = append(f.lib, core.Skill{Path: "review-long", Name: "review-long", Dir: "/lib/review-long", Record: gitRecord("https://github.com/acme/skills.git")})
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	row := func(name string) string {
		for l := range strings.SplitSeq(h.view(), "\n") {
			if list, _, _ := strings.Cut(l, "│"); strings.Contains(list, name) && !strings.Contains(list, "›") {
				return strings.TrimRight(list, " ")
			}
		}
		t.Fatalf("no row %s:\n%s", name, h.view())
		return ""
	}
	// The counts end in the same column, after the state.
	long, short := row("review-long"), row("lint")
	if !strings.HasSuffix(long, "✓ in library  12.3K") || !strings.HasSuffix(short, "999") ||
		ansi.StringWidth(long) != ansi.StringWidth(short) {
		t.Fatalf("rows:\n%q\n%q", long, short)
	}
	// Short of room, the count goes and the state stays.
	h.send(windowSize(44, 24))
	if long = row("review-long"); !strings.HasSuffix(long, "✓ in library") {
		t.Fatalf("narrow row = %q", long)
	}
}
