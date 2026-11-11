// Package source parses the source specs accepted by `myskills add`.
package source

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

// Kind is the kind of a source.
type Kind int

const (
	Git Kind = iota
	Local
)

// Spec is a parsed source (docs/library.md).
type Spec struct {
	Kind  Kind
	Input string // as typed by the user

	// Git sources.
	URL    string // clone URL
	Ref    string // branch or tag; "" for the remote HEAD
	Subdir string // directory inside the repository to discover from; "" for the root

	// Local sources.
	Path string // absolute, cleaned

	// Skills restricts the source to the skills with these names (nil for all).
	Skills []string
}

var (
	// nameRe matches a GitHub owner or repository name and a GitLab path segment.
	nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// scpRe matches scp-like git remotes, user@host:path.
	scpRe = regexp.MustCompile(`^[^@/\s]+@([^@:/\s]+):(.+)$`)
)

// Parse parses a source spec. Local paths are only recognized in explicit
// forms ("./x", "../x", "/x", "~/x", Windows drive and UNC paths) and are made
// absolute against the current directory.
func Parse(input string) (Spec, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return Spec{}, errors.New("empty source")
	}
	if isLocal(s) {
		p, err := localPath(s)
		if err != nil {
			return Spec{}, err
		}
		return Spec{Kind: Local, Input: s, Path: p}, nil
	}

	base, ref, hasRef := strings.CutLast(s, "#")
	if hasRef && ref == "" {
		return Spec{}, fmt.Errorf("empty ref in source %q", s)
	}
	var (
		spec Spec
		err  error
	)
	switch {
	case strings.Contains(base, "://"):
		spec, err = parseURL(s, base)
	case scpRe.MatchString(base):
		spec = Spec{URL: base}
	case hasPrefixFold(base, "github.com/", "www.github.com/", "gitlab.com/", "www.gitlab.com/"):
		spec, err = parseURL(s, "https://"+base)
	default:
		spec, err = parseShorthand(s, base)
	}
	if err != nil {
		return Spec{}, err
	}
	spec.Kind = Git
	spec.Input = s
	if hasRef {
		spec.Ref = ref
	}
	return spec, nil
}

// IsExplicit reports whether input is unambiguously a source spec rather than
// a search query: a URL, a git remote, an explicit local path, or owner/repo
// shorthand. The TUI Find view uses it to offer browsing a source.
func IsExplicit(input string) bool {
	s := strings.TrimSpace(input)
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return false
	}
	_, err := Parse(s)
	return err == nil
}

// Label returns a short human label for a git URL: "owner/repo" for GitHub,
// the repository path for GitLab, otherwise the URL without scheme and ".git".
func Label(url string) string {
	if host, repo := hostRepo(url); (host == "github.com" || host == "gitlab.com") && repo != "" {
		return repo
	}
	s := url
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	// Drop a user@ before the host, but not an '@' in the path.
	if at := strings.Index(s, "@"); at >= 0 && !strings.Contains(s[:at], "/") {
		s = s[at+1:]
	}
	return strings.TrimSuffix(strings.TrimRight(s, "/"), ".git")
}

// RepoName returns the last segment of a git URL's repository path without
// ".git": "repo" for https://github.com/owner/repo.git; "" when there is none.
func RepoName(url string) string {
	if _, repo := hostRepo(url); repo != "" {
		return path.Base(repo)
	}
	return ""
}

// WebURL returns the browser URL of a skill directory in a GitHub or GitLab
// repository, or "" for other hosts.
func WebURL(url, ref, path string) string {
	host, repo := hostRepo(url)
	var tree string
	switch {
	case repo == "":
		return ""
	case host == "github.com":
		tree = "/tree/"
	case host == "gitlab.com":
		tree = "/-/tree/"
	default:
		return ""
	}
	web := "https://" + host + "/" + repo
	if path = strings.Trim(path, "/"); path != "" {
		if ref == "" {
			ref = "HEAD"
		}
		return web + tree + escapePath(ref) + "/" + escapePath(path)
	}
	if ref != "" {
		return web + tree + escapePath(ref)
	}
	return web
}

