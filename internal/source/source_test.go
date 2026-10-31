package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	abs := func(p string) string {
		t.Helper()
		p, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	tests := []struct {
		in, want string
	}{
		{".", cwd},
		{"..", filepath.Dir(cwd)},
		{"./x", filepath.Join(cwd, "x")},
		{"  ./x/  ", filepath.Join(cwd, "x")},
		{"../x/../y", filepath.Join(filepath.Dir(cwd), "y")},
		{"/abs/x/", abs("/abs/x")},
		{"~", home},
		{"~/skills/x", filepath.Join(home, "skills", "x")},
		{"./x#main", filepath.Join(cwd, "x#main")}, // no ref or skill syntax for paths
		{"./x@tdd", filepath.Join(cwd, "x@tdd")},
		{`C:\Users\me\x`, filepath.Clean(`C:\Users\me\x`)},
		{"c:/Users/me/../x", filepath.Clean("c:/Users/me/../x")},
		{`\\server\share\x`, filepath.Clean(`\\server\share\x`)},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		want := Spec{Kind: Local, Input: strings.TrimSpace(tt.in), Path: tt.want}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, want)
		}
	}
}

func TestParseGit(t *testing.T) {
	const gh = "https://github.com/owner/repo.git"
	tests := []struct {
		in   string
		want Spec // Kind and Input are checked separately
	}{
		// Shorthand.
		{"owner/repo", Spec{URL: gh}},
		{"owner/repo.git", Spec{URL: gh}},
		{"owner/repo/", Spec{URL: gh}},
		{"owner/repo/skills/tdd", Spec{URL: gh, Subdir: "skills/tdd"}},
		{"owner/repo//skills/./tdd/", Spec{URL: gh, Subdir: "skills/tdd"}},
		{"owner/repo@tdd", Spec{URL: gh, Skill: "tdd"}},
		{"owner/repo/skills@tdd", Spec{URL: gh, Subdir: "skills", Skill: "tdd"}},
		{"owner/repo#v1", Spec{URL: gh, Ref: "v1"}},
		{"owner/repo@tdd#v1.2", Spec{URL: gh, Ref: "v1.2", Skill: "tdd"}},
		{"owner/repo/packages/@scope/x", Spec{URL: gh, Subdir: "packages/@scope/x"}},
		{"Owner.Name/repo_x-y.z", Spec{URL: "https://github.com/Owner.Name/repo_x-y.z.git"}},

		// GitHub URLs.
		{"https://github.com/owner/repo", Spec{URL: gh}},
		{"http://www.github.com/owner/repo.git/", Spec{URL: gh}},
		{"https://GitHub.com/owner/repo?tab=readme-ov-file", Spec{URL: gh}},
		{"https://github.com/owner/repo/tree/main", Spec{URL: gh, Ref: "main"}},
		{"https://github.com/owner/repo/tree/main/skills/tdd", Spec{URL: gh, Ref: "main", Subdir: "skills/tdd"}},
		{"https://github.com/owner/repo/tree/main/skills/tdd#v2", Spec{URL: gh, Ref: "v2", Subdir: "skills/tdd"}},
		{"https://github.com/owner/repo/blob/main/skills/tdd/SKILL.md", Spec{URL: gh, Ref: "main", Subdir: "skills/tdd"}},
		{"https://github.com/owner/repo/blob/v1/SKILL.md", Spec{URL: gh, Ref: "v1"}},
		{"https://github.com/owner/repo/tree/a%20b/x%23y", Spec{URL: gh, Ref: "a b", Subdir: "x#y"}},
		{"github.com/owner/repo/tree/dev/x", Spec{URL: gh, Ref: "dev", Subdir: "x"}},

		// GitLab URLs.
		{"https://gitlab.com/group/repo", Spec{URL: "https://gitlab.com/group/repo.git"}},
		{"https://gitlab.com/group/sub/repo.git", Spec{URL: "https://gitlab.com/group/sub/repo.git"}},
		{
			"https://gitlab.com/group/sub/repo/-/tree/main/skills/x",
			Spec{URL: "https://gitlab.com/group/sub/repo.git", Ref: "main", Subdir: "skills/x"},
		},
		{
			"https://gitlab.com/group/repo/-/blob/v1/a/SKILL.md#dev",
			Spec{URL: "https://gitlab.com/group/repo.git", Ref: "dev", Subdir: "a"},
		},

		// Other remotes, kept as given.
		{"git@github.com:owner/repo.git", Spec{URL: "git@github.com:owner/repo.git"}},
		{"git@github.com:owner/repo.git#dev", Spec{URL: "git@github.com:owner/repo.git", Ref: "dev"}},
		{"user@host.xz:path/to/repo", Spec{URL: "user@host.xz:path/to/repo"}},
		{"ssh://git@host.xz:2222/team/repo.git", Spec{URL: "ssh://git@host.xz:2222/team/repo.git"}},
		{"git://host.xz/team/repo.git#v1", Spec{URL: "git://host.xz/team/repo.git", Ref: "v1"}},
		{"file:///tmp/repo", Spec{URL: "file:///tmp/repo"}},
		{"file:///tmp/repo#main", Spec{URL: "file:///tmp/repo", Ref: "main"}},
		{"https://example.com/team/repo.git", Spec{URL: "https://example.com/team/repo.git"}},
		{"https://codeberg.org/team/repo#v1", Spec{URL: "https://codeberg.org/team/repo", Ref: "v1"}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		want := tt.want
		want.Kind, want.Input = Git, tt.in
		if got != want {
			t.Errorf("Parse(%q) =\n  %+v\nwant\n  %+v", tt.in, got, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		in, err string
	}{
		{"", "empty source"},
		{"   ", "empty source"},
		{"swiftui", "unrecognized source"},
		{"react hooks", "unrecognized source"},
		{"owner/re po", "unrecognized source"},
		{"owner/repo@", "unrecognized source"},
		{"owner/repo@a@b", "unrecognized source"},
		{"own!er/repo", "unrecognized source"},
		{"owner/.git", "unrecognized source"},
		{"~user/x", "unrecognized source"},
		{"ftp://host/repo", "unrecognized source"},
		{"owner/repo#", "empty ref"},
		{"owner/repo/../x", `must not contain ".."`},
		{"https://github.com/owner/repo/tree/main/../x", `must not contain ".."`},
		{"https://github.com/owner", "use https://github.com/owner/repo"},
		{"https://github.com/owner/repo/issues", "use https://github.com/owner/repo"},
		{"https://github.com/owner/repo/tree", "use https://github.com/owner/repo"},
		{"https://github.com/owner/repo/blob/main", "use https://github.com/owner/repo"},
		{"https://github.com/owner/repo@tdd", "use https://github.com/owner/repo"},
		{"https://gitlab.com/repo", "use https://gitlab.com/group/repo"},
		{"https://gitlab.com/group/repo/-/issues/1", "use https://gitlab.com/group/repo"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.in)
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("Parse(%q) error = %v, want %q", tt.in, err, tt.err)
		}
	}
}

func TestIsExplicit(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"owner/repo", true},
		{" owner/repo ", true},
		{"owner/repo@tdd", true},
		{"https://github.com/owner/repo", true},
		{"git@github.com:owner/repo.git", true},
		{"file:///tmp/repo", true},
		{"./x", true},
		{"~", true},
		{"/tmp", true},
		{`C:\x`, true},
		{"", false},
		{"swiftui", false},
		{"react hooks", false},
		{"owner/repo extra", false},
		{"./my dir", false},
		{"https://github.com/owner", false},
	}
	for _, tt := range tests {
		if got := IsExplicit(tt.in); got != tt.want {
			t.Errorf("IsExplicit(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		url, want string
	}{
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"https://gitlab.com/group/sub/repo.git", "group/sub/repo"},
		{"git@gitlab.com:group/repo.git", "group/repo"},
		{"https://example.com/team/repo.git", "example.com/team/repo"},
		{"https://user@example.com/repo.git/", "example.com/repo"},
		{"git@host.xz:team/repo.git", "host.xz:team/repo"},
		{"ssh://git@host.xz:2222/repo.git", "host.xz:2222/repo"},
		{"file:///tmp/repo/", "/tmp/repo"},
		{"file:///tmp/a@b/repo", "/tmp/a@b/repo"},
	}
	for _, tt := range tests {
		if got := Label(tt.url); got != tt.want {
			t.Errorf("Label(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestWebURL(t *testing.T) {
	tests := []struct {
		url, ref, path, want string
	}{
		{"https://github.com/owner/repo.git", "", "", "https://github.com/owner/repo"},
		{"https://github.com/owner/repo.git", "v1", "", "https://github.com/owner/repo/tree/v1"},
		{"https://github.com/owner/repo.git", "", "skills/tdd", "https://github.com/owner/repo/tree/HEAD/skills/tdd"},
		{"https://github.com/owner/repo.git", "dev", "/skills/tdd/", "https://github.com/owner/repo/tree/dev/skills/tdd"},
		{"git@github.com:owner/repo.git", "feature/x", "a b", "https://github.com/owner/repo/tree/feature/x/a%20b"},
		{"https://gitlab.com/group/sub/repo.git", "", "", "https://gitlab.com/group/sub/repo"},
		{"https://gitlab.com/group/sub/repo.git", "", "x", "https://gitlab.com/group/sub/repo/-/tree/HEAD/x"},
		{"git@gitlab.com:group/repo.git", "v1", "", "https://gitlab.com/group/repo/-/tree/v1"},
		{"https://example.com/team/repo.git", "", "x", ""},
		{"file:///tmp/repo", "", "", ""},
	}
	for _, tt := range tests {
		if got := WebURL(tt.url, tt.ref, tt.path); got != tt.want {
			t.Errorf("WebURL(%q, %q, %q) = %q, want %q", tt.url, tt.ref, tt.path, got, tt.want)
		}
	}
}
