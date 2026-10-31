package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
)

// Styles use the terminal's own basic colors; the colorprofile writers drop
// them when output is piped or NO_COLOR is set.
var (
	styleOK   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	styleWarn = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	styleErr  = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleDim  = lipgloss.NewStyle().Faint(true)
	styleBold = lipgloss.NewStyle().Bold(true)
)

func dim(s string) string  { return styleDim.Render(s) }
func bold(s string) string { return styleBold.Render(s) }

// ok prints a success line to stdout.
func (a *app) ok(msg string) {
	a.println(styleOK.Render("✓") + " " + msg)
}

// warn prints a warning line to stdout.
func (a *app) warn(msg string) {
	a.println(styleWarn.Render("!") + " " + msg)
}

// fail prints an error line to stderr and makes the run exit with 1.
func (a *app) fail(msg string) {
	a.failed = true
	a.eprintln(styleErr.Render("✗") + " " + msg)
}

// failErr reports a failed item, with the error's hint if it has one.
func (a *app) failErr(err error) {
	if errors.Is(err, context.Canceled) {
		a.fail("interrupted")
		return
	}
	a.fail(err.Error())
	if h, ok := errors.AsType[*hintError](err); ok && h.hint != "" {
		a.eprintln(dim("  " + h.hint))
	}
}

// note prints a dimmed line to stdout.
func (a *app) note(msg string) {
	a.println(dim(msg))
}

// printJSON writes v as one indented JSON document to stdout.
func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.rawOut)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// println writes a line to stdout. Lines carry text from skills, sources and
// file names, so only their styling survives as escape sequences. Write
// errors are ignored: there is nowhere left to report them.
func (a *app) println(s string) {
	_, _ = fmt.Fprintln(a.out, sanitize.Styled(s))
}

// eprintln writes a line to stderr, like println.
func (a *app) eprintln(s string) {
	_, _ = fmt.Fprintln(a.err, sanitize.Styled(s))
}

// table formats rows as columns separated by two spaces, each line prefixed
// with indent. Cells may be styled: widths ignore escape sequences.
func table(indent string, rows [][]string) string {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	lines := make([]string, len(rows))
	for r, row := range rows {
		// Drop empty trailing cells, which may still hold escape sequences.
		for len(row) > 0 && lipgloss.Width(row[len(row)-1]) == 0 {
			row = row[:len(row)-1]
		}
		var b strings.Builder
		b.WriteString(indent)
		for i, cell := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)))
			}
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}

// tilde shortens a path in the home directory to "~/…".
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && filepath.IsLocal(rel) {
		return filepath.Join("~", rel)
	}
	return p
}

// scopePath shows a placement path relative to the project root of scope,
// or with "~" for the home directory.
func scopePath(scope core.Scope, p string) string {
	if !scope.IsGlobal() {
		if rel, err := filepath.Rel(scope.Root, p); err == nil && filepath.IsLocal(rel) {
			return rel
		}
	}
	return tilde(p)
}

// plural returns "1 skill" or "3 skills".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// confirm asks a yes/no question on stderr and reads the answer from stdin.
// Anything but "y" or "yes" is no. An interrupt while waiting returns the
// context's error.
func (a *app) confirm(ctx context.Context, question string) (bool, error) {
	_, _ = fmt.Fprint(a.err, sanitize.Styled(question)+" [y/N] ")
	type reply struct {
		line string
		err  error
	}
	answer := make(chan reply, 1)
	go func() {
		line, err := bufio.NewReader(a.in).ReadString('\n')
		answer <- reply{line, err}
	}()
	select {
	case <-ctx.Done():
		_, _ = fmt.Fprintln(a.err)
		return false, ctx.Err()
	case r := <-answer:
		if r.err != nil && !errors.Is(r.err, io.EOF) {
			return false, r.err
		}
		line := strings.ToLower(strings.TrimSpace(r.line))
		return line == "y" || line == "yes", nil
	}
}

// agentNames returns the display names of agent ids, joined by commas.
func (a *app) agentNames(ids []string) string {
	if a.agents == nil {
		if svc, err := a.service(); err == nil {
			a.agents, _ = svc.Agents()
		}
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = id
		for _, ag := range a.agents {
			if ag.ID == id {
				names[i] = ag.Name
				break
			}
		}
	}
	return strings.Join(names, ", ")
}

// printResult prints the changes of an enable, disable or refresh.
func (a *app) printResult(scope core.Scope, res core.Result) {
	for _, c := range res.Changes {
		path, agents := scopePath(scope, c.Path), "("+a.agentNames(c.Agents)+")"
		switch c.Action {
		case core.Removed:
			verb := "removed"
			if c.Mode == core.Link {
				verb = "unlinked"
			}
			a.ok(fmt.Sprintf("%s %s from %s %s", verb, res.Skill, path, dim(agents)))
		case core.Unchanged:
			if c.Warning == "" {
				state := "linked"
				if c.Mode == core.Copy {
					state = "copied"
				}
				a.ok(dim(fmt.Sprintf("%s already %s → %s %s", res.Skill, state, path, agents)))
			}
		default:
			a.ok(fmt.Sprintf("%s %s → %s %s", c.Action, res.Skill, path, dim(agents)))
		}
		if c.Warning != "" {
			a.warn(path + ": " + c.Warning)
		}
	}
	for _, w := range res.Warnings {
		a.warn(w)
	}
}

// progress is a spinner with a message on stderr. It does nothing when
// stderr is not a terminal.
type progress struct {
	w    io.Writer
	mu   sync.Mutex
	msg  string
	stop chan struct{}
	done chan struct{}
}

// progress starts a spinner showing msg until stop is called.
func (a *app) progress(msg string) *progress {
	p := &progress{msg: msg}
	if !a.errTTY {
		return p
	}
	p.w, p.stop, p.done = a.rawErr, make(chan struct{}), make(chan struct{})
	go p.loop()
	return p
}

func (p *progress) loop() {
	defer close(p.done)
	frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; i++ {
		p.mu.Lock()
		_, _ = fmt.Fprintf(p.w, "\r\x1b[K%c %s", frames[i%len(frames)], p.msg)
		p.mu.Unlock()
		select {
		case <-p.stop:
			_, _ = fmt.Fprint(p.w, "\r\x1b[K")
			return
		case <-tick.C:
		}
	}
}

// set changes the message.
func (p *progress) set(msg string) {
	p.mu.Lock()
	p.msg = msg
	p.mu.Unlock()
}

// end stops the spinner and clears its line.
func (p *progress) end() {
	if p.stop == nil {
		return
	}
	close(p.stop)
	<-p.done
	p.stop = nil
}
