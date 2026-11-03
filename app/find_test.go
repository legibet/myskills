package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skillssh"
)

// fakeSH is skills.sh for the tests: results by query and SKILL.md by
// owner/repo/skill. It records the searches.
type fakeSH struct {
	mu       sync.Mutex
	queries  []string
	results  map[string][]skillssh.Result
	markdown map[string]string
	fail     int           // searches failing next
	stall    string        // a query answered only once its request is canceled
	stalled  chan struct{} // closed as the stalled request arrives
}

// findHarness is a harness whose skills.sh is a fakeSH answering "review".
func findHarness(t *testing.T) (*harness, *fakeSH) {
	t.Helper()
	h := newHarness(t)
	sh := &fakeSH{
		results: map[string][]skillssh.Result{"review": {
			{Source: "acme/skills", SkillID: "review", Name: "review", Installs: 12345},
			{Source: "other/tools", SkillID: "lint", Name: "lint", Installs: 999},
		}},
		markdown: map[string]string{
			"acme/skills/review": "---\nname: review\ndescription: Reviews code.\nlicense: MIT\n---\n# Review\n\nRemote body of review.\n",
			"other/tools/lint":   "---\nname: lint\ndescription: Lints code.\n---\nRemote body of lint.\n",
		},
		stalled: make(chan struct{}),
	}
	srv := httptest.NewServer(http.HandlerFunc(sh.serve))
	t.Cleanup(srv.Close)
	h.svc.SkillsSH = &skillssh.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	return h, sh
}

func (sh *fakeSH) serve(w http.ResponseWriter, r *http.Request) {
	if path, ok := strings.CutPrefix(r.URL.Path, "/api/download/"); ok {
		md, ok := sh.markdown[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]string{{"path": "SKILL.md", "contents": md}}})
		return
	}
	q := r.URL.Query().Get("q")
	sh.mu.Lock()
	sh.queries = append(sh.queries, q)
	failing := sh.fail > 0
	sh.fail--
	sh.mu.Unlock()
	switch {
	case q == sh.stall:
		close(sh.stalled)
		<-r.Context().Done()
	case failing:
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": sh.results[q]})
	}
}

func (sh *fakeSH) searched() []string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return slices.Clone(sh.queries)
}

// openFind shows Find, whose field takes the keyboard.
func (h *harness) openFind() {
	h.a.page = pageFind
	h.settle()
}

// github makes the GitHub repository acme/skills a local repository with
// review, through git's url.insteadOf: adding a skills.sh result clones it.
func (h *harness) github() {
	url := h.repo("acme-skills", map[string]string{"review": "skills/review"})
	writeFile(h.t, filepath.Join(h.home, ".gitconfig"), "[url \""+url+"\"]\n\tinsteadOf = https://github.com/acme/skills.git\n")
}

// query focuses the field with ⌘F and replaces the query with s.
func (h *harness) query(s string) {
	h.tt.Key(ui.Cmd, ui.KeyF)
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.typeText(s)
}

func (h *harness) enter() {
	h.tt.Key(0, ui.KeyEnter)
	h.settle()
}

func (h *harness) wantGone(dir string) {
	h.t.Helper()
	h.a.work.Wait() // checkouts are removed off the main thread
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		h.t.Errorf("checkout %s left behind: %v", dir, err)
	}
}

func TestFindSearchesAsTyped(t *testing.T) {
	h, sh := findHarness(t)
	h.start()
	h.openFind()
	h.want("Find skills")

	h.typeText("r")
	h.tt.Type("ev")
	h.tt.Advance(200 * time.Millisecond)
	h.tt.Type("iew") // typing again waits again
	h.tt.Advance(200 * time.Millisecond)
	if got := sh.searched(); len(got) != 0 {
		t.Fatalf("searched while typing: %v", got)
	}
	h.settle()
	if got := sh.searched(); !slices.Equal(got, []string{"review"}) {
		t.Fatalf("searches = %v, want [review]", got)
	}
	h.want("review", "acme/skills · 12.3K installs", "lint", "999 installs")
	h.want("Reviews code.", "MIT", "Remote body of review.")
	h.click("lint")
	h.want("Lints code.", "Remote body of lint.")
}

func TestFindReturnSearchesAtOnce(t *testing.T) {
	h, _ := findHarness(t)
	h.start()
	h.openFind()
	h.tt.Type("review")
	h.tt.Key(0, ui.KeyEnter)
	if !h.a.find.due.IsZero() || !h.a.busy[findSearch] {
		t.Fatal("Return should search without waiting")
	}
	h.settle()
	h.want("acme/skills · 12.3K installs")

	h.typeText(" nothing")
	h.want("No skills found")
}

func TestFindSearchFailsAndRetries(t *testing.T) {
	h, sh := findHarness(t)
	sh.fail = 1
	h.start()
	h.openFind()
	h.typeText("review")
	h.want("Search failed", "503")
	h.click("Retry")
	h.want("acme/skills · 12.3K installs")
}

