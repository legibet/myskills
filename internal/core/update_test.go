package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/legibet/myskills/internal/skill"
	"github.com/legibet/myskills/internal/store"
)

func (e *env) check(skills ...string) *Updates {
	e.t.Helper()
	var sks []Skill
	for _, ref := range skills {
		sks = append(sks, e.skill(ref))
	}
	u, err := e.svc.CheckUpdates(context.Background(), sks, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = u.Close() })
	return u
}

func states(u *Updates) []UpdateState {
	var s []UpdateState
	for _, c := range u.Checks {
		s = append(s, c.State)
	}
	return s
}

func TestCheckUpdatesNoUpstreamChange(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	e.add(url)
	commit := e.lock().Skills["alpha"].Commit

	var calls [][2]int
	u, err := e.svc.CheckUpdates(context.Background(), []Skill{e.skill("alpha"), e.skill("beta")}, func(done, total int) {
		calls = append(calls, [2]int{done, total})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.Close() }()
	for _, c := range u.Checks {
		if c.State != UpToDate || c.Commit != commit || c.Err != nil {
			t.Errorf("check %s = %+v", c.Skill.Path, c)
		}
	}
	// Skills from one source are checked together.
	if !slices.Equal(calls, [][2]int{{1, 1}}) {
		t.Errorf("progress calls = %v", calls)
	}
	if res := e.svc.ApplyUpdates(u, nil, false); len(res) != 0 {
		t.Errorf("apply = %+v", res)
	}
}

func TestCheckUpdatesRecordsVerifiedCommit(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "skills/alpha"})
	e.add(url)
	writeFile(t, filepath.Join(dir, "README.md"), "unrelated")
	head := commitAll(t, dir, "unrelated change")

	u := e.check("alpha")
	if c := u.Checks[0]; c.State != UpToDate || c.Commit != head {
		t.Fatalf("check = %+v", c)
	}
	if rec := e.lock().Skills["alpha"]; rec.Commit != head {
		t.Errorf("recorded commit = %s, want %s", rec.Commit, head)
	}
}

