package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	h.press("esc")
	if h.a.view != Skills {
		t.Fatal("esc should go back to Skills")
	}
}

func TestFindAddsResultWithoutEnabling(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.press("enter")
	want := []string{"Fetch https://github.com/acme/skills.git skill=review", "Install [review]"}
	if got := slices.Concat(f.called("Fetch"), f.called("Install")); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if got := slices.Concat(f.called("Enable"), f.called("Disable")); len(got) != 0 {
		t.Fatalf("adding must not enable: %v", got)
	}
	h.contains("✓ added", "Added review to the library from acme/skills", "enable it from Skills")
	h.press("enter")
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
	h.press("enter", "esc")
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
	h.press("enter")
	if len(f.called("Fetch")) != 0 {
		t.Fatal("an installed result should not be fetched")
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
	h.contains("→ Browse acme/skills")
	h.press("enter")
	if got := f.called("Fetch"); !slices.Equal(got, []string{"Fetch https://github.com/acme/skills.git skill="}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("acme/skills · 3 skills", "alpha", "skills/alpha", "✓ in library", "✗ conflict", "First skill.", "Body of /co/alpha")

	h.press("down", "down", "enter") // core decides, and refuses the conflict
	h.contains("already has a skill \"gamma\" from another source")
	h.press("up", "up", "enter")
	if got := f.called("Install"); !slices.Equal(got, []string{"Install [gamma]", "Install [alpha]"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("✓ added")

	h.press("esc")
	if h.a.fd.fetched != nil || h.a.view != Find {
		t.Fatal("esc should leave the source but stay in Find")
	}
	h.contains("→ Browse acme/skills")
}

func TestFindFetchErrorRetries(t *testing.T) {
	f := libraryFixture()
	f.failNext("Fetch", errors.New("repository not found"))
	h := findHarness(t, f)
	h.typeText("acme/nope")
	h.press("enter")
	h.contains("Couldn't fetch acme/nope", "repository not found", "ctrl+r")
	h.press("ctrl+r")
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
	h.contains("Search failed", "skills.sh: 503", "ctrl+r retry")
	h.press("ctrl+r")
	h.contains("acme/skills")
}

func TestFindPreviewWaitsForCursor(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire() // debounce → results, which start the settle timer
	if got := f.called("SkillMarkdown"); len(got) != 0 {
		t.Fatalf("preview fetched before the cursor settled: %v", got)
	}
	h.contains("Loading preview…")
	h.press("down", "up")
	h.fire() // three settle timers; only the last counts
	if got := f.called("SkillMarkdown"); !slices.Equal(got, []string{"SkillMarkdown acme/skills/review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("review remote body")
	h.lacks("name: review") // frontmatter is stripped

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
	h.press("ctrl+o")
	h.contains("Opening https://github.com/acme/skills")
}

func TestFindIgnoresStaleResults(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.typeText("er") // the next search is pending
	h.press("enter")
	if got := f.called("Fetch"); len(got) != 0 {
		t.Fatalf("a result of the previous query was added: %v", got)
	}
	if h.a.nkeys.Enter.Enabled() {
		t.Fatal("enter should be off until the results answer the input")
	}
}

func TestFindAddsAgainAfterRemove(t *testing.T) {
	f := libraryFixture()
	h := findHarness(t, f)
	h.typeText("review")
	h.fire()
	h.press("enter", "esc") // added; Skills selects it
	h.press("x", "y")
	if got := f.called("Remove"); !slices.Equal(got, []string{"Remove review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.press("f")
	h.lacks("✓ added", "✓ in library")
	h.press("enter")
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
	input = "file://" + repo
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
	_, install := h.a.Update(keyPress("enter")) // the install starts
	h.press("esc")                              // leaving keeps the checkout for it
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
