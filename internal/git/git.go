// Package git runs the git command line for the few operations myskills needs.
package git

import (
	"context"
	"errors"
)

// ErrNotRepo reports that a directory is not inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// ErrNotFound reports that git is not installed.
var ErrNotFound = errors.New("git is not installed")

// Available reports whether the git binary can be found.
func Available() bool {
	panic("not implemented")
}

// Clone shallow-clones url into dir (which must not exist) at ref, or at the
// remote HEAD when ref is empty, and returns the checked-out commit.
func Clone(ctx context.Context, url, ref, dir string) (commit string, err error) {
	panic("not implemented")
}

// RemoteCommit returns the commit the remote ref points to (design §7): HEAD
// when ref is empty, otherwise refs/heads/<ref>, then refs/tags/<ref> (peeled).
func RemoteCommit(ctx context.Context, url, ref string) (string, error) {
	panic("not implemented")
}

// Toplevel returns the root of the work tree containing dir, or ErrNotRepo.
func Toplevel(dir string) (string, error) {
	panic("not implemented")
}

// GitPath resolves `git rev-parse --git-path name` for the repository
// containing dir, as an absolute path.
func GitPath(dir, name string) (string, error) {
	panic("not implemented")
}

// Worktrees returns the root of every work tree of the repository containing
// dir, including dir's own.
func Worktrees(dir string) ([]string, error) {
	panic("not implemented")
}

// Tracked reports whether any file at or below path is tracked in the index
// of the work tree rooted at root.
func Tracked(root, path string) (bool, error) {
	panic("not implemented")
}

// Ignored reports whether path is ignored by the work tree rooted at root.
func Ignored(root, path string) (bool, error) {
	panic("not implemented")
}