func TestUpdateAvailableAndApply(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	e.add(url)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	before := e.lock().Skills["alpha"]

	writeSkill(t, filepath.Join(dir, "skills", "alpha"), "alpha", "alpha v2")
	writeFile(t, filepath.Join(dir, "skills", "alpha", "scripts", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(dir, "skills", "alpha", "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := commitAll(t, dir, "alpha v2")

	u := e.check("alpha", "beta")
	if !slices.Equal(states(u), []UpdateState{Available, UpToDate}) || u.Checks[0].Commit != head {
		t.Fatalf("checks = %+v", u.Checks)
	}

	res := e.svc.ApplyUpdates(u, nil, false)
	if len(res) != 1 || !res[0].Updated || res[0].Err != nil || res[0].Skill != "alpha" {
		t.Fatalf("apply = %+v", res)
	}
	lib := e.libDir("alpha")
	if !strings.Contains(readFile(t, filepath.Join(lib, "SKILL.md")), "alpha v2") {
		t.Error("content not updated")
	}
	if info, err := os.Stat(filepath.Join(lib, "scripts", "run.sh")); err != nil || runtime.GOOS != "windows" && info.Mode()&0o100 == 0 {
		t.Errorf("script = %v, %v", info, err)
	}
	rec := e.lock().Skills["alpha"]
	hash, _ := skill.Hash(lib)
	if rec.Commit != head || rec.Hash != hash || rec.InstalledAt != before.InstalledAt || rec.UpdatedAt.Before(before.UpdatedAt) {
		t.Errorf("record = %+v", rec)
	}
	if sk := e.skill("alpha"); sk.Modified {
		t.Error("updated skill shows as modified")
	}
	if l := leftovers(t, e.svc.paths.Skills); len(l) > 0 {
		t.Errorf("leftovers: %v", l)
	}

	// The copy in the project is now outdated and gets refreshed.
	if p := e.status(sc).Enablements[0].Placements[0]; !p.Outdated {
		t.Fatalf("copy not outdated: %+v", p)
	}
	if _, err := e.svc.RefreshCopies(sc, []string{"alpha"}, false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(sc.Root, ".agents", "skills", "alpha", "SKILL.md")); !strings.Contains(got, "alpha v2") {
		t.Error("copy not refreshed")
	}

	if u := e.check("alpha"); u.Checks[0].State != UpToDate {
		t.Errorf("after update: %+v", u.Checks[0])
	}
}

func TestUpdateSkipsModified(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	writeFile(t, filepath.Join(e.libDir("alpha"), "mine.md"), "local")

	u := e.check("alpha")
	if u.Checks[0].State != Available || !u.Checks[0].Skill.Modified {
		t.Fatalf("check = %+v", u.Checks[0])
	}
	res := e.svc.ApplyUpdates(u, []string{"alpha"}, false)
	wantErr(t, res[0].Err, ErrModified, "alpha")
	if res[0].Updated || !exists(filepath.Join(e.libDir("alpha"), "mine.md")) {
		t.Fatal("modified skill updated without force")
	}

	res = e.svc.ApplyUpdates(u, nil, true)
	if !res[0].Updated || exists(filepath.Join(e.libDir("alpha"), "mine.md")) {
		t.Errorf("forced update = %+v", res)
	}
	if res := e.svc.ApplyUpdates(u, []string{"other"}, true); len(res) != 0 {
		t.Errorf("unselected skill applied: %+v", res)
	}
}

func TestUpdateRechecksTheSkillBeforeSwapping(t *testing.T) {
	tests := []struct {
		name   string
		force  bool
		change func(t *testing.T, dest string) // while the new version is staged
		kind   error
		kept   string // file the change made, which must survive
	}{
		{"edited", false, func(t *testing.T, dest string) {
			writeFile(t, filepath.Join(dest, "mine.md"), "local")
		}, ErrModified, "mine.md"},
		{"replaced, forced", true, func(t *testing.T, dest string) {
			if err := os.Rename(dest, dest+"-moved"); err != nil {
				t.Fatal(err)
			}
			writeSkill(t, dest, "alpha", "unrelated")
			writeFile(t, filepath.Join(dest, "unmanaged.md"), "x")
		}, ErrConflict, "unmanaged.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
			e.add(url)
			writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
			commitAll(t, dir, "v2")
			u := e.check("alpha")
			dest := e.libDir("alpha")
			copySkill = func(src, dst, boundary string) error {
				tt.change(t, dest)
				return skill.Copy(src, dst, boundary)
			}
			defer func() { copySkill = skill.Copy }()

			res := e.svc.ApplyUpdates(u, nil, tt.force)
			wantErr(t, res[0].Err, tt.kind, "alpha")
			if res[0].Updated || !exists(filepath.Join(dest, tt.kept)) {
				t.Errorf("apply = %+v; %s lost", res, tt.kept)
			}
		})
	}
}

func TestUpdateRemovedUpstream(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	e.add(url)
	if err := os.RemoveAll(filepath.Join(dir, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "drop alpha")
	if u := e.check("alpha", "beta"); !slices.Equal(states(u), []UpdateState{RemovedUpstream, UpToDate}) {
		t.Errorf("states = %v", states(u))
	}
}

func TestUpdateAnnotatedTag(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	tagged := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "tag", "-a", "v1", "-m", "v1")
	e.add(url + "#v1")
	rec := e.lock().Skills["alpha"]
	if rec.Source.Ref != "v1" || rec.Commit != tagged {
		t.Fatalf("record = %+v", rec)
	}

	// The branch moves on; the tag does not.
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	if u := e.check("alpha"); u.Checks[0].State != UpToDate || u.Checks[0].Commit != tagged {
		t.Fatalf("check = %+v", u.Checks[0])
	}

	runGit(t, dir, "tag", "-a", "-f", "v1", "-m", "v1 again")
	u := e.check("alpha")
	if u.Checks[0].State != Available || u.Checks[0].Commit == tagged {
		t.Fatalf("moved tag: %+v", u.Checks[0])
	}
	if res := e.svc.ApplyUpdates(u, nil, false); !res[0].Updated {
		t.Fatalf("apply = %+v", res)
	}
	if !strings.Contains(readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md")), "v2") {
		t.Error("tag content not installed")
	}
}

func TestUpdateLocalSource(t *testing.T) {
	e := newEnv(t)
	dir := e.path("dev", "my-tool")
	writeSkill(t, dir, "my-tool", "v1")
	e.add(dir)

	if u := e.check("my-tool"); u.Checks[0].State != UpToDate {
		t.Fatalf("check = %+v", u.Checks[0])
	}
	writeSkill(t, dir, "my-tool", "v2")
	u := e.check("my-tool")
	if u.Checks[0].State != Available || u.Checks[0].Commit != "" {
		t.Fatalf("check = %+v", u.Checks[0])
	}
	if res := e.svc.ApplyUpdates(u, nil, false); !res[0].Updated {
		t.Fatalf("apply = %+v", res)
	}
	if !strings.Contains(readFile(t, filepath.Join(e.libDir("my-tool"), "SKILL.md")), "v2") {
		t.Error("not updated")
	}
	if rec := e.lock().Skills["my-tool"]; rec.Commit != "" {
		t.Errorf("local record got a commit: %+v", rec)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if u := e.check("my-tool"); u.Checks[0].State != RemovedUpstream {
		t.Errorf("check = %+v", u.Checks[0])
	}
}

func TestUpdateSkill(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "skills/alpha"})
	e.add(url)
	lib := e.libDir("alpha")
	update := func(force bool) (UpdateState, error) {
		return e.svc.UpdateSkill(context.Background(), e.skill("alpha"), force)
	}

	if state, err := update(false); state != UpToDate || err != nil {
		t.Fatalf("unchanged upstream: %s, %v", state, err)
	}

	writeSkill(t, filepath.Join(dir, "skills", "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	if state, err := update(false); state != Available || err != nil {
		t.Fatalf("new commit: %s, %v", state, err)
	}
	if !strings.Contains(readFile(t, filepath.Join(lib, "SKILL.md")), "v2") {
		t.Error("v2 not installed")
	}

	writeSkill(t, filepath.Join(dir, "skills", "alpha"), "alpha", "v3")
	commitAll(t, dir, "v3")
	writeFile(t, filepath.Join(lib, "mine.md"), "local")
	state, err := update(false)
	if state != Available {
		t.Fatalf("local edit: state = %s", state)
	}
	wantErr(t, err, ErrModified, "alpha")
	if !exists(filepath.Join(lib, "mine.md")) {
		t.Fatal("the local edit was replaced without force")
	}
	if state, err := update(true); state != Available || err != nil {
		t.Fatalf("forced: %s, %v", state, err)
	}
	if !strings.Contains(readFile(t, filepath.Join(lib, "SKILL.md")), "v3") {
		t.Error("v3 not installed")
	}

	if err := os.RemoveAll(filepath.Join(dir, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "drop alpha")
	if state, err := update(false); state != RemovedUpstream || err != nil {
		t.Errorf("removed upstream: %s, %v", state, err)
	}
}

func TestUpdateNotUpdatableAndFailed(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeSkill(t, e.libDir("handmade"), "handmade", "")
	coll := e.path("coll")
	writeSkill(t, filepath.Join(coll, "linked"), "linked", "")
	if _, err := e.svc.LinkLocal(coll); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, e.path("moved")); err != nil {
		t.Fatal(err)
	}

	u := e.check("alpha", "handmade", "linked")
	if !slices.Equal(states(u), []UpdateState{CheckFailed, NotUpdatable, NotUpdatable}) {
		t.Fatalf("states = %v", states(u))
	}
	if u.Checks[0].Err == nil {
		t.Error("failed check without error")
	}
}

func TestUpdateFailedSwapRestores(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	u := e.check("alpha")
	before := e.lock().Skills["alpha"]
	content := readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md"))

	rename = func(string, string) error { return errors.New("disk full") }
	defer func() { rename = os.Rename }()
	res := e.svc.ApplyUpdates(u, nil, false)
	if res[0].Updated || res[0].Err == nil || !strings.Contains(res[0].Err.Error(), "disk full") {
		t.Fatalf("apply = %+v", res)
	}
	if got := readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md")); got != content {
		t.Error("old version not restored")
	}
	if after := e.lock().Skills["alpha"]; after != before {
		t.Errorf("record changed: %+v", after)
	}
	if l := leftovers(t, e.svc.paths.Skills); len(l) > 0 {
		t.Errorf("leftovers: %v", l)
	}
}

func TestUpdateStaleCheck(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	stale := e.check("alpha")

	// Another process updates to v3 after the check.
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v3")
	commitAll(t, dir, "v3")
	if res := e.svc.ApplyUpdates(e.check("alpha"), nil, false); !res[0].Updated {
		t.Fatalf("apply = %+v", res)
	}
	before := e.lock().Skills["alpha"]

	for _, force := range []bool{false, true} {
		res := e.svc.ApplyUpdates(stale, nil, force)
		if res[0].Updated || res[0].Err == nil || !strings.Contains(res[0].Err.Error(), "changed since it was checked") {
			t.Errorf("force %v: apply = %+v", force, res)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md")), "v3") {
		t.Error("v3 replaced by the stale check")
	}
	if after := e.lock().Skills["alpha"]; after != before {
		t.Errorf("record changed: %+v", after)
	}
}

func TestUpdateRefusesSymlinkOnTheWay(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	// alpha recorded inside a real collection.
	mkdirAll(t, e.libDir("pack"))
	if err := os.Rename(e.libDir("alpha"), e.libDir("pack/alpha")); err != nil {
		t.Fatal(err)
	}
	lock := e.lock()
	lock.Skills["pack/alpha"] = lock.Skills["alpha"]
	delete(lock.Skills, "alpha")
	if err := store.SaveLock(e.svc.paths.Lock, lock); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	u := e.check("pack/alpha")
	if u.Checks[0].State != Available {
		t.Fatalf("check = %+v", u.Checks[0])
	}

	// The collection is replaced by a link to a folder outside the library.
	outside := e.path("outside", "pack")
	mkdirAll(t, filepath.Dir(outside))
	if err := os.Rename(e.libDir("pack"), outside); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, e.libDir("pack"))
	res := e.svc.ApplyUpdates(u, nil, true)
	if res[0].Updated || res[0].Err == nil || !strings.Contains(res[0].Err.Error(), "not a folder in the library") {
		t.Fatalf("apply = %+v", res)
	}
	if got := readFile(t, filepath.Join(outside, "alpha", "SKILL.md")); !strings.Contains(got, "alpha body") {
		t.Errorf("files behind the link changed: %q", got)
	}
	if l := leftovers(t, outside); len(l) > 0 {
		t.Errorf("leftovers: %v", l)
	}
}

func TestCheckUpdatesCanceled(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u, err := e.svc.CheckUpdates(ctx, []Skill{e.skill("alpha")}, nil)
	if !errors.Is(err, context.Canceled) || u != nil {
		t.Errorf("CheckUpdates = %v, %v", u, err)
	}
}

func TestUpdatesCloseRemovesCheckouts(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeSkill(t, filepath.Join(dir, "alpha"), "alpha", "v2")
	commitAll(t, dir, "v2")
	u := e.check("alpha")
	staged := u.Checks[0].staged
	if !exists(staged) {
		t.Fatal("no staged checkout")
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	if exists(staged) {
		t.Error("checkout kept after Close")
	}
}
