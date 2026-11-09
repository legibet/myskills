package git

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// setup isolates git from the user's configuration.
func setup(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Test remotes serve partial clones, as GitHub does. Local transports
	// drop GIT_CONFIG_* variables, so this goes in the global config.
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[uploadpack]\n\tallowFilter = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// tempDir returns a new temporary directory with symlinks resolved, so it
// compares equal to the paths git prints.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// gitT runs git in dir and returns its trimmed output.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	gitT(t, dir, "init", "-q", "-b", "main")
	return dir
}

// commit writes name with content in repo, commits it and returns the commit.
func commit(t *testing.T, repo, name, content string) string {
	t.Helper()
	p := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "add", "--", name)
	gitT(t, repo, "commit", "-q", "-m", name)
	return gitT(t, repo, "rev-parse", "HEAD")
}

// remote is a repository with branches and tags pointing at distinct commits.
type remote struct {
	url                                   string
	main, feature, myFeature, tagged, dup string
}

func newRemote(t *testing.T) remote {
	t.Helper()
	repo := newRepo(t)
	r := remote{url: "file://" + filepath.ToSlash(repo)}
	r.tagged = commit(t, repo, "a", "a")
	gitT(t, repo, "tag", "light")
	gitT(t, repo, "tag", "-a", "annotated", "-m", "annotated")
	gitT(t, repo, "tag", "dup")
	r.feature = commit(t, repo, "b", "b")
	gitT(t, repo, "branch", "feature")
	r.myFeature = commit(t, repo, "c", "c")
	gitT(t, repo, "branch", "my-feature")
	// Refs that ls-remote's tail matching also returns for "feature" and "HEAD".
	gitT(t, repo, "branch", "nested/refs/heads/feature")
	gitT(t, repo, "update-ref", "refs/remotes/origin/HEAD", "HEAD")
	r.dup = commit(t, repo, "d", "d")
	gitT(t, repo, "branch", "dup")
	r.main = commit(t, repo, "e", "e")
	return r
}

func TestRemoteCommit(t *testing.T) {
	setup(t)
	r := newRemote(t)
	tests := []struct {
		ref, want string
	}{
		{"", r.main},
		{"main", r.main},
		{"feature", r.feature},
		{"my-feature", r.myFeature},
		{"light", r.tagged},
		{"annotated", r.tagged}, // peeled to the commit, not the tag object
		{"dup", r.dup},          // the branch wins over the tag
	}
	for _, tt := range tests {
		got, err := RemoteCommit(t.Context(), r.url, tt.ref)
		if err != nil {
			t.Errorf("RemoteCommit(%q): %v", tt.ref, err)
			continue
		}
		if got != tt.want {
			t.Errorf("RemoteCommit(%q) = %s, want %s", tt.ref, got, tt.want)
		}
	}

	for _, ref := range []string{"missing", "eature", "heads/feature"} {
		_, err := RemoteCommit(t.Context(), r.url, ref)
		want := `ref "` + ref + `" not found in ` + r.url
		if err == nil || err.Error() != want {
			t.Errorf("RemoteCommit(%q) error = %v, want %q", ref, err, want)
		}
	}
}

func TestRemoteCommitEmptyRepo(t *testing.T) {
	setup(t)
	url := "file://" + filepath.ToSlash(newRepo(t))
	_, err := RemoteCommit(t.Context(), url, "")
	if err == nil || !strings.Contains(err.Error(), `ref "HEAD" not found`) {
		t.Errorf("error = %v", err)
	}
}

func TestRemoteCommitMissingRepo(t *testing.T) {
	setup(t)
	url := "file://" + filepath.ToSlash(filepath.Join(tempDir(t), "missing"))
	_, err := RemoteCommit(t.Context(), url, "")
	if err == nil || !strings.HasPrefix(err.Error(), "git ls-remote "+url+": ") ||
		!strings.Contains(err.Error(), "does not appear to be a git repository") {
		t.Errorf("error = %v", err)
	}
}

