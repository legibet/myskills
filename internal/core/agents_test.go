package core

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/legibet/myskills/internal/agent"
)

func TestAgentsConfig(t *testing.T) {
	e := newEnv(t)
	agents, err := e.svc.Agents()
	if err != nil || len(agents) != len(agent.Builtins) {
		t.Fatalf("Agents = %v, %v", agents, err)
	}

	if err := e.svc.AddAgent(agent.Agent{ID: "pi", Global: "~/.pi/agent/skills"}); err != nil {
		t.Fatal(err)
	}
	err = e.svc.AddAgent(agent.Agent{ID: "pi", Project: ".pi"})
	wantErr(t, err, ErrConflict, "pi")
	if err := e.svc.AddAgent(agent.Agent{ID: "universal", Project: ".x"}); err == nil {
		t.Error("built-in id accepted")
	}
	if err := e.svc.AddAgent(agent.Agent{ID: "Bad", Project: ".x"}); err == nil {
		t.Error("invalid id accepted")
	}

	err = e.svc.SetDefaultAgents([]string{"pi", "nope"})
	wantErr(t, err, ErrNotFound, "nope")
	if err := e.svc.SetDefaultAgents(nil); err == nil {
		t.Error("empty defaults accepted")
	}
	if err := e.svc.SetDefaultAgents([]string{"pi", "universal", "pi"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := e.svc.Config()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Agents, []string{"pi", "universal"}) || len(cfg.CustomAgents) != 1 || cfg.CustomAgents[0].Name != "pi" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestRemoveAgent(t *testing.T) {
	e := setup(t)
	for _, a := range []agent.Agent{
		{ID: "twin", Name: "Twin", Project: ".agents/skills"}, // shares the universal dir
		{ID: "solo", Name: "Solo", Project: ".solo/skills", Global: "~/.solo/skills"},
	} {
		if err := e.svc.AddAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.SetDefaultAgents([]string{"universal", "solo"}); err != nil {
		t.Fatal(err)
	}
	sc := e.project("proj", true)
	e.enable(sc, "alpha", EnableOptions{})
	e.enable(sc, "beta", EnableOptions{Mode: Copy, Agents: []string{"solo"}})
	e.enable(Global, "alpha", EnableOptions{Agents: []string{"solo"}})
	soloLink := filepath.Join(sc.Root, ".solo", "skills", "alpha")
	soloCopy := filepath.Join(sc.Root, ".solo", "skills", "beta")
	if !strings.Contains(exclude(t, sc.Root), "/.solo/skills/alpha") {
		t.Fatal("exclude block lacks the solo link")
	}

	// A dir shared with a remaining agent keeps its placements.
	removed, err := e.svc.RemoveAgent("twin")
	if err != nil || len(removed) != 0 {
		t.Fatalf("remove twin = %v, %v", removed, err)
	}
	if !isSymlink(filepath.Join(sc.Root, ".agents", "skills", "alpha")) {
		t.Fatal("shared placement removed")
	}

	removed, err = e.svc.RemoveAgent("solo")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(e.home, ".solo", "skills", "alpha"), soloLink}
	if !slices.Equal(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if exists(soloLink) || !isRealDir(soloCopy) {
		t.Error("link kept or copy deleted")
	}
	if _, ok := e.lock().Copies[soloCopy]; ok {
		t.Error("copy not released")
	}
	cfg, _ := e.svc.Config()
	if !slices.Equal(cfg.Agents, []string{"universal"}) || len(cfg.CustomAgents) != 0 {
		t.Errorf("config = %+v", cfg)
	}
	ex := managedBlock(exclude(t, sc.Root))
	if strings.Contains(ex, ".solo") || !strings.Contains(ex, "/.agents/skills/alpha") {
		t.Errorf("exclude = %q", ex)
	}

	if _, err := e.svc.RemoveAgent("universal"); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("remove built-in: %v", err)
	}
	_, err = e.svc.RemoveAgent("nope")
	wantErr(t, err, ErrNotFound)
}

func TestRemoveAliasAgentKeepsSharedCopies(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	// .alias/skills resolves to the universal dir.
	mkdirAll(t, filepath.Join(sc.Root, ".agents"))
	symlink(t, ".agents", filepath.Join(sc.Root, ".alias"))
	if err := e.svc.AddAgent(agent.Agent{ID: "alias", Name: "Alias", Project: ".alias/skills"}); err != nil {
		t.Fatal(err)
	}
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"alias"}})
	if _, ok := e.lock().Copies[filepath.Join(sc.Root, ".alias", "skills", "alpha")]; !ok {
		t.Fatalf("copies = %v", e.lock().Copies)
	}

	if _, err := e.svc.RemoveAgent("alias"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.lock().Copies[filepath.Join(sc.Root, ".agents", "skills", "alpha")]; !ok {
		t.Errorf("copies = %v", e.lock().Copies)
	}
	st := e.status(sc)
	if len(st.Enablements) != 1 || st.Enablements[0].Mode != Copy || len(st.Others) != 0 {
		t.Errorf("status = %+v", st)
	}
}

// Concurrent mutations from several services serialize on the lock file, so
// no record is lost.
func TestConcurrentMutations(t *testing.T) {
	e := newEnv(t)
	skills := map[string]string{}
	for i := range 8 {
		name := fmt.Sprintf("s%d", i)
		skills[name] = "skills/" + name
	}
	_, url := e.repo("src", skills)
	f := e.fetch(url)
	defer func() { _ = f.Close() }()

	var projects []Scope
	for i := range 6 {
		projects = append(projects, e.project(fmt.Sprintf("p%d", i), false))
	}
	_, url2 := e.repo("src2", map[string]string{"shared": "shared"})
	e.add(url2)
	shared := e.skill("shared")

	var wg sync.WaitGroup
	errs := make(chan error, len(f.Candidates)+len(projects))
	for _, c := range f.Candidates {
		wg.Go(func() {
			svc, err := Open()
			if err == nil {
				err = svc.Install(f, []Candidate{c})[0].Err
			}
			errs <- err
		})
	}
	for _, sc := range projects {
		wg.Go(func() {
			svc, err := Open()
			if err == nil {
				_, err = svc.Enable(sc, shared, EnableOptions{Mode: Copy})
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	lock := e.lock()
	if len(lock.Skills) != 9 || len(lock.Projects) != 6 || len(lock.Copies) != 12 {
		t.Errorf("records: %d skills, %d projects, %d copies", len(lock.Skills), len(lock.Projects), len(lock.Copies))
	}
}
