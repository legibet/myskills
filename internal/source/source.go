// Package source parses the source specs accepted by `myskills add`.
package source

// Kind is the kind of a source.
type Kind int

const (
	Git Kind = iota
	Local
)

// Spec is a parsed source (design §5).
type Spec struct {
	Kind  Kind
	Input string // as typed by the user

	// Git sources.
	URL    string // clone URL
	Ref    string // branch or tag; "" for the remote HEAD
	Subdir string // directory inside the repository to discover from; "" for the root

	// Local sources.
	Path string // absolute, cleaned

	// Skill restricts the source to the skill with this name ("" for all).
	Skill string
}

// Parse parses a source spec. Local paths are only recognized in explicit
// forms ("./x", "../x", "/x", "~/x", Windows drive and UNC paths) and are made
// absolute against the current directory.
func Parse(input string) (Spec, error) {
	panic("not implemented")
}

// IsExplicit reports whether input is unambiguously a source spec rather than
// a search query: a URL, a git remote, an explicit local path, or owner/repo
// shorthand. The TUI Find view uses it to offer browsing a source.
func IsExplicit(input string) bool {
	panic("not implemented")
}

// Label returns a short human label for a git URL: "owner/repo" for GitHub,
// the repository path for GitLab, otherwise the URL without scheme and ".git".
func Label(url string) string {
	panic("not implemented")
}

// WebURL returns the browser URL of a skill directory in a GitHub or GitLab
// repository, or "" for other hosts.
func WebURL(url, ref, path string) string {
	panic("not implemented")
}
