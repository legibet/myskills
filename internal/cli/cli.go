// Package cli implements the myskills command line (design §8). It parses
// arguments, calls internal/core and formats the results; all domain logic
// lives in core.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
)

// Execute runs the command line and returns the process exit code.
func Execute() int {
	// The first interrupt cancels the context; a second one kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	isTerm := func(f *os.File) bool { return term.IsTerminal(f.Fd()) }
	s := streams{
		in:          os.Stdin,
		out:         os.Stdout,
		err:         os.Stderr,
		interactive: isTerm(os.Stdin) && isTerm(os.Stdout),
		outTTY:      isTerm(os.Stdout),
		errTTY:      isTerm(os.Stderr),
	}
	return run(ctx, os.Args[1:], s, core.Open)
}

// streams are the standard streams and what is known about them.
type streams struct {
	in          io.Reader
	out, err    io.Writer
	interactive bool // stdin and stdout are terminals: TUI and prompts allowed
	outTTY      bool // stdout is a terminal: rendered markdown
	errTTY      bool // stderr is a terminal: spinners and progress
}

// app is the state shared by the commands of one run.
type app struct {
	in          io.Reader
	out         io.Writer // stdout, colors adapted to the terminal and NO_COLOR
	err         io.Writer // stderr, same
	rawOut      io.Writer // stdout as given, for JSON
	rawErr      io.Writer // stderr as given, for progress lines
	interactive bool
	outTTY      bool
	errTTY      bool

	open   func() (*core.Service, error)
	svc    *core.Service
	agents []agent.Agent // loaded on first use by agentNames

	failed bool // an item failed: exit code 1
}

// run executes the command line with args and returns the exit code. open
// creates the service on first use, so tests can point it at temp files and
// a fake skills.sh server.
func run(ctx context.Context, args []string, s streams, open func() (*core.Service, error)) int {
	a := &app{
		in:          s.in,
		out:         colorprofile.NewWriter(s.out, os.Environ()),
		err:         colorprofile.NewWriter(s.err, os.Environ()),
		rawOut:      s.out,
		rawErr:      s.err,
		interactive: s.interactive,
		outTTY:      s.outTTY,
		errTTY:      s.errTTY,
		open:        open,
	}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(a.out)
	root.SetErr(a.err)
	if err := root.ExecuteContext(ctx); err != nil {
		a.failErr(err)
	}
	if a.failed {
		return 1
	}
	return 0
}

// service returns the core service, opening it on first use.
func (a *app) service() (*core.Service, error) {
	if a.svc == nil {
		svc, err := a.open()
		if err != nil {
			return nil, err
		}
		a.svc = svc
	}
	return a.svc, nil
}

// hintError is an error with a suggestion printed dimmed below it.
type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() }
func (e *hintError) Unwrap() error { return e.err }

// withHint adds a hint to err.
func withHint(err error, hint string) error {
	return &hintError{err: err, hint: hint}
}

// usageError is a flag or argument error, with a pointer to the command's help.
func usageError(cmd *cobra.Command, err error) error {
	return withHint(err, fmt.Sprintf("Run '%s --help' for usage.", cmd.CommandPath()))
}

// args wraps a cobra argument validator so its errors point to the help.
func args(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return usageError(cmd, err)
		}
		return nil
	}
}

// versionString is the module version that go build stamps: the tag, or the
// commit's pseudo-version.
func versionString() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "devel"
}