func isLocal(s string) bool {
	if s == "." || s == ".." || s == "~" || isWindowsPath(s) {
		return true
	}
	prefixes := []string{"./", "../", "/", "~/"}
	if runtime.GOOS == "windows" {
		prefixes = append(prefixes, `.\`, `..\`, `~\`)
	}
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

// isWindowsPath reports whether s is a Windows drive ("C:\x", "C:/x") or UNC
// (`\\server\share`) path.
func isWindowsPath(s string) bool {
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	return len(s) >= 3 && ('a' <= s[0] && s[0] <= 'z' || 'A' <= s[0] && s[0] <= 'Z') &&
		s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

func localPath(s string) (string, error) {
	if isWindowsPath(s) && runtime.GOOS != "windows" {
		return filepath.Clean(s), nil
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		s = filepath.Join(home, s[1:])
	}
	return filepath.Abs(s)
}

func parseURL(input, raw string) (Spec, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Spec{}, fmt.Errorf("invalid source %q: %w", input, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "ssh", "git", "file", "git+ssh", "ssh+git":
		return Spec{URL: raw}, nil
	default:
		return Spec{}, unrecognized(input)
	}
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	repo, rest := segs, []string(nil)
	switch strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") {
	case "github.com":
		if len(segs) > 2 {
			repo, rest = segs[:2], segs[2:]
		}
		return parseHosted(input, "github.com", repo, rest, "https://github.com/owner/repo[/tree/<ref>[/dir]]")
	case "gitlab.com":
		// The project path, which may include subgroups, ends at "/-/".
		if i := slices.Index(segs, "-"); i >= 0 {
			repo, rest = segs[:i], segs[i+1:]
		}
		return parseHosted(input, "gitlab.com", repo, rest, "https://gitlab.com/group/repo[/-/tree/<ref>[/dir]]")
	}
	return Spec{URL: raw}, nil
}

// parseHosted builds a GitHub or GitLab spec from the repository path
// segments and the rest of the URL path: nothing, tree/<ref>[/dir…], or
// blob/<ref>/<file path>. form describes the accepted URLs for errors.
func parseHosted(input, host string, repo, rest []string, form string) (Spec, error) {
	if len(repo) < 2 {
		return Spec{}, badHosted(input, form)
	}
	repo = slices.Clone(repo)
	repo[len(repo)-1] = strings.TrimSuffix(repo[len(repo)-1], ".git")
	for _, seg := range repo {
		if !nameRe.MatchString(seg) {
			return Spec{}, badHosted(input, form)
		}
	}
	spec := Spec{URL: "https://" + host + "/" + strings.Join(repo, "/") + ".git"}
	if len(rest) == 0 {
		return spec, nil
	}
	var dir []string
	switch {
	case rest[0] == "tree" && len(rest) >= 2:
		dir = rest[2:]
	case rest[0] == "blob" && len(rest) >= 3:
		dir = rest[2 : len(rest)-1] // the directory containing the file
	default:
		return Spec{}, badHosted(input, form)
	}
	spec.Ref = rest[1]
	var err error
	spec.Subdir, err = subdir(input, dir)
	return spec, err
}

// parseShorthand parses owner/repo[/sub/dir…][@skill].
func parseShorthand(input, s string) (Spec, error) {
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return Spec{}, unrecognized(input)
	}
	var skills []string
	if base, name, ok := strings.CutLast(s, "@"); ok && !strings.ContainsAny(name, `/\`) {
		if name == "" {
			return Spec{}, unrecognized(input)
		}
		s, skills = base, []string{name}
	}
	segs := strings.Split(s, "/")
	if len(segs) < 2 {
		return Spec{}, unrecognized(input)
	}
	owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
	if !nameRe.MatchString(owner) || !nameRe.MatchString(repo) {
		return Spec{}, unrecognized(input)
	}
	dir, err := subdir(input, segs[2:])
	if err != nil {
		return Spec{}, err
	}
	return Spec{URL: "https://github.com/" + owner + "/" + repo + ".git", Subdir: dir, Skills: skills}, nil
}

// subdir joins path segments into a clean, slash-separated relative path.
func subdir(input string, segs []string) (string, error) {
	if slices.Contains(segs, "..") {
		return "", fmt.Errorf("invalid source %q: the directory must not contain \"..\"", input)
	}
	return strings.Trim(path.Clean("/"+strings.Join(segs, "/")), "/"), nil
}

// hostRepo returns the lower-cased host without "www." and the repository
// path without ".git" of a URL or scp-like remote.
func hostRepo(remote string) (host, repo string) {
	var p string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", ""
		}
		host, p = u.Hostname(), u.Path
	} else if m := scpRe.FindStringSubmatch(remote); m != nil {
		host, p = m[1], m[2]
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host, strings.TrimSuffix(strings.Trim(p, "/"), ".git")
}

// escapePath escapes each segment of a slash-separated path for a URL.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func unrecognized(input string) error {
	return fmt.Errorf("unrecognized source %q; use owner/repo, a git URL, or a path like ./dir", input)
}

func badHosted(input, form string) error {
	return fmt.Errorf("unrecognized source %q; use %s", input, form)
}

// hasPrefixFold reports whether s starts with any of prefixes, ignoring case.
func hasPrefixFold(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && strings.EqualFold(s[:len(p)], p) {
			return true
		}
	}
	return false
}