func TestClone(t *testing.T) {
	setup(t)
	r := newRemote(t)
	tests := []struct {
		ref, want, file string
	}{
		{"", r.main, "e"},
		{"feature", r.feature, "b"},
		{"annotated", r.tagged, "a"},
		{"dup", r.dup, "d"},
	}
	for _, tt := range tests {
		dir := filepath.Join(tempDir(t), "clone")
		got, err := Clone(t.Context(), r.url, tt.ref, dir)
		if err != nil {
			t.Errorf("Clone(%q): %v", tt.ref, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Clone(%q) = %s, want %s", tt.ref, got, tt.want)
		}
		tree, err := Tree(t.Context(), dir)
		if err != nil || tree[tt.file] != File {
			t.Errorf("Clone(%q): tree %v, %v; want it to list %s", tt.ref, tree, err, tt.file)
		}
		if got := checkedOut(t, dir); len(got) > 0 {
			t.Errorf("Clone(%q) checked out %v", tt.ref, got)
		}
		if n := gitT(t, dir, "rev-list", "--count", "HEAD"); n != "1" {
			t.Errorf("Clone(%q): %s commits, want a shallow clone", tt.ref, n)
		}
	}
}

// checkedOut returns the files in the work tree at dir, slash-separated.
func checkedOut(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() == ".git" {
			return cmp.Or(err, fs.SkipDir)
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func TestCheckout(t *testing.T) {
	setup(t)
	repo := newRepo(t)
	// Characters that sparse-checkout patterns treat specially.
	odd := "#a [b]!c d"
	if runtime.GOOS != "windows" {
		odd += `*?\`
	}
	commit(t, repo, "README", "readme")
	commit(t, repo, "skills/one/SKILL.md", "one")
	commit(t, repo, "skills/one/big.txt", "big")
	commit(t, repo, "skills/one-more/SKILL.md", "more")
	commit(t, repo, "skills/"+odd+"/SKILL.md", "odd")
	// A symlink to README, committed as such on every OS: its blob is the
	// target path.
	targetFile := filepath.Join(tempDir(t), "target")
	if err := os.WriteFile(targetFile, []byte("README"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := gitT(t, repo, "hash-object", "-w", targetFile)
	gitT(t, repo, "update-index", "--add", "--cacheinfo", "120000,"+target+",link")
	gitT(t, repo, "commit", "-q", "-m", "link")

	dir := filepath.Join(tempDir(t), "clone")
	if _, err := Clone(t.Context(), "file://"+filepath.ToSlash(repo), "", dir); err != nil {
		t.Fatal(err)
	}
	// Only the file list was downloaded.
	if missing := strings.Count(gitT(t, dir, "rev-list", "--objects", "--missing=print", "HEAD"), "\n?"); missing != 6 {
		t.Errorf("%d contents missing after Clone, want 6", missing)
	}

	tree, err := Tree(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	wantTree := map[string]Kind{
		"README": File, "link": Link,
		"skills": Dir, "skills/one": Dir, "skills/one-more": Dir, "skills/" + odd: Dir,
		"skills/one/SKILL.md": File, "skills/one/big.txt": File,
		"skills/one-more/SKILL.md": File, "skills/" + odd + "/SKILL.md": File,
	}
	if !maps.Equal(tree, wantTree) {
		t.Errorf("Tree = %v, want %v", tree, wantTree)
	}

	if err := Checkout(t.Context(), dir, []string{"skills/one", "skills/" + odd + "/SKILL.md"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"skills/" + odd + "/SKILL.md", "skills/one/SKILL.md", "skills/one/big.txt"}
	if got := checkedOut(t, dir); !slices.Equal(got, want) {
		t.Errorf("checked out %q, want %q", got, want)
	}
	if err := Checkout(t.Context(), dir, []string{"."}); err != nil {
		t.Fatal(err)
	}
	if got := checkedOut(t, dir); len(got) != 6 {
		t.Errorf("checked out %q after Checkout(.), want every file", got)
	}
}

func TestCloneFailureRemovesDir(t *testing.T) {
	setup(t)
	r := newRemote(t)
	dir := filepath.Join(tempDir(t), "clone")

	_, err := Clone(t.Context(), r.url, "missing", dir)
	if err == nil || err.Error() != "git clone "+r.url+": Remote branch missing not found in upstream origin" {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir left behind: %v", err)
	}

	missing := "file://" + filepath.ToSlash(filepath.Join(tempDir(t), "missing"))
	if _, err := Clone(t.Context(), missing, "", dir); err == nil {
		t.Error("clone of a missing repository succeeded")
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir left behind: %v", err)
	}
}

func TestCloneCanceled(t *testing.T) {
	setup(t)
	r := newRemote(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Clone(ctx, r.url, "", filepath.Join(tempDir(t), "clone"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestNotInstalled(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := RemoteCommit(t.Context(), "file:///x", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoteCommit error = %v, want ErrNotFound", err)
	}
	if _, err := Toplevel(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Toplevel error = %v, want ErrNotFound", err)
	}
}

func TestToplevel(t *testing.T) {
	setup(t)
	repo := newRepo(t)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{repo, sub} {
		got, err := Toplevel(dir)
		if err != nil || got != repo {
			t.Errorf("Toplevel(%s) = %q, %v; want %q", dir, got, err, repo)
		}
	}

	bare := tempDir(t)
	gitT(t, bare, "init", "-q", "--bare")
	for _, dir := range []string{tempDir(t), filepath.Join(repo, ".git"), bare} {
		if _, err := Toplevel(dir); !errors.Is(err, ErrNotRepo) {
			t.Errorf("Toplevel(%s) error = %v, want ErrNotRepo", dir, err)
		}
	}
}

func TestGitPathAndWorktrees(t *testing.T) {
	setup(t)
	repo := newRepo(t)
	commit(t, repo, "a", "a")
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(tempDir(t), "wt")
	gitT(t, repo, "worktree", "add", "-q", "-b", "wt", wt)

	exclude := filepath.Join(repo, ".git", "info", "exclude")
	for _, dir := range []string{repo, sub, wt} {
		got, err := GitPath(dir, "info/exclude")
		if err != nil || got != exclude {
			t.Errorf("GitPath(%s) = %q, %v; want %q", dir, got, err, exclude)
		}
	}
	if _, err := GitPath(tempDir(t), "info/exclude"); !errors.Is(err, ErrNotRepo) {
		t.Errorf("GitPath outside a repository: error = %v, want ErrNotRepo", err)
	}

	for _, dir := range []string{repo, wt} {
		got, err := Worktrees(dir)
		if err != nil || !slices.Equal(got, []string{repo, wt}) {
			t.Errorf("Worktrees(%s) = %q, %v", dir, got, err)
		}
	}
	if _, err := Worktrees(tempDir(t)); !errors.Is(err, ErrNotRepo) {
		t.Errorf("Worktrees outside a repository: error = %v, want ErrNotRepo", err)
	}
}

func TestWorktreesBare(t *testing.T) {
	setup(t)
	src := newRepo(t)
	commit(t, src, "a", "a")
	bare := filepath.Join(tempDir(t), "bare.git")
	gitT(t, src, "clone", "-q", "--bare", src, bare)
	wt := filepath.Join(tempDir(t), "wt")
	gitT(t, bare, "worktree", "add", "-q", wt, "main")

	got, err := Worktrees(wt)
	if err != nil || !slices.Equal(got, []string{wt}) {
		t.Errorf("Worktrees = %q, %v; want only %s", got, err, wt)
	}
}

func TestTracked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`Windows file names cannot contain "*"`)
	}
	setup(t)
	repo := newRepo(t)
	commit(t, repo, ".agents/skills/tdd/SKILL.md", "x")
	commit(t, repo, "g*/f", "x")
	if err := os.Symlink("../.agents/skills/tdd", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "add", "link")
	gitT(t, repo, "commit", "-q", "-m", "link")
	if err := os.WriteFile(filepath.Join(repo, "untracked"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{".agents/skills/tdd", true},
		{".agents/skills/tdd/SKILL.md", true},
		{filepath.Join(repo, ".agents", "skills", "tdd"), true},
		{".agents/skills/other", false},
		{".agents/skills/td", false},
		{"link", true},
		{"untracked", false},
		{"g*", true},
		{"g*/f", true},
		{"g?", false}, // pathspec magic is off: no glob matching
		{"missing", false},
	}
	for _, tt := range tests {
		got, err := Tracked(repo, tt.path)
		if err != nil || got != tt.want {
			t.Errorf("Tracked(%q) = %v, %v; want %v", tt.path, got, err, tt.want)
		}
	}

	if _, err := Tracked(tempDir(t), "x"); !errors.Is(err, ErrNotRepo) {
		t.Errorf("Tracked outside a repository: error = %v, want ErrNotRepo", err)
	}
}

func TestMessage(t *testing.T) {
	tests := []struct {
		stderr, want string
	}{
		{"", ""},
		{"fatal: Remote branch x not found in upstream origin\n", "Remote branch x not found in upstream origin"},
		{
			"warning: Could not find remote branch x to clone.\nfatal: Remote branch x not found in upstream origin\n",
			"Remote branch x not found in upstream origin",
		},
		{
			"fatal: '/x' does not appear to be a git repository\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"'/x' does not appear to be a git repository",
		},
		{
			"git@github.com: Permission denied (publickey).\r\nfatal: Could not read from remote repository.\n\nPlease make sure…\n",
			"git@github.com: Permission denied (publickey). Could not read from remote repository.",
		},
		{"hint: something\nerror: pathspec 'x' did not match\n", "pathspec 'x' did not match"},
		{"something odd\nand more\n", "something odd"},
	}
	for _, tt := range tests {
		if got := message(tt.stderr); got != tt.want {
			t.Errorf("message(%q) = %q, want %q", tt.stderr, got, tt.want)
		}
	}
}
