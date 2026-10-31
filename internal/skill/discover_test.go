package skill

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func discover(t *testing.T, root string) []string {
	t.Helper()
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func joinAll(root string, rels ...string) []string {
	var out []string
	for _, rel := range rels {
		out = append(out, filepath.Join(root, filepath.FromSlash(rel)))
	}
	return out
}

func TestDiscoverRootSkill(t *testing.T) {
	root := tempDir(t)
	writeSkill(t, root)
	writeSkill(t, filepath.Join(root, "nested"))
	if got, want := discover(t, root), []string{root}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverNested(t *testing.T) {
	root := tempDir(t)
	writeSkill(t, filepath.Join(root, "b", "c"))
	writeSkill(t, filepath.Join(root, "a"))
	writeSkill(t, filepath.Join(root, "a", "inner")) // inside a candidate: not descended
	writeSkill(t, filepath.Join(root, ".claude", "skills", "hidden"))
	writeSkill(t, filepath.Join(root, "node_modules", "pkg"))
	writeSkill(t, filepath.Join(root, "x", "node_modules", "pkg"))
	writeSkill(t, filepath.Join(root, ".git", "skill"))
	writeFile(t, filepath.Join(root, "notes", "README.md"), "no skill here")
	// A directory named SKILL.md does not make a skill.
	if err := os.MkdirAll(filepath.Join(root, "d", FileName), 0o755); err != nil {
		t.Fatal(err)
	}

	want := joinAll(root, ".claude/skills/hidden", "a", "b/c")
	if got := discover(t, root); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverDepthLimit(t *testing.T) {
	root := tempDir(t)
	writeSkill(t, filepath.Join(root, "1", "2", "3", "4", "5", "6"))
	writeSkill(t, filepath.Join(root, "a", "2", "3", "4", "5", "6", "7"))
	want := joinAll(root, "1/2/3/4/5/6")
	if got := discover(t, root); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverSymlinks(t *testing.T) {
	root := tempDir(t)
	outside := tempDir(t)
	writeSkill(t, filepath.Join(root, "skills", "a"))
	writeSkill(t, filepath.Join(root, "store", "b"))
	writeSkill(t, filepath.Join(outside, "evil"))

	symlink(t, filepath.Join(root, "skills", "a"), filepath.Join(root, "alias")) // same skill twice
	symlink(t, "store", filepath.Join(root, "linked"))                           // dir with skills, also walked directly
	symlink(t, outside, filepath.Join(root, "escape"))                           // leaves root: ignored
	symlink(t, filepath.Join(outside, "evil"), filepath.Join(root, "evil"))      // leaves root: ignored
	symlink(t, "missing", filepath.Join(root, "broken"))
	symlink(t, "..", filepath.Join(root, "skills", "up")) // cycle

	want := joinAll(root, "skills/a", "store/b")
	if got := discover(t, root); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverOnlyThroughSymlink(t *testing.T) {
	root := tempDir(t)
	// The real dir is skipped by name, but reachable through a link inside root;
	// the result is the real path.
	writeSkill(t, filepath.Join(root, "node_modules", "pkg"))
	symlink(t, filepath.Join("node_modules", "pkg"), filepath.Join(root, "pkg"))
	want := joinAll(root, "node_modules/pkg")
	if got := discover(t, root); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverShallowestVisitWins(t *testing.T) {
	root := tempDir(t)
	// "a/…/5/link" reaches "z" at depth 6, where it may not be descended; "z" is
	// also at depth 1, from where its skill at depth 2 must still be found.
	symlink(t, filepath.Join(root, "z"), filepath.Join(root, "a", "2", "3", "4", "5", "link"))
	writeSkill(t, filepath.Join(root, "z", "s"))
	want := joinAll(root, "z/s")
	if got := discover(t, root); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverSymlinkedRoot(t *testing.T) {
	root := tempDir(t)
	writeSkill(t, filepath.Join(root, "a"))
	link := filepath.Join(tempDir(t), "link")
	symlink(t, root, link)
	want := joinAll(root, "a")
	if got := discover(t, link); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// A root that is itself a skill, through a link.
	skillLink := filepath.Join(tempDir(t), "s")
	symlink(t, filepath.Join(root, "a"), skillLink)
	if got := discover(t, skillLink); !slices.Equal(got, want) {
		t.Errorf("skill root: got %v, want %v", got, want)
	}
}

func TestDiscoverRelativeRoot(t *testing.T) {
	root := tempDir(t)
	writeSkill(t, filepath.Join(root, "a"))
	t.Chdir(root)
	if got, want := discover(t, "."), joinAll(root, "a"); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	root := tempDir(t)
	writeSkill(t, filepath.Join(root, "a"))
	locked := filepath.Join(root, "locked")
	writeSkill(t, filepath.Join(locked, "b"))
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// An unreadable subdirectory is skipped.
	if got, want := discover(t, root), joinAll(root, "a"); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// An unreadable root is an error.
	if _, err := Discover(locked); err == nil {
		t.Error("unreadable root: no error")
	}
	if _, err := Discover(filepath.Join(root, "missing")); err == nil {
		t.Error("missing root: no error")
	}
}
