// Package git runs the git command line for the few operations myskills needs.
package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo reports that a directory is not inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// ErrNotFound reports that git is not installed.
var ErrNotFound = errors.New("git is not installed")

// Clone shallow-clones url into dir (which must not exist) at ref, or at the
// remote HEAD when ref is empty, and returns the checked-out commit.
func Clone(ctx context.Context, url, ref, dir string) (commit string, err error) {
	if _, statErr := os.Lstat(dir); errors.Is(statErr, fs.ErrNotExist) {
		defer func() {
			if err != nil {
				_ = os.RemoveAll(dir)
			}
		}()
	}
	args := []string{"-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--single-branch"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dir)
	if _, err := run(ctx, "git clone "+url, args...); err != nil {
		return "", err
	}
	return run(ctx, "git rev-parse HEAD in "+dir, "-C", dir, "rev-parse", "HEAD")
}

// RemoteCommit returns the commit the remote ref points to (design §7): HEAD
// when ref is empty, otherwise refs/heads/<ref>, then refs/tags/<ref> (peeled).
func RemoteCommit(ctx context.Context, url, ref string) (string, error) {
	// Candidates in order of preference. ls-remote patterns match any ref
	// ending in the pattern, so the output is filtered by exact name.
	want := []string{"HEAD"}
	if ref != "" {
		want = []string{"refs/heads/" + ref, "refs/tags/" + ref + "^{}", "refs/tags/" + ref}
	}
	out, err := run(ctx, "git ls-remote "+url, append([]string{"ls-remote", "--", url}, want...)...)
	if err != nil {
		return "", err
	}
	refs := map[string]string{}
	for line := range strings.Lines(out) {
		if hash, name, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "\t"); ok {
			refs[name] = hash
		}
	}
	for _, name := range want {
		if hash := refs[name]; hash != "" {
			return hash, nil
		}
	}
	return "", fmt.Errorf("ref %q not found in %s", cmp.Or(ref, "HEAD"), url)
}

// Toplevel returns the root of the work tree containing dir, or ErrNotRepo.
func Toplevel(dir string) (string, error) {
	out, err := runIn(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("%s: %w", dir, ErrNotRepo)
	}
	return filepath.Clean(out), nil
}

// GitPath resolves `git rev-parse --git-path name` for the repository
// containing dir, as an absolute path.
func GitPath(dir, name string) (string, error) {
	out, err := runIn(dir, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	p := filepath.FromSlash(out)
	if !filepath.IsAbs(p) {
		// git prints the path relative to the directory it ran in.
		p = filepath.Join(dir, p)
	}
	return filepath.Abs(p)
}

// Worktrees returns the root of every work tree of the repository containing
// dir, including dir's own.
func Worktrees(dir string) ([]string, error) {
	out, err := runIn(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var roots []string
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			roots = append(roots, filepath.Clean(p))
		} else if line == "bare" && len(roots) > 0 {
			roots = roots[:len(roots)-1]
		}
	}
	return roots, nil
}

// Tracked reports whether any file at or below path is tracked in the index
// of the work tree rooted at root.
func Tracked(root, path string) (bool, error) {
	out, err := runIn(root, "ls-files", "--", ":(literal)"+relTo(root, path))
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// Ignored reports whether path is ignored by the work tree rooted at root.
func Ignored(root, path string) (bool, error) {
	_, err := runIn(root, "check-ignore", "-q", "--", relTo(root, path))
	if err == nil {
		return true, nil
	}
	if re, ok := errors.AsType[*runError](err); ok && re.code == 1 {
		return false, nil
	}
	return false, err
}

// relTo returns path relative to root, slash-separated, when path is
// absolute; a relative path is taken as already relative to root.
func relTo(root, path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(root, path); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(path)
}

// runError is a git command that exited with a non-zero status.
type runError struct {
	desc   string // what was run, e.g. "git clone <url>"
	code   int
	stderr string
}

func (e *runError) Error() string {
	return e.desc + ": " + cmp.Or(message(e.stderr), fmt.Sprintf("exit status %d", e.code))
}

// run runs git with args and returns its standard output without the final
// newline. desc names the command in errors.
func run(ctx context.Context, desc string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	// Never prompt for credentials, and keep messages in English so they can
	// be recognized.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimRight(string(out), "\r\n"), nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("%s: %w", desc, ErrNotFound)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s: %w", desc, ctx.Err())
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", &runError{desc: desc, code: ee.ExitCode(), stderr: string(ee.Stderr)}
	}
	return "", fmt.Errorf("%s: %w", desc, err)
}

// runIn runs a git subcommand in dir and reports ErrNotRepo when dir is not
// inside a work tree.
func runIn(dir string, args ...string) (string, error) {
	out, err := run(context.Background(), "git "+args[0]+" in "+dir, append([]string{"-C", dir}, args...)...)
	if re, ok := errors.AsType[*runError](err); ok &&
		(strings.Contains(re.stderr, "not a git repository") || strings.Contains(re.stderr, "must be run in a work tree")) {
		return "", fmt.Errorf("%s: %w", dir, ErrNotRepo)
	}
	return out, err
}

// message extracts git's explanation from its stderr: the lines up to and
// including the first "fatal:" or "error:" line, without hints and warnings
// (so "git@host: Permission denied (publickey)." is kept), or the first line
// when there is no such line.
func message(stderr string) string {
	var lines []string
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "hint:") || strings.HasPrefix(line, "warning:") {
			continue
		}
		for _, prefix := range []string{"fatal: ", "error: "} {
			if msg, ok := strings.CutPrefix(line, prefix); ok {
				return strings.Join(append(lines, msg), " ")
			}
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		return lines[0]
	}
	return ""
}
