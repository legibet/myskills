package skill

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustHash(t *testing.T, dir string) string {
	t.Helper()
	h, err := Hash(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// goldenTree builds a small fixed tree. "a-b" sorts between "a" and "a/c",
// unlike a per-directory walk.
func goldenTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "hello\n")
	writeFile(t, filepath.Join(dir, "a", "c"), "c\n")
	writeFile(t, filepath.Join(dir, "a-b"), "ab\n")
	writeFile(t, filepath.Join(dir, "bin", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, "SKILL.md", filepath.Join(dir, "link"))
	return dir
}

func TestHashGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no executable bit")
	}
	// Computed independently from the format described on Hash.
	const want = "v1:1598e81fb07139f7a44d07784cd2266ed0f3a3ba54d9d126c7464e6c94322d0f"
	if got := mustHash(t, goldenTree(t)); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestHashEmptyDir(t *testing.T) {
	// SHA-256 of no input.
	const want = "v1:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := mustHash(t, t.TempDir()); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestHashAmbiguity(t *testing.T) {
	// Without length prefixes these two trees could serialize to the same bytes.
	one := t.TempDir()
	writeFile(t, filepath.Join(one, "a"), "xFb\x00y")
	two := t.TempDir()
	writeFile(t, filepath.Join(two, "a"), "x")
	writeFile(t, filepath.Join(two, "b"), "y")
	if mustHash(t, one) == mustHash(t, two) {
		t.Error("different trees hash the same")
	}
}

func TestHashChanges(t *testing.T) {
	base := mustHash(t, goldenTree(t))
	tests := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{"content", func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "a-b"), "ab!\n") }},
		{"rename", func(t *testing.T, dir string) {
			if err := os.Rename(filepath.Join(dir, "a-b"), filepath.Join(dir, "a-c")); err != nil {
				t.Fatal(err)
			}
		}},
		{"new empty dir", func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "more"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"link target", func(t *testing.T, dir string) {
			link := filepath.Join(dir, "link")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			symlink(t, "./SKILL.md", link)
		}},
		{"link replaced by file", func(t *testing.T, dir string) {
			link := filepath.Join(dir, "link")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			writeFile(t, link, "SKILL.md")
		}},
		{"exec bit cleared", func(t *testing.T, dir string) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows keeps no executable bit")
			}
			if err := os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"exec bit set", func(t *testing.T, dir string) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows keeps no executable bit")
			}
			if err := os.Chmod(filepath.Join(dir, "a-b"), 0o744); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := goldenTree(t)
			tt.change(t, dir)
			if mustHash(t, dir) == base {
				t.Error("hash did not change")
			}
		})
	}
}

func TestHashStable(t *testing.T) {
	base := mustHash(t, goldenTree(t))
	tests := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{"other exec bits", func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o711); err != nil {
				t.Fatal(err)
			}
		}},
		{"other permission bits", func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "a-b"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"ignored entries", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
			writeFile(t, filepath.Join(dir, ".DS_Store"), "x")
			writeFile(t, filepath.Join(dir, "a", ".DS_Store"), "x")
			writeFile(t, filepath.Join(dir, "a", ".git"), "gitdir: ../.git\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := goldenTree(t)
			tt.change(t, dir)
			if got := mustHash(t, dir); got != base {
				t.Errorf("hash changed: %s", got)
			}
		})
	}
}

func TestHashSymlinks(t *testing.T) {
	dir := goldenTree(t)
	want := mustHash(t, dir)

	// The root may be a symlink.
	link := filepath.Join(t.TempDir(), "link")
	symlink(t, dir, link)
	if got := mustHash(t, link); got != want {
		t.Errorf("through a symlinked root: got %s, want %s", got, want)
	}

	// A symlinked directory inside is hashed as a link, not walked.
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "f"), "1")
	symlink(t, outside, filepath.Join(dir, "ext"))
	before := mustHash(t, dir)
	writeFile(t, filepath.Join(outside, "f"), "2")
	if mustHash(t, dir) != before {
		t.Error("hash followed a symlinked directory")
	}
}

func TestHashErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Hash(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing dir: no error")
	}

	file := filepath.Join(dir, "file")
	writeFile(t, file, "x")
	if _, err := Hash(file); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("file: got %v", err)
	}

	sock := filepath.Join(dir, "s")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("cannot create a unix socket: %v", err)
	}
	defer func() { _ = l.Close() }()
	if _, err := Hash(dir); err == nil || err.Error() != "s: unsupported file type" {
		t.Errorf("socket: got %v", err)
	}
}