func TestFindDropsStaleSearches(t *testing.T) {
	h, sh := findHarness(t)
	sh.stall = "rev"
	h.start()
	h.openFind()
	h.tt.Type("rev")
	h.tt.Key(0, ui.KeyEnter)
	select {
	case <-sh.stalled:
	case <-time.After(10 * time.Second):
		t.Fatal("the search for rev never arrived")
	}
	h.tt.Type("iew") // cancels the search for rev
	h.enter()
	h.want("acme/skills · 12.3K installs")
	h.wantNo("Search failed")

	// An answer to an earlier search is dropped.
	h.a.searched(h.a.find.seq-1, "rev", []skillssh.Result{{Source: "x/y", SkillID: "stale-skill"}}, nil)
	h.settle()
	h.wantNo("stale-skill")
	h.want("acme/skills · 12.3K installs")
}

func TestFindAddsSearchResult(t *testing.T) {
	h, _ := findHarness(t)
	h.github()
	h.start()
	h.openFind()
	h.typeText("review")
	h.click("Add review")
	if h.alert() != "" {
		t.Fatalf("alert %q", h.alert())
	}
	sk := h.skill("review")
	if sk.Record == nil || sk.Record.Source.URL != "https://github.com/acme/skills.git" || sk.Record.Source.Path != "skills/review" {
		t.Fatalf("record = %+v", sk.Record)
	}
	if got := h.enabled(core.Global); len(got) != 0 {
		t.Fatalf("adding enabled %v", got)
	}
	h.want("Added")
	if _, ok := h.tt.Find("Add review"); ok {
		t.Error("an added skill still offers Add")
	}
	h.click("Show in Skills")
	if h.a.page != pageSkills || h.a.skills.path != "review" {
		t.Fatalf("page %q, skill %q", h.a.page, h.a.skills.path)
	}
}

func TestFindMarksResultsInLibrary(t *testing.T) {
	h, _ := findHarness(t)
	h.github()
	h.add("acme/skills")
	h.start()
	h.openFind()
	h.typeText("review")
	h.want("In Library")
	if _, ok := h.tt.Find("Add review"); ok {
		t.Error("a skill in the library offers Add")
	}
	if _, ok := h.tt.Find("Add lint"); !ok {
		t.Error("lint should offer Add")
	}
}

func TestFindBrowsesGitSource(t *testing.T) {
	h, sh := findHarness(t)
	url := h.repo("pack", map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	h.add(h.repo("other", map[string]string{"beta": "beta"}))
	h.start()
	h.openFind()
	h.typeText(url)
	h.want("Fetch", "Fetch to see its skills")
	if got := sh.searched(); len(got) != 0 {
		t.Fatalf("a source was searched: %v", got)
	}

	h.enter()
	h.want("alpha", "skills/alpha", "2 skills", "Conflict", "Use alpha.", "The body of alpha.")
	root := h.a.find.fetched.Root
	h.click("Add alpha")
	if sk := h.skill("alpha"); sk.Record == nil || sk.Record.Source.Path != "skills/alpha" {
		t.Fatalf("alpha = %+v", sk)
	}
	h.want("Added")

	h.click("beta")
	h.want("The library already has beta from another source under this name.")

	h.tt.Key(ui.Cmd, ui.KeyF)
	h.tt.Key(0, ui.KeyEscape) // clears the field: the source is left
	h.settle()
	h.want("Find skills")
	h.wantGone(root)
}

func TestFindKeepsCheckoutForAddsInFlight(t *testing.T) {
	h, _ := findHarness(t)
	url := h.repo("pack", map[string]string{"alpha": "alpha"})
	h.start()
	h.openFind()
	h.typeText(url)
	h.click("Fetch")
	f := h.a.find.fetched
	h.a.find.hold(f) // as an add in flight does
	h.query(url + "/more")
	h.a.work.Wait()
	if _, err := os.Stat(f.Root); err != nil {
		t.Fatalf("the checkout went while an add used it: %v", err)
	}
	h.a.find.release(f)
	h.wantGone(f.Root)

	// Quitting removes the checkout shown.
	h.a.find.query = url
	h.a.submitQuery()
	h.settle()
	root := h.a.find.fetched.Root
	h.a.close()
	h.wantGone(root)
}

func TestFindLinksChosenFolder(t *testing.T) {
	h, _ := findHarness(t)
	dir := h.path("my skills")
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "Use alpha.", "The body of alpha.")
	writeSkill(t, filepath.Join(dir, "beta"), "beta", "Use beta.", "The body of beta.")
	h.folder = dir
	h.start()
	h.openFind()
	h.click("Choose Folder…")
	h.want("alpha", "beta", "2 skills", "Link Folder", "The body of alpha.")
	if h.a.find.query != dir {
		t.Fatalf("query = %q", h.a.find.query)
	}

	h.click("Link Folder")
	lib, err := h.svc.Library()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(lib, func(sk core.Skill) bool { return sk.Path == "my skills/alpha" && sk.Linked }) {
		t.Fatalf("library = %+v", lib)
	}
	h.want("Linked", "In Library")
	h.wantNo("Link Folder")
}

func TestFindMarksNameConflicts(t *testing.T) {
	h, _ := findHarness(t)
	h.add(h.repo("mine", map[string]string{"lint": "skills/lint"}))
	h.start()
	h.openFind()
	h.typeText("review")
	h.want("Conflict")
	if _, ok := h.tt.Find("Add lint"); ok {
		t.Error("a skill whose name the library has offers Add")
	}
	h.click("lint")
	h.want("The library already has lint from another source under this name.")
}
